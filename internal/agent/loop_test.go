package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/tool"
)

func TestRunCancelsStreamingProviderAndRecordsSession(t *testing.T) {
	synctest.Test(t, testRunCancelsStreamingProviderAndRecordsSession)
}

func TestRunCallsBeforeToolsHookBeforeToolBatch(t *testing.T) {
	store := openStore(t)
	order := []string{}
	extension := &orderingHook{call: func() { order = append(order, "hook") }}
	executor := tool.NewExecutor([]tool.Tool{orderingTool{call: func() { order = append(order, "tool") }}})
	llm := &scriptedLLM{responses: []provider.Response{{Blocks: []provider.Block{{Type: "tool_use", CallID: "1", Name: "ordered", Input: `{}`}}}, {Blocks: []provider.Block{{Type: "text", Text: "done"}}}}}
	if _, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Tools: executor, Model: "fake", Workdir: ".", Hooks: []hook.Hook{extension}}, "test", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "hook,tool" {
		t.Fatalf("order = %v", order)
	}
}

func TestRunRecordsCancelledResultsWhenHookWithholdsToolBatch(t *testing.T) {
	store := openStore(t)
	toolRan := false
	executor := tool.NewExecutor([]tool.Tool{orderingTool{call: func() { toolRan = true }}})
	llm := &scriptedLLM{responses: []provider.Response{{Blocks: []provider.Block{{Type: "tool_use", CallID: "1", Name: "ordered", Input: `{}`}}}, {Blocks: []provider.Block{{Type: "text", Text: "reconsidered"}}}}}
	if _, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Tools: executor, Model: "fake", Workdir: ".", Hooks: []hook.Hook{withholdingHook{}}}, "test", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if toolRan {
		t.Fatal("withheld tool was executed")
	}
	if len(llm.requests) != 2 {
		t.Fatalf("requests = %d", len(llm.requests))
	}
	last := llm.requests[1].Messages[len(llm.requests[1].Messages)-1]
	if last.Role != provider.RoleTool || len(last.Blocks) != 1 || !last.Blocks[0].IsError {
		t.Fatalf("reconsideration context = %#v", llm.requests[1].Messages)
	}
}

func TestRunRecordsEscalatedTerminalStatusBeforePendingTools(t *testing.T) {
	store := openStore(t)
	toolRan := false
	executor := tool.NewExecutor([]tool.Tool{orderingTool{call: func() { toolRan = true }}})
	llm := &scriptedLLM{responses: []provider.Response{{Blocks: []provider.Block{{Type: "tool_use", CallID: "1", Name: "ordered", Input: `{}`}}}}}
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Tools: executor, Model: "fake", Workdir: ".", Hooks: []hook.Hook{escalatingHook{}}}, "test", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if toolRan {
		t.Fatal("tool ran after escalation")
	}
	session, err := store.Session(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != event.StatusEscalated {
		t.Fatalf("status = %s", session.Status)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var ended event.SessionEnd
	if err := find(t, events, event.TypeSessionEnd).Decode(&ended); err != nil {
		t.Fatal(err)
	}
	if ended.Status != event.StatusEscalated {
		t.Fatalf("session.end = %#v", ended)
	}
}

type orderingHook struct{ call func() }

func (*orderingHook) Name() string                                    { return "ordering" }
func (h *orderingHook) BeforeTools(context.Context, *hook.Turn) error { h.call(); return nil }

type withholdingHook struct{}

func (withholdingHook) Name() string { return "withhold" }
func (withholdingHook) BeforeTools(_ context.Context, turn *hook.Turn) error {
	turn.ToolCalls = nil
	return nil
}

type escalatingHook struct{}

func (escalatingHook) Name() string { return "escalate" }
func (escalatingHook) BeforeTools(_ context.Context, turn *hook.Turn) error {
	turn.Cancel("health critically low")
	return nil
}

type orderingTool struct{ call func() }

func (orderingTool) Name() string                { return "ordered" }
func (orderingTool) Description() string         { return "records order" }
func (orderingTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t orderingTool) Run(context.Context, json.RawMessage) (tool.Result, error) {
	t.call()
	return tool.Result{Output: "ok"}, nil
}

func testRunCancelsStreamingProviderAndRecordsSession(t *testing.T) {
	store := openStore(t)
	llm := &cancellingLLM{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	type runResult struct {
		id  string
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		id, err := agent.Run(ctx, agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: "."}, "wait", func(string) {})
		done <- runResult{id: id, err: err}
	}()
	<-llm.started
	started := time.Now()
	cancel()
	var result runResult
	select {
	case result = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop within two seconds")
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("error = %v", result.err)
	}
	assertCancelledSession(t, store, result.id)
	events, err := store.List(t.Context(), result.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var response event.LLMResponse
	if err := find(t, events, event.TypeLLMResponse).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Cancelled {
		t.Fatalf("llm.response = %#v", response)
	}
}

func TestRunRecordsCancellationForEveryInFlightTool(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := openStore(t)
		started := make(chan struct{}, 2)
		llm := &scriptedLLM{responses: []provider.Response{{Blocks: []provider.Block{
			{Type: "tool_use", CallID: "one", Name: "wait", Input: "{}"},
			{Type: "tool_use", CallID: "two", Name: "wait", Input: "{}"},
		}, StopReason: "tool_calls", Model: "fake"}}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		type outcome struct {
			id  string
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			id, err := agent.Run(ctx, agent.Dependencies{
				Store: store, LLM: llm, Model: "fake", Workdir: ".",
				Tools: tool.NewExecutor([]tool.Tool{cancellationTool{started}}),
			}, "wait", func(string) {})
			done <- outcome{id, err}
		}()
		<-started
		<-started
		cancel()
		result := <-done
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("run error = %v", result.err)
		}
		assertCancelledSession(t, store, result.id)
		events, err := store.List(t.Context(), result.id, 1)
		if err != nil {
			t.Fatal(err)
		}
		cancelled := map[string]bool{}
		for _, e := range events {
			if e.Type != event.TypeToolResult {
				continue
			}
			var result event.ToolResult
			if err := e.Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.Cancelled && !result.TimedOut {
				cancelled[result.CallID] = true
			}
		}
		if !cancelled["one"] || !cancelled["two"] {
			t.Fatalf("cancelled tools = %v", cancelled)
		}
	})
}

