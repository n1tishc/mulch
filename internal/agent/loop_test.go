package agent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/tool"
)

func TestRunRecordsCompletedSessionAndExactRequest(t *testing.T) {
	store := openStore(t)
	llm := &fakeLLM{response: provider.Response{Blocks: []provider.Block{{Type: "text", Text: "answer"}}, StopReason: "stop", Model: "fake", InputTokens: 2, OutputTokens: 1}}
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: "."}, "hello", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Session(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != event.StatusCompleted {
		t.Fatalf("status = %s", s.Status)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	requestEvent := find(t, events, event.TypeLLMRequest)
	var request event.LLMRequest
	if err := requestEvent.Decode(&request); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := event.BuildMessages(events, request.VisibleEventSeqs)
	if err != nil {
		t.Fatal(err)
	}
	if !equalMessages(rebuilt, llm.request.Messages) {
		t.Fatalf("rebuilt request differs: %#v != %#v", rebuilt, llm.request.Messages)
	}
}

func TestRunCompletesCodingTaskThroughToolTurns(t *testing.T) {
	workdir := t.TempDir()
	store := openStore(t)
	llm := &scriptedLLM{responses: []provider.Response{
		{Blocks: []provider.Block{{Type: "tool_use", CallID: "write-1", Name: "write", Input: `{"path":"hello.go","content":"package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hello\")}\n"}`}}, StopReason: "tool_calls", Model: "fake"},
		{Blocks: []provider.Block{{Type: "tool_use", CallID: "bash-1", Name: "bash", Input: `{"command":"go run hello.go"}`}}, StopReason: "tool_calls", Model: "fake"},
		{Blocks: []provider.Block{{Type: "text", Text: "done"}}, StopReason: "stop", Model: "fake"},
	}}
	executor := tool.NewExecutor([]tool.Tool{tool.NewRead(workdir), tool.NewWrite(workdir), tool.NewEdit(workdir), tool.NewBash(workdir)})
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: workdir, Tools: executor, MaxTurns: 5}, "create and run hello.go", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workdir, "hello.go"))
	if err != nil || len(content) == 0 {
		t.Fatalf("hello.go = %q, %v", content, err)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var starts, results int
	for _, e := range events {
		switch e.Type {
		case event.TypeToolStart:
			starts++
			var payload event.ToolStart
			if err := e.Decode(&payload); err != nil || payload.CallID == "" || payload.StartedAt.IsZero() {
				t.Fatalf("tool.start = %#v, %v", payload, err)
			}
		case event.TypeToolResult:
			results++
			var payload event.ToolResult
			if err := e.Decode(&payload); err != nil || payload.CallID == "" || payload.DurationMS < 0 {
				t.Fatalf("tool.result = %#v, %v", payload, err)
			}
			if payload.Name == "bash" && (payload.IsError || payload.Output != "hello\n") {
				t.Fatalf("bash result = %#v", payload)
			}
		}
	}
	if starts != 2 || results != 2 {
		t.Fatalf("tool events: starts=%d results=%d", starts, results)
	}
	for i, request := range llm.requests {
		requestEvent := nth(t, events, event.TypeLLMRequest, i)
		var payload event.LLMRequest
		if err := requestEvent.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := event.BuildMessages(events, payload.VisibleEventSeqs)
		if err != nil {
			t.Fatal(err)
		}
		if !equalMessages(rebuilt, request.Messages) {
			t.Fatalf("turn %d request differs: %#v != %#v", i+1, rebuilt, request.Messages)
		}
	}
}

func TestRunRecordsPromptOverrideAndSource(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "SYSTEM.md"), []byte("local override"), 0644); err != nil {
		t.Fatal(err)
	}
	store := openStore(t)
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: &fakeLLM{response: provider.Response{Blocks: []provider.Block{{Type: "text", Text: "ok"}}, Model: "fake"}}, Model: "fake", Workdir: workdir}, "hello", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := store.List(t.Context(), id, 1)
	var payload event.SystemPrompt
	if err := find(t, events, event.TypeSystemPrompt).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != "local override" || len(payload.Sources) != 1 || payload.Sources[0].ModifiedAt.IsZero() {
		t.Fatalf("system prompt = %#v", payload)
	}
}

func TestRunRecordsFailedAndCancelledStatuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status event.Status
	}{{"failed", errors.New("boom"), event.StatusFailed}, {"cancelled", context.Canceled, event.StatusCancelled}} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			llm := &fakeLLM{err: tc.err}
			id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: "."}, "hello", func(string) {})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v", err)
			}
			s, err := store.Session(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if s.Status != tc.status {
				t.Fatalf("status = %s, want %s", s.Status, tc.status)
			}
			events, err := store.List(t.Context(), id, 1)
			if err != nil {
				t.Fatal(err)
			}
			end := find(t, events, event.TypeSessionEnd)
			var payload event.SessionEnd
			if err := end.Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Status != tc.status {
				t.Fatalf("session.end status = %s, want %s", payload.Status, tc.status)
			}
		})
	}
}

func TestRunEndsSessionWhenSetupFails(t *testing.T) {
	store := &setupFailureStore{visibleErr: errors.New("read failed")}
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: &fakeLLM{}, Model: "fake", Workdir: "."}, "hello", func(string) {})
	if !errors.Is(err, store.visibleErr) {
		t.Fatalf("error = %v", err)
	}
	if id == "" {
		t.Fatal("session ID is empty")
	}
	if store.status != event.StatusFailed {
		t.Fatalf("status = %s, want failed", store.status)
	}
	end := find(t, store.events, event.TypeSessionEnd)
	var payload event.SessionEnd
	if err := end.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != event.StatusFailed {
		t.Fatalf("session.end status = %s, want failed", payload.Status)
	}
}

type fakeLLM struct {
	request  provider.Request
	response provider.Response
	err      error
}

type scriptedLLM struct {
	responses []provider.Response
	requests  []provider.Request
}

func (f *scriptedLLM) Stream(_ context.Context, request provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	f.requests = append(f.requests, request)
	if len(f.responses) == 0 {
		return provider.Response{}, errors.New("unexpected model turn")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

type setupFailureStore struct {
	events     []event.Event
	visibleErr error
	status     event.Status
}

func (s *setupFailureStore) CreateSession(context.Context, event.Session) error { return nil }
func (s *setupFailureStore) Append(_ context.Context, e event.Event) (event.Event, error) {
	e.Seq = int64(len(s.events) + 1)
	s.events = append(s.events, e)
	return e, nil
}
func (s *setupFailureStore) Visible(context.Context, string) ([]event.Event, error) {
	return nil, s.visibleErr
}
func (s *setupFailureStore) EndSession(_ context.Context, _ string, status event.Status) error {
	s.status = status
	return nil
}

func (f *fakeLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	f.request = req
	if f.err != nil {
		return provider.Response{}, f.err
	}
	out <- provider.Delta{Text: "answer"}
	return f.response, nil
}
func openStore(t *testing.T) *event.SQLiteStore {
	t.Helper()
	s, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func find(t *testing.T, es []event.Event, typ event.Type) event.Event {
	t.Helper()
	for _, e := range es {
		if e.Type == typ {
			return e
		}
	}
	t.Fatalf("missing %s", typ)
	return event.Event{}
}
func nth(t *testing.T, es []event.Event, typ event.Type, n int) event.Event {
	t.Helper()
	for _, e := range es {
		if e.Type == typ {
			if n == 0 {
				return e
			}
			n--
		}
	}
	t.Fatalf("missing occurrence of %s", typ)
	return event.Event{}
}

func equalMessages(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || len(a[i].Blocks) != len(b[i].Blocks) {
			return false
		}
		for j := range a[i].Blocks {
			if a[i].Blocks[j] != b[i].Blocks[j] {
				return false
			}
		}
	}
	return true
}
