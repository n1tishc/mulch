package session_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

type recordingStore struct{ events []event.Event }

func (s *recordingStore) Append(_ context.Context, candidate event.Event) (event.Event, error) {
	candidate.Seq = int64(len(s.events) + 1)
	s.events = append(s.events, candidate)
	return candidate, nil
}

type twoTurnLLM struct{ calls atomic.Int32 }

func (l *twoTurnLLM) Stream(_ context.Context, _ provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	if l.calls.Add(1) == 1 {
		return provider.Response{Blocks: []provider.Block{{Type: "tool_use", CallID: "1", Name: "block", Input: `{}`}}}, nil
	}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "done"}}}, nil
}

type blockingTool struct {
	started chan struct{}
	release chan struct{}
}

func (b blockingTool) Name() string                { return "block" }
func (b blockingTool) Description() string         { return "blocks" }
func (b blockingTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (b blockingTool) Run(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	close(b.started)
	select {
	case <-b.release:
		return tool.Result{Output: "released"}, nil
	case <-ctx.Done():
		return tool.Result{}, ctx.Err()
	}
}

func TestSteerDuringToolBatchLandsAtFollowingBoundary(t *testing.T) {
	eventBus := bus.New()
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), eventBus)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	executor := tool.NewExecutor([]tool.Tool{blockingTool{started: started, release: release}})
	m := session.New(session.Options{Bus: eventBus, Close: store.Close, Run: func(ctx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		_, runErr := agent.RunSession(ctx, agent.Dependencies{Store: store, LLM: &twoTurnLLM{}, Tools: executor, Model: "fake", Workdir: t.TempDir(), Hooks: []hook.Hook{steering.Bind(store)}}, id, task, nil)
		return runErr
	}})
	defer m.Close()
	id, err := m.Start(t.Context(), "task")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	if err := m.Steer(id, "change direction"); err != nil {
		t.Fatal(err)
	}
	before, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range before {
		if candidate.Type == event.TypeUserMessage && candidate.Turn > 0 {
			t.Fatal("steer interrupted the active tool batch")
		}
	}
	close(release)
	if err := m.Wait(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	after, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	steerIndex, requestIndex := -1, -1
	for i, candidate := range after {
		if candidate.Type == event.TypeUserMessage && candidate.Turn == 2 {
			var msg event.UserMessage
			_ = candidate.Decode(&msg)
			if msg.Origin == "steer" {
				steerIndex = i
			}
		}
		if candidate.Type == event.TypeLLMRequest && candidate.Turn == 2 {
			requestIndex = i
		}
	}
	if steerIndex < 0 || requestIndex < 0 || steerIndex >= requestIndex {
		t.Fatalf("steer/request indexes = %d/%d", steerIndex, requestIndex)
	}
}

func TestSteeringHookCommitsSteerAtNextTurnBoundary(t *testing.T) {
	store := &recordingStore{}
	steering := (&session.Steering{}).Bind(store)
	steering.Add("finish tests first")
	if err := steering.BeforeTurn(t.Context(), &hook.Turn{SessionID: "s", Turn: 2}); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d", len(store.events))
	}
	got := store.events[0]
	if got.Type != event.TypeUserMessage || got.Turn != 2 || !got.Visible {
		t.Fatalf("event = %#v", got)
	}
	var payload event.UserMessage
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != "finish tests first" || payload.Origin != "steer" {
		t.Fatalf("payload = %#v", payload)
	}
}
