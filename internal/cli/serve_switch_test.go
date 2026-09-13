package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/server"
)

// This drives the actual daemon, including manager construction, config HTTP
// updates, runtime execution, and persisted conversation replay.
func TestServeProviderSwitchFromMissingCredentials(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	if err := writeConfig(path, map[string]string{
		"provider": "blank", "provider.blank.models": "blank-model",
		"provider.ready.models": "ready-model", "provider.ready.api-key": "ready-key",
		"provider.next.models": "next-model", "provider.next.api-key": "next-key",
	}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	addr := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := make(chan switchCall, 8)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Execute(ctx, []string{"serve", "--addr", addr, "--db", filepath.Join(root, "sessions.db"), "--workdir", root}, Options{
			Stdout: io.Discard, Stderr: io.Discard,
			Getenv: func(key string) string {
				if key == "MULCH_CONFIG" {
					return path
				}
				return ""
			},
			LLMFactory: func(key, _ string) provider.LLM {
				return &switchLLM{key: key, calls: calls, release: release}
			},
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(base + "/api/config")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	request := func(method, path, body string, status int, target any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.StatusCode, status, data)
		}
		if target != nil {
			if err := json.Unmarshal(data, target); err != nil {
				t.Fatal(err)
			}
		}
	}
	var config server.Config
	request("GET", "/api/config", "", 200, &config)
	if config.Ready || config.ReadOnlyReason == "" {
		t.Fatalf("initial readiness: %+v", config)
	}
	request("POST", "/api/sessions", `{"task":"blocked"}`, 409, nil)
	request("PATCH", "/api/config", `{"provider":"ready","mode":"plain"}`, 200, &config)
	if !config.Ready || config.Model != "ready-model" {
		t.Fatalf("selected readiness: %+v", config)
	}
	var started server.SessionResponse
	request("POST", "/api/sessions", `{"task":"first task"}`, 202, &started)
	var first switchCall
	select {
	case first = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("ready provider never executed")
	}
	if first.key != "ready-key" || first.request.Model != "ready-model" {
		t.Fatalf("first selection: %+v", first)
	}
	request("PATCH", "/api/config", `{"provider":"next","mode":"invalid"}`, 400, nil)
	request("GET", "/api/config", "", 200, &config)
	if config.Provider != "ready" {
		t.Fatal("invalid update partially changed provider")
	}
	request("PATCH", "/api/config", `{"provider":"next","mode":"plain"}`, 200, &config)
	close(release)
	var second switchCall
	select {
	case second = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("second tool turn never executed")
	}
	if second.key != "ready-key" || second.request.Model != "ready-model" {
		t.Fatalf("in-flight selection changed: %+v", second)
	}
	waitDone := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var detail struct {
				CanResume bool `json:"can_resume"`
			}
			request("GET", "/api/sessions/"+started.ID, "", 200, &detail)
			if detail.CanResume {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("task did not finish")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitDone()
	var resumed server.SessionResponse
	request("POST", "/api/sessions/"+started.ID+"/resume", `{"text":"follow-up"}`, 202, &resumed)
	if resumed.ID != started.ID {
		t.Fatal("resume replaced session")
	}
	select {
	case call := <-calls:
		data, _ := json.Marshal(call.request.Messages)
		if call.key != "next-key" || call.request.Model != "next-model" || !bytes.Contains(data, []byte("first task")) || !bytes.Contains(data, []byte("follow-up")) {
			t.Fatalf("resume did not preserve history and new selection: %+v", call)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never executed")
	}
	waitDone()
	request("PATCH", "/api/config", `{"provider":"blank"}`, 200, &config)
	if config.Ready || config.ReadOnlyReason == "" {
		t.Fatalf("missing-key readiness: %+v", config)
	}
	request("POST", "/api/sessions/"+started.ID+"/resume", `{"text":"blocked"}`, 409, nil)
}

type switchCall struct {
	key     string
	request provider.Request
}

type switchLLM struct {
	key     string
	calls   chan<- switchCall
	release <-chan struct{}
	turn    int
}

func (l *switchLLM) Stream(ctx context.Context, req provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	l.turn++
	select {
	case l.calls <- switchCall{l.key, req}:
	case <-ctx.Done():
		return provider.Response{}, ctx.Err()
	}
	if l.key == "ready-key" && l.turn == 1 {
		select {
		case <-l.release:
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
		return provider.Response{Model: req.Model, Blocks: []provider.Block{{Type: "tool_use", CallID: "write", Name: "write", Input: `{"path":"answer.txt","content":"ok"}`}}}, nil
	}
	return provider.Response{Model: req.Model, Blocks: []provider.Block{{Type: "text", Text: fmt.Sprintf("done with %s", req.Model)}}}, nil
}
