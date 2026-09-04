package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/n1tishc/mulch/internal/cli"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestResumeReconstructsVisibleContextAndAppendsHistory(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var runErr bytes.Buffer
	opts := cli.Options{Stdout: &bytes.Buffer{}, Stderr: &runErr, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }}
	if err := cli.Execute(t.Context(), []string{"run", "--db", db, "hello"}, opts); err != nil {
		t.Fatal(err)
	}
	id := sessionID(t, runErr.String())
	store, err := event.Open(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	capture := &captureLLM{}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute(t.Context(), []string{"resume", "--db", db, id, "continue"}, cli.Options{Stdout: &stdout, Stderr: &stderr, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return capture }}); err != nil {
		t.Fatal(err)
	}
	request := capture.Request()
	if len(request.Messages) != 4 || request.Messages[1].Blocks[0].Text != "hello" || request.Messages[2].Blocks[0].Text != "streamed answer" || request.Messages[3].Blocks[0].Text != "continue" {
		t.Fatalf("resumed messages = %#v", request.Messages)
	}
	store, err = event.Open(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) {
		t.Fatalf("history length = %d, want > %d", len(after), len(before))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Seq != after[i].Seq {
			t.Fatalf("history rewritten at index %d", i)
		}
	}
}

func TestBranchTreeSessionsAndLabelCommands(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var runErr bytes.Buffer
	if err := cli.Execute(t.Context(), []string{"run", "--db", db, "root task"}, cli.Options{Stdout: &bytes.Buffer{}, Stderr: &runErr, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }}); err != nil {
		t.Fatal(err)
	}
	root := sessionID(t, runErr.String())
	var branchErr bytes.Buffer
	if err := cli.Execute(t.Context(), []string{"branch", root, "--at", "3", "fork task", "--db", db, "--no-intervene"}, cli.Options{Stdout: &bytes.Buffer{}, Stderr: &branchErr, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }}); err != nil {
		t.Fatal(err)
	}
	child := sessionID(t, branchErr.String())
	if err := cli.Execute(t.Context(), []string{"label", "--db", db, child, "experiment"}, cli.Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: testGetenv}); err != nil {
		t.Fatal(err)
	}
	var sessionsOut, treeOut bytes.Buffer
	if err := cli.Execute(t.Context(), []string{"sessions", "--db", db}, cli.Options{Stdout: &sessionsOut, Stderr: &bytes.Buffer{}, Getenv: testGetenv}); err != nil {
		t.Fatal(err)
	}
	if err := cli.Execute(t.Context(), []string{"tree", "--db", db, root}, cli.Options{Stdout: &treeOut, Stderr: &bytes.Buffer{}, Getenv: testGetenv}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sessionsOut.String(), child+"\tcompleted\texperiment\t"+root+"\t3") {
		t.Fatalf("sessions output = %q", sessionsOut.String())
	}
	if !strings.Contains(treeOut.String(), "  "+child+" [completed] fork=3 \"experiment\"") {
		t.Fatalf("tree output = %q", treeOut.String())
	}
}

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
	id := sessionID(t, stderr.String())
	if !strings.Contains(stderr.String(), "[t1] health") {
		t.Fatalf("stderr = %q", stderr.String())
	}
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

func TestRunScoresRelevanceWhenEmbeddingProviderIsConfigured(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var stderr bytes.Buffer
	getenv := func(key string) string {
		if key == "MULCH_PROVIDER_API_KEY" || key == "MULCH_EMBEDDING_API_KEY" {
			return "test"
		}
		return ""
	}
	opts := cli.Options{
		Stdout: &bytes.Buffer{}, Stderr: &stderr, Getenv: getenv,
		LLMFactory:      func(string, string) provider.LLM { return commandLLM{} },
		EmbedderFactory: func(string, string, string) provider.Embedder { return constantEmbedder{} },
	}
	if err := cli.Execute(t.Context(), []string{"run", "--db", db, "answer the greeting"}, opts); err != nil {
		t.Fatal(err)
	}
	store, err := event.Open(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events, err := store.List(t.Context(), sessionID(t, stderr.String()), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range events {
		if recorded.Type != event.TypeScoreHealth {
			continue
		}
		var health event.ScoreHealth
		if err := recorded.Decode(&health); err != nil {
			t.Fatal(err)
		}
		if health.Relevance == nil || *health.Relevance != 1 {
			t.Fatalf("relevance = %v", health.Relevance)
		}
		return
	}
	t.Fatal("missing score.health")
}

func TestRunNoInterveneKeepsHealthScoringAndPolicyIsValidated(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mulch.db")
	var stderr bytes.Buffer
	if err := cli.Execute(t.Context(), []string{"run", "--no-intervene", "--db", db, "hello"}, cli.Options{Stdout: &bytes.Buffer{}, Stderr: &stderr, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }}); err != nil {
		t.Fatal(err)
	}
	store, err := event.Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), sessionID(t, stderr.String()), 1)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !eventTypesContain(events, event.TypeScoreHealth) {
		t.Fatal("health scoring disabled by --no-intervene")
	}
	if eventTypesContain(events, event.TypeInterveneFire) || eventTypesContain(events, event.TypeInterveneSkip) {
		t.Fatal("intervention event recorded with --no-intervene")
	}

	policy := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(policy, []byte(`{"warn_below":20,"prune_below":65}`), 0600); err != nil {
		t.Fatal(err)
	}
	err = cli.Execute(t.Context(), []string{"run", "--db", filepath.Join(t.TempDir(), "invalid.db"), "--policy", policy, "hello"}, cli.Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return commandLLM{} }})
	if err == nil || !strings.Contains(err.Error(), "thresholds") {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func eventTypesContain(events []event.Event, want event.Type) bool {
	for _, candidate := range events {
		if candidate.Type == want {
			return true
		}
	}
	return false
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
	id := sessionID(t, runErr.String())

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

func sessionID(t *testing.T, output string) string {
	t.Helper()
	fields := strings.Fields(output)
	for i := range fields {
		if fields[i] == "session" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	t.Fatalf("missing session ID in %q", output)
	return ""
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

type constantEmbedder struct{}

func (constantEmbedder) Model() string { return "test" }
func (constantEmbedder) MaxBatch() int { return 1000 }
func (constantEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func (commandLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	out <- provider.Delta{Text: "streamed "}
	out <- provider.Delta{Text: "answer"}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "streamed answer"}}, StopReason: "stop", Model: req.Model}, nil
}

func testGetenv(key string) string {
	if key == "MULCH_PROVIDER_API_KEY" {
		return "test"
	}
	return ""
}

type captureLLM struct {
	mu      sync.Mutex
	request provider.Request
}

func (c *captureLLM) Stream(_ context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	c.mu.Lock()
	c.request = req
	c.mu.Unlock()
	out <- provider.Delta{Text: "continued"}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "continued"}}, StopReason: "stop", Model: req.Model}, nil
}
func (c *captureLLM) Request() provider.Request { c.mu.Lock(); defer c.mu.Unlock(); return c.request }
