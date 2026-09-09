package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeValidatesAddressBeforeOpeningDatabase(t *testing.T) {
	err := Execute(context.Background(), []string{"serve", "--addr", "bad address", "--db", t.TempDir() + "/mulch.db"}, Options{Getenv: func(string) string { return "" }})
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("error = %v", err)
	}
}

func TestDaemonSessionsRecordHealthAndInterventionDecisions(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	db := filepath.Join(t.TempDir(), "daemon.db")
	done := make(chan error, 1)
	go func() {
		done <- Execute(ctx, []string{"serve", "--addr", addr, "--db", db}, Options{Stdout: io.Discard, Stderr: io.Discard, Getenv: func(name string) string {
			if name == "MULCH_PROVIDER_API_KEY" {
				return "fake"
			}
			return ""
		}, LLMFactory: func(string, string) provider.LLM { return &daemonLLM{} }})
	}()
	defer func() { cancel(); <-done }()
	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := http.Get(base + "/api/sessions")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	body, _ := json.Marshal(map[string]any{"task": "write answer", "opts": map[string]string{"workdir": t.TempDir()}})
	response, err := http.Post(base+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(response.Body).Decode(&started)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	store, err := event.Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for {
		es, err := store.List(t.Context(), started.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		health, decision, ended := false, false, false
		for _, e := range es {
			health = health || e.Type == event.TypeScoreHealth
			decision = decision || e.Type == event.TypeInterveneSkip
			ended = ended || e.Type == event.TypeSessionEnd
		}
		if ended {
			if !health || !decision {
				t.Fatalf("health=%v intervention decision=%v", health, decision)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type daemonLLM struct{ calls int }

func (l *daemonLLM) Stream(_ context.Context, req provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	l.calls++
	if l.calls == 1 {
		return provider.Response{Model: req.Model, InputTokens: 10, OutputTokens: 10, Blocks: []provider.Block{{Type: "tool_use", CallID: "write", Name: "write", Input: `{"path":"answer.txt","content":"ok"}`}}}, nil
	}
	return provider.Response{Model: req.Model, InputTokens: 10, OutputTokens: 10, Blocks: []provider.Block{{Type: "text", Text: "done"}}}, nil
}
