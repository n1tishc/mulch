package cli_test

import (
	"bytes"
	"context"
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

type commandLLM struct{}

func (commandLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	out <- provider.Delta{Text: "streamed "}
	out <- provider.Delta{Text: "answer"}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "streamed answer"}}, StopReason: "stop", Model: req.Model}, nil
}
