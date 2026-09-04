package score_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/tool"
)

func TestRunnerPersistsPartialAndHealthForCompletedTurn(t *testing.T) {
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(t.Context(), event.Session{ID: "s", Task: "task", Model: "fake", Workdir: ".", ContextWindow: 1000}); err != nil {
		t.Fatal(err)
	}
	runner := score.NewRunner(store, event.Session{ID: "s", ContextWindow: 1000}, []score.Scorer{
		fakeScorer{name: "saturation", value: .8, deadline: time.Second},
		fakeScorer{name: "staleness", err: context.DeadlineExceeded, deadline: 5 * time.Millisecond},
	})
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 3, Type: event.TypeLLMResponse, Payload: payload(t, event.LLMResponse{InputTokens: 700})})
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 3, Type: event.TypeTurnCompleted, Payload: payload(t, event.TurnCompleted{}), CreatedAt: time.Now()})
	if err := runner.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), "s", 1)
	if err != nil {
		t.Fatal(err)
	}
	var partial event.ScorePartial
	if err := findType(t, events, event.TypeScorePartial).Decode(&partial); err != nil {
		t.Fatal(err)
	}
	if partial.Name != "staleness" || partial.UsedPrevious {
		t.Fatalf("partial = %#v", partial)
	}
	var health event.ScoreHealth
	if err := findType(t, events, event.TypeScoreHealth).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.TurnScored != 3 || health.Saturation == nil || health.Staleness != nil || health.LatencyMS < 0 {
		t.Fatalf("health = %#v", health)
	}
}

func TestRunnerCancelsAndPersistsFinalScoreAtWaitDeadline(t *testing.T) {
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(t.Context(), event.Session{ID: "s", Task: "task", Model: "fake", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	runner := score.NewRunner(store, event.Session{ID: "s"}, []score.Scorer{fakeScorer{name: "saturation", delay: time.Hour, deadline: time.Hour}})
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 1, Type: event.TypeTurnCompleted, Payload: payload(t, event.TurnCompleted{}), CreatedAt: time.Now()})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := runner.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v", err)
	}
	events, err := store.List(t.Context(), "s", 1)
	if err != nil {
		t.Fatal(err)
	}
	if findType(t, events, event.TypeScoreHealth).Seq == 0 || findType(t, events, event.TypeScorePartial).Seq == 0 {
		t.Fatal("final score was not durable")
	}
}

func TestRunnerRestoresPreviousScoreFromResumedHistory(t *testing.T) {
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(t.Context(), event.Session{ID: "s", Task: "task", Model: "fake", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	previous := .73
	history := event.Event{SessionID: "s", Turn: 1, Type: event.TypeScoreHealth, Payload: payload(t, event.ScoreHealth{TurnScored: 1, Saturation: &previous})}
	runner := score.NewRunner(store, event.Session{ID: "s"}, []score.Scorer{fakeScorer{name: "saturation", err: errors.New("failed"), deadline: time.Second}}, history)
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 2, Type: event.TypeTurnCompleted, Payload: payload(t, event.TurnCompleted{}), CreatedAt: time.Now()})
	if err := runner.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), "s", 1)
	if err != nil {
		t.Fatal(err)
	}
	var partial event.ScorePartial
	if err := findType(t, events, event.TypeScorePartial).Decode(&partial); err != nil {
		t.Fatal(err)
	}
	var health event.ScoreHealth
	if err := findType(t, events, event.TypeScoreHealth).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if !partial.UsedPrevious || health.Saturation == nil || *health.Saturation != previous {
		t.Fatalf("partial=%#v health=%#v", partial, health)
	}
}

func TestTenTurnScoringOverlapsFollowingModelRequest(t *testing.T) {
	forward := &forwardPublisher{}
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", forward)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secondRequest := make(chan struct{})
	llm := &tenTurnLLM{secondRequest: secondRequest}
	runner := score.NewRunner(store, event.Session{ContextWindow: 1000}, []score.Scorer{overlapScorer{secondRequest: secondRequest}})
	forward.set(runner)
	executor := tool.NewExecutor([]tool.Tool{instantTool{}})
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Tools: executor, Model: "fake", Workdir: ".", MaxTurns: 10, ContextWindow: 1000, Hooks: []hook.Hook{runner}}, "work", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var health int
	finalScored := false
	for _, candidate := range events {
		if candidate.Type == event.TypeScoreHealth {
			health++
			var payload event.ScoreHealth
			if candidate.Decode(&payload) == nil && payload.TurnScored == 10 {
				finalScored = true
			}
		}
	}
	if health < 2 || !finalScored {
		t.Fatalf("health events = %d, final scored = %v", health, finalScored)
	}
}

type forwardPublisher struct {
	mu     sync.RWMutex
	target event.Publisher
}

func (p *forwardPublisher) set(target event.Publisher) { p.mu.Lock(); p.target = target; p.mu.Unlock() }
func (p *forwardPublisher) Publish(candidate event.Event) {
	p.mu.RLock()
	target := p.target
	p.mu.RUnlock()
	if target != nil {
		target.Publish(candidate)
	}
}

type overlapScorer struct{ secondRequest <-chan struct{} }

func (overlapScorer) Name() string            { return "saturation" }
func (overlapScorer) Deadline() time.Duration { return time.Second }
func (s overlapScorer) Score(ctx context.Context, input score.Input) (score.Result, error) {
	if input.LastResponse.InputTokens == 1 {
		select {
		case <-s.secondRequest:
		case <-ctx.Done():
			return score.Result{}, ctx.Err()
		}
	}
	return score.Result{Score: 1}, nil
}

type tenTurnLLM struct {
	calls         int
	secondRequest chan struct{}
}

func (l *tenTurnLLM) Stream(_ context.Context, _ provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	l.calls++
	if l.calls == 2 {
		close(l.secondRequest)
	}
	if l.calls == 10 {
		return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "done"}}, InputTokens: l.calls, Model: "fake"}, nil
	}
	return provider.Response{Blocks: []provider.Block{{Type: "tool_use", CallID: fmt.Sprintf("c%d", l.calls), Name: "instant", Input: `{}`}}, InputTokens: l.calls, Model: "fake"}, nil
}

type instantTool struct{}

func (instantTool) Name() string                { return "instant" }
func (instantTool) Description() string         { return "instant" }
func (instantTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (instantTool) Run(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Output: "ok"}, nil
}

func findType(t *testing.T, events []event.Event, typ event.Type) event.Event {
	t.Helper()
	for _, candidate := range events {
		if candidate.Type == typ {
			return candidate
		}
	}
	t.Fatalf("missing %s", typ)
	return event.Event{}
}