type cancellationTool struct{ started chan<- struct{} }

func (cancellationTool) Name() string                { return "wait" }
func (cancellationTool) Description() string         { return "Wait until cancelled" }
func (cancellationTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (w cancellationTool) Run(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	w.started <- struct{}{}
	<-ctx.Done()
	return tool.Result{IsError: true}, ctx.Err()
}

func TestRunCancelsAllToolProcessGroupsAndRecordsSession(t *testing.T) {
	workdir := t.TempDir()
	store := openStore(t)
	inputs := []provider.Block{
		{Type: "tool_use", CallID: "one", Name: "bash", Input: `{"command":"echo $$ > one.pid; sleep 30"}`},
		{Type: "tool_use", CallID: "two", Name: "bash", Input: `{"command":"echo $$ > two.pid; sleep 30"}`},
	}
	llm := &scriptedLLM{responses: []provider.Response{{Blocks: inputs, StopReason: "tool_calls", Model: "fake"}}}
	executor := tool.NewExecutor([]tool.Tool{tool.NewBash(workdir)})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type runResult struct {
		id  string
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		id, err := agent.Run(ctx, agent.Dependencies{Store: store, LLM: llm, Tools: executor, Model: "fake", Workdir: workdir}, "wait", func(string) {})
		done <- runResult{id: id, err: err}
	}()
	pids := waitForPIDs(t, workdir, "one.pid", "two.pid")
	groups := make([]int, len(pids))
	for i, pid := range pids {
		group, err := syscall.Getpgid(pid)
		if err != nil {
			t.Fatalf("get process group for %d: %v", pid, err)
		}
		groups[i] = group
		t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
	}
	cancel()
	var result runResult
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("cancellation watchdog: agent did not finish cleanup and persistence; process groups=%v", groups)
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("error = %v", result.err)
	}
	assertCancelledSession(t, store, result.id)
	events, err := store.List(t.Context(), result.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var cancelled int
	for _, candidate := range events {
		if candidate.Type != event.TypeToolResult {
			continue
		}
		var payload event.ToolResult
		if err := candidate.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Cancelled && !payload.TimedOut {
			cancelled++
		}
	}
	if cancelled != 2 {
		t.Fatalf("cancelled tool results = %d", cancelled)
	}
	for _, group := range groups {
		if err := syscall.Kill(-group, 0); err == nil {
			t.Fatalf("process group %d still exists", group)
		}
	}
}

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

type cancellingLLM struct{ started chan struct{} }

func (f *cancellingLLM) Stream(ctx context.Context, _ provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	close(f.started)
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
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

func assertCancelledSession(t *testing.T, store *event.SQLiteStore, id string) {
	t.Helper()
	session, err := store.Session(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != event.StatusCancelled {
		t.Fatalf("session status = %s", session.Status)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var end event.SessionEnd
	if err := find(t, events, event.TypeSessionEnd).Decode(&end); err != nil {
		t.Fatal(err)
	}
	if end.Status != event.StatusCancelled {
		t.Fatalf("session.end status = %s", end.Status)
	}
}

func waitForPIDs(t *testing.T, workdir string, names ...string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	pids := make([]int, len(names))
	for time.Now().Before(deadline) {
		ready := true
		for i, name := range names {
			if pids[i] != 0 {
				continue
			}
			data, err := os.ReadFile(filepath.Join(workdir, name))
			if err != nil {
				ready = false
				continue
			}
			pids[i], _ = strconv.Atoi(strings.TrimSpace(string(data)))
			ready = ready && pids[i] != 0
		}
		if ready {
			return pids
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("tool processes did not start: %v", pids)
	return nil
}
