package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/cli"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

type chatLLM struct {
	mu         sync.Mutex
	requests   []provider.Request
	interrupts chan os.Signal
}

func (c *chatLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	// Only primary requests carry the coding tools.
	if len(req.Tools) == 0 {
		return provider.Response{}, nil
	}
	c.mu.Lock()
	c.requests = append(c.requests, req)
	first := len(c.requests) == 1
	c.mu.Unlock()
	if first && c.interrupts != nil {
		c.interrupts <- os.Interrupt
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
	out <- provider.Delta{Text: "answer"}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "answer"}}}, nil
}

func TestChatPreservesConversationAndReopens(t *testing.T) {
	db := filepath.Join(t.TempDir(), "chat.db")
	fake := &chatLLM{}
	var output bytes.Buffer
	opts := cli.Options{Stdin: strings.NewReader("hello\nfollow up\n/exit\n"), Stdout: &output, Stderr: io.Discard, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return fake }}
	args := []string{"chat", "--db", db, "--workdir", t.TempDir(), "--no-intervene"}
	if err := cli.Execute(t.Context(), args, opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("requests = %d", len(fake.requests))
	}
	second := fake.requests[1]
	if len(second.Messages) != 4 || second.Messages[1].Blocks[0].Text != "hello" || second.Messages[2].Blocks[0].Text != "answer" || second.Messages[3].Blocks[0].Text != "follow up" {
		t.Fatalf("lost history: %#v", second.Messages)
	}
	opts.Stdin = strings.NewReader("third message\n") // EOF exits after completion.
	if err := cli.Execute(t.Context(), []string{"chat", "--db", db, "--resume", second.SessionID, "--no-intervene"}, opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 3 || len(fake.requests[2].Messages) != 6 {
		t.Fatal("reopen lost conversation")
	}
	store, err := event.Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sessions, err := store.Sessions(t.Context())
	if err != nil || len(sessions) != 1 || sessions[0].Status != event.StatusCompleted {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
}

func TestChatInterruptReturnsToPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	interrupts := make(chan os.Signal, 1)
	fake := &chatLLM{interrupts: interrupts}
	var output bytes.Buffer
	err := cli.Execute(ctx, []string{"chat", "--db", filepath.Join(t.TempDir(), "chat.db"), "--workdir", t.TempDir(), "--no-intervene"}, cli.Options{Stdin: strings.NewReader("interrupt me\ntry again\n/exit\n"), Interrupts: interrupts, Stdout: &output, Stderr: io.Discard, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return fake }})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 2 || !strings.Contains(output.String(), "Task stopped:") || !strings.Contains(output.String(), "answer") {
		t.Fatalf("chat did not recover: %s", output.String())
	}
	if fake.requests[0].SessionID != fake.requests[1].SessionID {
		t.Fatal("interrupt changed session")
	}
}

func TestChatExitWithoutProviderCalls(t *testing.T) {
	fake := &chatLLM{}
	err := cli.Execute(t.Context(), []string{"chat", "--db", filepath.Join(t.TempDir(), "chat.db")}, cli.Options{Stdin: strings.NewReader("/help\n/session\n/exit\n"), Stdout: io.Discard, Stderr: io.Discard, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return fake }})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 0 {
		t.Fatal("idle chat called provider")
	}
}

func TestChatSessionCommandsPreserveHistoryAndRejectUnknown(t *testing.T) {
	db := filepath.Join(t.TempDir(), "chat.db")
	fake := &chatLLM{}
	var output bytes.Buffer
	opts := cli.Options{Stdin: strings.NewReader("first task\n/rename first\n/status\n/typo\n/new\n/mode plain\nsecond task\n/model test-other\nthird task\n/sessions\n/exit\n"), Stdout: &output, Stderr: io.Discard, Getenv: testGetenv, LLMFactory: func(string, string) provider.LLM { return fake }}
	if err := cli.Execute(t.Context(), []string{"--db", db, "--workdir", t.TempDir(), "--no-intervene"}, opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("commands reached provider: %d requests", len(fake.requests))
	}
	for _, request := range fake.requests {
		if len(request.Messages) != 2 {
			t.Fatal("new session retained old messages")
		}
	}
	if fake.requests[2].Model != "test-other" {
		t.Fatal("model command was not applied")
	}
	if !strings.Contains(output.String(), "unknown command") {
		t.Fatal("unknown command not rejected")
	}
	store, err := event.Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.Sessions(t.Context())
	if err != nil || len(sessions) != 3 {
		t.Fatalf("saved sessions: %#v %v", sessions, err)
	}
	if sessions[0].Label != "first" {
		t.Fatal("rename lost")
	}
	store.Close()
	opts.Stdin = strings.NewReader("/resume " + fake.requests[0].SessionID + "\nfollow up\n/exit\n")
	if err := cli.Execute(t.Context(), []string{"--db", db, "--no-intervene"}, opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 4 || len(fake.requests[3].Messages) != 4 || fake.requests[3].SessionID != fake.requests[0].SessionID {
		t.Fatal("slash resume did not restore the original conversation")
	}
}
