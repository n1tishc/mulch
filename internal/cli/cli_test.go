package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n1tishc/mulch/internal/cli"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestRunStreamsAnswerAndPrintsDurableSessionID(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var stdout, stderr bytes.Buffer
	err := cli.Execute(t.Context(), []string{"run", "--db", db, "hello"}, cli.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(key string) string {
		if key == "MULCH_PROVIDER_API_KEY" {
			return "test"
		}
		return ""
	}, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "streamed answer" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	fields := strings.Fields(stderr.String())
	if len(fields) != 2 || fields[0] != "session" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	id := fields[1]
	store, err := event.Open(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.Session(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != event.StatusCompleted {
		t.Fatalf("status = %s", session.Status)
	}
}

func TestRunJSONWritesOnlyCommittedEventsAsJSONLines(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var stdout, stderr bytes.Buffer
	err := cli.Execute(t.Context(), []string{"run", "--json", "--db", db, "hello"}, cli.Options{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(key string) string {
			if key == "MULCH_PROVIDER_API_KEY" {
				return "test"
			}
			return ""
		},
		LLMFactory: func(string, string) provider.LLM { return commandLLM{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("stdout contained no events")
	}
	var types []event.Type
	for _, line := range lines {
		var got event.Event
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		if got.Seq == 0 || got.SessionID == "" || got.Payload == nil || got.CreatedAt.IsZero() {
			t.Fatalf("event omitted integration fields: %+v", got)
		}
		types = append(types, got.Type)
	}
	if !contains(types, event.TypeAssistantDelta) || !contains(types, event.TypeSessionEnd) {
		t.Fatalf("event types = %v", types)
	}
	if stderr.String() != "" {
		t.Fatalf("JSON mode wrote human output to stderr: %q", stderr.String())
	}
}

func TestReplayIsOfflineAndDeterministic(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var runOut, runErr bytes.Buffer
	err := cli.Execute(t.Context(), []string{"run", "--db", db, "hello"}, cli.Options{
		Stdout: &runOut, Stderr: &runErr,
		Getenv: func(key string) string {
			if key == "MULCH_PROVIDER_API_KEY" {
				return "test"
			}
			return ""
		},
		LLMFactory: func(string, string) provider.LLM { return commandLLM{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Fields(runErr.String())[1]

	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		err = cli.Execute(t.Context(), []string{"replay", "--db", db, id}, cli.Options{
			Stdout:     out,
			Stderr:     &bytes.Buffer{},
			Getenv:     func(string) string { return "" },
			LLMFactory: func(string, string) provider.LLM { t.Fatal("replay contacted model"); return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if first.String() != "streamed answer" || first.String() != second.String() {
		t.Fatalf("replays = %q and %q", first.String(), second.String())
	}

	var firstJSON, secondJSON bytes.Buffer
	for _, out := range []*bytes.Buffer{&firstJSON, &secondJSON} {
		err = cli.Execute(t.Context(), []string{"replay", "--json", "--db", db, id}, cli.Options{
			Stdout: out, Stderr: &bytes.Buffer{}, Getenv: func(string) string { return "" },
			LLMFactory: func(string, string) provider.LLM { t.Fatal("JSON replay contacted model"); return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if firstJSON.String() != secondJSON.String() {
		t.Fatal("repeated JSON replays differ")
	}
	var deltas []string
	for _, line := range strings.Split(strings.TrimSpace(firstJSON.String()), "\n") {
		var recorded event.Event
		if err := json.Unmarshal([]byte(line), &recorded); err != nil {
			t.Fatal(err)
		}
		if recorded.Type == event.TypeAssistantDelta {
			var delta event.AssistantDelta
			if err := recorded.Decode(&delta); err != nil {
				t.Fatal(err)
			}
			deltas = append(deltas, delta.Text)
		}
	}
	if len(deltas) != 2 || deltas[0] != "streamed " || deltas[1] != "answer" {
		t.Fatalf("replayed deltas = %#v", deltas)
	}
}

func contains(types []event.Type, want event.Type) bool {
	for _, got := range types {
		if got == want {
			return true
		}
	}
	return false
}

type commandLLM struct{}

func (commandLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	out <- provider.Delta{Text: "streamed "}
	out <- provider.Delta{Text: "answer"}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "streamed answer"}}, StopReason: "stop", Model: req.Model}, nil
}
