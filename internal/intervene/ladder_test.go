package intervene_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestLadderConfirmsLowRelevanceThenPrunesProtectedContext(t *testing.T) {
	store := &memoryStore{}
	ladder := intervene.New(store, intervene.Policy{WarnBelow: 80, PruneBelow: 65, ConfirmTurns: 2, CooldownTurns: 2, MaxPruneShare: .3})
	low := .2
	ladder.OnEvent(t.Context(), health(t, 4, 60, &low, nil, map[string]map[string]any{"relevance": {"lowest": []any{map[string]any{"seq": int64(3), "similarity": .1}, map[string]any{"seq": int64(4), "similarity": .2}}}}))
	turn := &hook.Turn{SessionID: "s", Turn: 5, Visible: visibleHistory(t), ToolCalls: []provider.Block{{Type: "tool_use"}}}
	if err := ladder.BeforeTools(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	if len(store.hidden) != 0 || store.lastType != event.TypeInterveneSkip {
		t.Fatalf("first decision hidden=%v type=%s", store.hidden, store.lastType)
	}
	ladder.OnEvent(t.Context(), health(t, 5, 59, &low, nil, map[string]map[string]any{"relevance": {"lowest": []any{map[string]any{"seq": int64(3), "similarity": .1}, map[string]any{"seq": int64(4), "similarity": .2}}}}))
	turn.Turn = 6
	if err := ladder.BeforeTools(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	if len(store.hidden) != 1 || store.hidden[0] != 3 {
		t.Fatalf("hidden = %v", store.hidden)
	}
	if store.lastType != event.TypeInterveneFire {
		t.Fatalf("event type = %s", store.lastType)
	}
	var fired event.InterveneFire
	if err := json.Unmarshal(store.lastPayload, &fired); err != nil {
		t.Fatal(err)
	}
	if fired.Action != "prune" || fired.TurnScored != 5 || fired.AppliedBeforeTurn != 6 {
		t.Fatalf("fire = %#v", fired)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("pending tools were not stopped: %v", turn.ToolCalls)
	}
}

func TestLadderRoutesLowCoherenceToReanchor(t *testing.T) {
	store := &memoryStore{}
	ladder := intervene.New(store, intervene.Policy{WarnBelow: 80, ReanchorBelow: 40, ConfirmTurns: 1})
	coh := .1
	ladder.OnEvent(t.Context(), health(t, 7, 30, nil, &coh, map[string]map[string]any{"coherence": {"contradictory_pairs": []any{map[string]any{"left_seq": 2, "right_seq": 5, "reason": "ports differ"}}}}))
	if err := ladder.BeforeTools(t.Context(), &hook.Turn{SessionID: "s", Turn: 8, Visible: visibleHistory(t)}); err != nil {
		t.Fatal(err)
	}
	if store.lastType != event.TypeInterveneFire || store.inject == "" {
		t.Fatalf("type=%s inject=%q", store.lastType, store.inject)
	}
}

func TestLadderRequestsCoherenceWhileConfirmingAndRestoresCooldown(t *testing.T) {
	store := &memoryStore{}
	requester := &requestCounter{}
	ladder := intervene.New(store, intervene.Policy{WarnBelow: 80, ConfirmTurns: 2, CooldownTurns: 2}, requester)
	low := .2
	ladder.OnEvent(t.Context(), health(t, 3, 60, &low, nil, nil))
	if err := ladder.BeforeTools(t.Context(), &hook.Turn{SessionID: "s", Turn: 4}); err != nil {
		t.Fatal(err)
	}
	if requester.calls != 1 {
		t.Fatalf("coherence requests = %d", requester.calls)
	}
	fired, _ := json.Marshal(event.InterveneFire{AppliedBeforeTurn: 4})
	resumed := intervene.New(store, intervene.Policy{WarnBelow: 80, ConfirmTurns: 1, CooldownTurns: 2})
	resumed.OnEvent(t.Context(), event.Event{Type: event.TypeInterveneFire, Payload: fired})
	resumed.OnEvent(t.Context(), health(t, 4, 50, &low, nil, nil))
	if err := resumed.BeforeTools(t.Context(), &hook.Turn{SessionID: "s", Turn: 5}); err != nil {
		t.Fatal(err)
	}
	if store.lastType != event.TypeInterveneSkip {
		t.Fatalf("resumed cooldown event = %s", store.lastType)
	}
}

type requestCounter struct{ calls int }

func (r *requestCounter) Request() { r.calls++ }

type memoryStore struct {
	hidden      []int64
	inject      string
	lastType    event.Type
	lastPayload json.RawMessage
}

func (s *memoryStore) Append(_ context.Context, e event.Event) (event.Event, error) {
	s.lastType = e.Type
	s.lastPayload = e.Payload
	if e.Type == event.TypeContextInject {
		var p event.ContextInject
		_ = json.Unmarshal(e.Payload, &p)
		s.inject = p.Text
	}
	return e, nil
}
func (s *memoryStore) SetVisibleBecause(_ context.Context, _ string, seqs []int64, _ bool, _, _ string) error {
	s.hidden = append(s.hidden, seqs...)
	return nil
}

func health(t *testing.T, turn int, composite float64, relevance, coherence *float64, details map[string]map[string]any) event.Event {
	t.Helper()
	b, _ := json.Marshal(event.ScoreHealth{TurnScored: turn, Composite: composite, Relevance: relevance, Coherence: coherence, Details: details})
	return event.Event{SessionID: "s", Turn: turn, Type: event.TypeScoreHealth, Payload: b}
}
func visibleHistory(t *testing.T) []event.Event {
	return []event.Event{
		text(t, 1, 0, event.TypeSystemPrompt, event.SystemPrompt{Text: "system"}), text(t, 2, 0, event.TypeUserMessage, event.UserMessage{Text: "original", Origin: "task"}),
		text(t, 3, 1, event.TypeToolResult, event.ToolResult{Output: "old distractor"}), text(t, 4, 2, event.TypeAssistantMessage, event.AssistantMessage{Text: "keep recent"}), text(t, 5, 3, event.TypeToolResult, event.ToolResult{Output: "keep latest"}),
	}
}
func text(t *testing.T, seq int64, turn int, typ event.Type, p any) event.Event {
	t.Helper()
	b, _ := json.Marshal(p)
	return event.Event{Seq: seq, Turn: turn, Type: typ, Payload: b, Visible: true}
}
