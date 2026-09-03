package tool_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/n1tishc/mulch/internal/tool"
)

func TestExecutorEmitsMatchingTimedEventsInCallOrder(t *testing.T) {
	synctest.Test(t, testExecutorEmitsMatchingTimedEventsInCallOrder)
}

func TestExecutorAppliesPerCallTimeoutWithFakeTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		executor := tool.NewExecutor([]tool.Tool{&blockingTool{name: "wait"}})
		executor.Timeout = 60 * time.Second
		outcomes := executor.RunAll(context.Background(), []tool.Call{{ID: "1", Name: "wait", Input: raw(`{}`)}}, nil)
		if len(outcomes) != 1 || !outcomes[0].Cancelled || !outcomes[0].Result.IsError {
			t.Fatalf("outcomes = %#v", outcomes)
		}
	})
}

func testExecutorEmitsMatchingTimedEventsInCallOrder(t *testing.T) {
	executor := tool.NewExecutor([]tool.Tool{
		&fakeTool{name: "slow", delay: 40 * time.Millisecond, result: tool.Result{Output: "first"}},
		&fakeTool{name: "fast", delay: 2 * time.Millisecond, result: tool.Result{Output: "second", SourceTS: time.Unix(10, 0)}},
	})
	executor.MaxPar = 1
	var emitted []tool.ExecutionEvent
	outcomes := executor.RunAll(t.Context(), []tool.Call{{ID: "1", Name: "slow", Input: raw(`{}`)}, {ID: "2", Name: "fast", Input: raw(`{}`)}}, func(e tool.ExecutionEvent) {
		emitted = append(emitted, e)
	})
	if len(outcomes) != 2 || outcomes[0].Call.ID != "1" || outcomes[1].Call.ID != "2" {
		t.Fatalf("outcomes = %#v", outcomes)
	}
	if outcomes[0].Duration != 40*time.Millisecond || outcomes[1].Duration != 2*time.Millisecond {
		t.Fatalf("durations include queue time: %s, %s", outcomes[0].Duration, outcomes[1].Duration)
	}
	starts, results := map[string]bool{}, map[string]tool.ExecutionEvent{}
	for _, e := range emitted {
		if e.Kind == tool.EventStart {
			starts[e.Call.ID] = true
		}
		if e.Kind == tool.EventResult {
			results[e.Call.ID] = e
		}
	}
	for _, id := range []string{"1", "2"} {
		if !starts[id] || results[id].Call.ID != id || results[id].Duration < 0 {
			t.Fatalf("events = %#v", emitted)
		}
	}
	if results["2"].Result.SourceTS.IsZero() {
		t.Fatalf("missing source timestamp: %#v", results["2"])
	}
}

type fakeTool struct {
	name   string
	delay  time.Duration
	result tool.Result
}

type blockingTool struct{ name string }

func (f *blockingTool) Name() string              { return f.name }
func (*blockingTool) Description() string         { return "wait" }
func (*blockingTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (*blockingTool) Run(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	<-ctx.Done()
	return tool.Result{Output: "timed out", IsError: true}, ctx.Err()
}

func (f *fakeTool) Name() string              { return f.name }
func (*fakeTool) Description() string         { return "fake" }
func (*fakeTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (f *fakeTool) Run(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	select {
	case <-time.After(f.delay):
		return f.result, nil
	case <-ctx.Done():
		return tool.Result{Output: "cancelled", IsError: true}, ctx.Err()
	}
}
