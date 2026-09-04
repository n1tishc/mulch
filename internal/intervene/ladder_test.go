package intervene_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestLadderCompactsOldSaturatedHistoryWithoutDeletingIt(t *testing.T) {
	store := &memoryStore{}
	summarizer := &fakeSummarizer{summary: "Paths: internal/agent/loop.go. Decision: preserve the log. Open questions: none."}
	ladder := intervene.New(store, intervene.Policy{WarnBelow: 80, CompactBelow: 50, ConfirmTurns: 1}).WithSummarizer(summarizer)
	saturation := .1
	ladder.OnEvent(t.Context(), healthScores(t, 7, 45, &saturation, nil, nil, nil))
	visible := append(visibleHistory(t), text(t, 6, 4, event.TypeAssistantMessage, event.AssistantMessage{Text: "another old decision"}), text(t, 7, 7, event.TypeToolResult, event.ToolResult{Output: "latest"}))
	turn := &hook.Turn{SessionID: "s", Turn: 8, Visible: visible, ToolCalls: []provider.Block{{Type: "tool_use"}}}
	if err := ladder.BeforeTools(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	if len(summarizer.events) == 0 || len(store.hidden) == 0 {
		t.Fatalf("summarized=%d hidden=%v", len(summarizer.events), store.hidden)
	}
	if store.compact.Summary != summarizer.summary || len(store.compact.ReplacedSeqs) != len(store.hidden) {
		t.Fatalf("compact = %#v", store.compact)
	}
	if store.lastFire.Action != "compact" || len(turn.ToolCalls) != 0 {
		t.Fatalf("fire=%#v tools=%v", store.lastFire, turn.ToolCalls)
	}
}

func TestLadderEscalatesCriticalHealthBeforeTools(t *testing.T) {
	store := &memoryStore{}
	ladder := intervene.New(store, intervene.Policy{WarnBelow: 80, EscalateBelow: 25, ConfirmTurns: 1})
	ladder.OnEvent(t.Context(), healthScores(t, 3, 20, nil, nil, nil, nil))
	reason := ""
	turn := &hook.Turn{SessionID: "s", Turn: 4, ToolCalls: []provider.Block{{Type: "tool_use"}}, Cancel: func(got string) { reason = got }}
	if err := ladder.BeforeTools(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	if reason == "" || store.lastFire.Action != "escalate" || len(turn.ToolCalls) != 0 {
		t.Fatalf("reason=%q fire=%#v tools=%v", reason, store.lastFire, turn.ToolCalls)
	}
}

func TestSyntheticSessionExercisesEveryLadderAction(t *testing.T) {
	low := .1
	cases := []struct {
		name   string
		score  event.ScoreHealth
		action string
	}{
		{"warn", event.ScoreHealth{TurnScored: 3, Composite: 75, Saturation: &low}, "warn"},
		{"prune", event.ScoreHealth{TurnScored: 3, Composite: 60, Relevance: &low, Details: map[string]map[string]any{"relevance": {"lowest": []any{map[string]any{"seq": int64(3), "similarity": .1}}}}}, "prune"},
		{"compact", event.ScoreHealth{TurnScored: 3, Composite: 45, Saturation: &low}, "compact"},
		{"reanchor", event.ScoreHealth{TurnScored: 3, Composite: 35, Coherence: &low}, "reanchor"},
		{"escalate", event.ScoreHealth{TurnScored: 3, Composite: 20}, "escalate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &memoryStore{}
			ladder := intervene.New(store, intervene.Policy{ConfirmTurns: 1}).WithSummarizer(&fakeSummarizer{summary: "Paths: x.go. Decisions: keep. Open questions: none."})
			encoded, _ := json.Marshal(tc.score)
			ladder.OnEvent(t.Context(), event.Event{SessionID: "synthetic", Turn: 3, Type: event.TypeScoreHealth, Payload: encoded})
			turn := &hook.Turn{SessionID: "synthetic", Turn: 4, Visible: visibleHistory(t), ToolCalls: []provider.Block{{Type: "tool_use"}}, Cancel: func(string) {}}
			if err := ladder.BeforeTools(t.Context(), turn); err != nil {
				t.Fatal(err)
			}
			if store.lastFire.Action != tc.action {
				t.Fatalf("action = %q, want %q", store.lastFire.Action, tc.action)
			}
		})
	}
}

func TestSyntheticSessionLogReconstructsContextAfterEveryAction(t *testing.T) {
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const id = "synthetic-ladder"
	if err := store.CreateSession(t.Context(), event.Session{ID: id, Task: "preserve the event log", Model: "fake", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	appendEvent := func(turn int, typ event.Type, value any) event.Event {
		encoded, _ := json.Marshal(value)
		got, appendErr := store.Append(t.Context(), event.Event{SessionID: id, Turn: turn, Type: typ, Payload: encoded, Visible: true})
		if appendErr != nil {
			t.Fatal(appendErr)
		}
		return got
	}
	appendEvent(0, event.TypeSystemPrompt, event.SystemPrompt{Text: "system"})
	appendEvent(0, event.TypeUserMessage, event.UserMessage{Text: "preserve the event log", Origin: "task"})
	appendEvent(1, event.TypeAssistantMessage, event.AssistantMessage{Text: "old path internal/agent/loop.go"})
	prunable := appendEvent(2, event.TypeAssistantMessage, event.AssistantMessage{Text: "irrelevant distraction"})
	appendEvent(3, event.TypeAssistantMessage, event.AssistantMessage{Text: "recent work"})

	apply := func(turn int, score event.ScoreHealth) (bool, []provider.Message) {
		ladder := intervene.New(store, intervene.Policy{ConfirmTurns: 1}).WithSummarizer(&fakeSummarizer{summary: "Paths: internal/agent/loop.go. Decisions: preserve the event log. Open questions: none."})
		encoded, _ := json.Marshal(score)
		ladder.OnEvent(t.Context(), event.Event{SessionID: id, Turn: score.TurnScored, Type: event.TypeScoreHealth, Payload: encoded})
		visible, visibleErr := store.Visible(t.Context(), id)
		if visibleErr != nil {
			t.Fatal(visibleErr)
		}
		cancelled := false
		if err := ladder.BeforeTools(t.Context(), &hook.Turn{SessionID: id, Turn: turn, Visible: visible, ToolCalls: []provider.Block{{Type: "tool_use"}}, Cancel: func(string) { cancelled = true }}); err != nil {
			t.Fatal(err)
		}
		all, listErr := store.List(t.Context(), id, 1)
		if listErr != nil {
			t.Fatal(listErr)
		}
		visible, visibleErr = store.Visible(t.Context(), id)
		if visibleErr != nil {
			t.Fatal(visibleErr)
		}
		seqs := make([]int64, len(visible))
		for i := range visible {
			seqs[i] = visible[i].Seq
		}
		messages, buildErr := event.BuildMessages(all, seqs)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		return cancelled, messages
	}
	joined := func(messages []provider.Message) string {
		var parts []string
		for _, message := range messages {
			for _, block := range message.Blocks {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	low := .1
	_, messages := apply(4, event.ScoreHealth{TurnScored: 3, Composite: 75, Saturation: &low})
	if !strings.Contains(joined(messages), "Context health warning") {
		t.Fatal("warn injection was not reconstructable")
	}
	_, messages = apply(6, event.ScoreHealth{
		TurnScored: 5, Composite: 60, Relevance: &low,
		Details: map[string]map[string]any{
			"relevance": {"lowest": []any{map[string]any{"seq": prunable.Seq, "similarity": .1}}},
		},
	})
	if strings.Contains(joined(messages), "irrelevant distraction") {
		t.Fatal("pruned event remained model-visible")
	}
	_, messages = apply(8, event.ScoreHealth{TurnScored: 7, Composite: 45, Saturation: &low})
	if strings.Contains(joined(messages), "old path") || !strings.Contains(joined(messages), "Compacted context:") {
		t.Fatal("compaction replacement was not reconstructable")
	}
	_, messages = apply(10, event.ScoreHealth{TurnScored: 9, Composite: 35, Coherence: &low})
	if !strings.Contains(joined(messages), "Reanchor on the original task: preserve the event log") {
		t.Fatal("reanchor was not reconstructable")
	}
	cancelled, messages := apply(12, event.ScoreHealth{TurnScored: 11, Composite: 20})
	if !cancelled || len(messages) == 0 {
		t.Fatal("escalation terminal snapshot was not reconstructable")
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
	compact     event.ContextCompact
	lastFire    event.InterveneFire
}

func (s *memoryStore) Append(_ context.Context, e event.Event) (event.Event, error) {
	s.lastType = e.Type
	s.lastPayload = e.Payload
	if e.Type == event.TypeContextInject {
		var p event.ContextInject
		_ = json.Unmarshal(e.Payload, &p)
		s.inject = p.Text
	}
	if e.Type == event.TypeContextCompact {
		_ = json.Unmarshal(e.Payload, &s.compact)
	}
	if e.Type == event.TypeInterveneFire {
		_ = json.Unmarshal(e.Payload, &s.lastFire)
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

func healthScores(t *testing.T, turn int, composite float64, saturation, staleness, relevance, coherence *float64) event.Event {
	t.Helper()
	b, _ := json.Marshal(event.ScoreHealth{TurnScored: turn, Composite: composite, Saturation: saturation, Staleness: staleness, Relevance: relevance, Coherence: coherence})
	return event.Event{SessionID: "s", Turn: turn, Type: event.TypeScoreHealth, Payload: b}
}

type fakeSummarizer struct {
	summary string
	events  []event.Event
}

func (f *fakeSummarizer) Summarize(_ context.Context, events []event.Event) (string, int, error) {
	f.events = append([]event.Event(nil), events...)
	return f.summary, 12, nil
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
