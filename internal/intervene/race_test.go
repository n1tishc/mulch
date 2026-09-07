package intervene_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestRaceRunsCandidatesConcurrentlyAndRetainsHealthierContext(t *testing.T) {
	store := newRaceStore()
	started := make(chan intervene.Action, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	runs := map[intervene.Action]int{}
	policy := intervene.DefaultPolicy()
	policy.CooldownTurns = 2
	race := intervene.NewRace(store, policy, func(ctx context.Context, child event.Session, action intervene.Action) (event.ScoreHealth, error) {
		mu.Lock()
		runs[action]++
		mu.Unlock()
		started <- action
		<-release
		if action == intervene.ActionReanchor {
			return event.ScoreHealth{Composite: 91}, nil
		}
		return event.ScoreHealth{Composite: 62}, nil
	})
	turn := &hook.Turn{SessionID: "parent", Turn: 4, Visible: store.visible["parent"], ToolCalls: []provider.Block{{Type: "tool_use", CallID: "pending", Name: "read"}}}
	done := make(chan error, 1)
	go func() { _, err := race.Compete(t.Context(), turn, raceHealth()); done <- err }()
	<-started
	<-started // both candidates reached the runner before either was released
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if runs[intervene.ActionPrune] != 1 || runs[intervene.ActionReanchor] != 1 {
		t.Fatalf("candidate runs = %#v", runs)
	}
	if !hasType(store.events["prune"], event.TypeToolResult) || !hasType(store.events["reanchor"], event.TypeToolResult) {
		t.Fatal("candidate history retained an unresolved parent tool call")
	}
	if !hasType(store.visible["prune"], event.TypeToolResult) || !hasType(store.visible["reanchor"], event.TypeToolResult) {
		t.Fatal("candidate tool result was not visible to the provider")
	}
	visible := store.visible["prune"]
	seqs := make([]int64, len(visible))
	for i := range visible {
		seqs[i] = visible[i].Seq
	}
	messages, err := event.BuildMessages(visible, seqs)
	if err != nil {
		t.Fatal(err)
	}
	if messages[len(messages)-1].Role != provider.RoleTool {
		t.Fatalf("last candidate message role = %q, want tool", messages[len(messages)-1].Role)
	}
	if store.status["prune"] != event.StatusAbandoned {
		t.Fatalf("loser status = %q", store.status["prune"])
	}
	if !hasType(store.events["parent"], event.TypeRaceStart) || !hasType(store.events["parent"], event.TypeRaceEnd) || !hasType(store.events["parent"], event.TypeContextInject) {
		t.Fatalf("parent event types = %#v", eventTypes(store.events["parent"]))
	}
	var ended event.RaceEnd
	for _, candidate := range store.events["parent"] {
		if candidate.Type == event.TypeRaceEnd {
			_ = candidate.Decode(&ended)
		}
	}
	if ended.Winner != "reanchor" || ended.Loser != "prune" {
		t.Fatalf("race result = %#v", ended)
	}
}

func TestRaceUsesDoubleInterventionCooldown(t *testing.T) {
	store := newRaceStore()
	policy := intervene.DefaultPolicy()
	policy.CooldownTurns = 2
	race := intervene.NewRace(store, policy, func(context.Context, event.Session, intervene.Action) (event.ScoreHealth, error) {
		return event.ScoreHealth{Composite: 80}, nil
	})
	turn := &hook.Turn{SessionID: "parent", Turn: 4, Visible: store.visible["parent"]}
	if _, err := race.Compete(t.Context(), turn, raceHealth()); err != nil {
		t.Fatal(err)
	}
	turn.Turn = 8
	if _, err := race.Compete(t.Context(), turn, raceHealth()); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, candidate := range store.events["parent"] {
		if candidate.Type == event.TypeRaceStart {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("race starts during doubled cooldown = %d", starts)
	}
	turn.Turn = 9
	if _, err := race.Compete(t.Context(), turn, raceHealth()); err != nil {
		t.Fatal(err)
	}
	starts = 0
	for _, candidate := range store.events["parent"] {
		if candidate.Type == event.TypeRaceStart {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("race starts after cooldown = %d", starts)
	}
}

type raceStore struct {
	mu      sync.Mutex
	events  map[string][]event.Event
	visible map[string][]event.Event
	status  map[string]event.Status
}

func newRaceStore() *raceStore {
	task, _ := json.Marshal(event.UserMessage{Text: "test task", Origin: "task"})
	old, _ := json.Marshal(event.AssistantMessage{Text: "old context"})
	call, _ := json.Marshal(event.AssistantToolCall{CallID: "pending", Name: "read", Input: json.RawMessage(`{}`)})
	return &raceStore{events: map[string][]event.Event{}, visible: map[string][]event.Event{"parent": {{SessionID: "parent", Seq: 1, Type: event.TypeUserMessage, Payload: task, Visible: true}, {SessionID: "parent", Seq: 2, Turn: 1, Type: event.TypeAssistantMessage, Payload: old, Visible: true}, {SessionID: "parent", Seq: 3, Turn: 3, Type: event.TypeAssistantMessage, Payload: old, Visible: true}, {SessionID: "parent", Seq: 4, Turn: 4, Type: event.TypeAssistantToolCall, Payload: call, Visible: true}}}, status: map[string]event.Status{}}
}
func raceHealth() event.ScoreHealth {
	return event.ScoreHealth{TurnScored: 3, Composite: 40, Details: map[string]map[string]any{"relevance": {"lowest": []map[string]any{{"seq": int64(2), "similarity": 0.1}}}}}
}
func (s *raceStore) Append(_ context.Context, e event.Event) (event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.Seq = int64(len(s.events[e.SessionID]) + 10)
	s.events[e.SessionID] = append(s.events[e.SessionID], e)
	if e.Visible {
		s.visible[e.SessionID] = append(s.visible[e.SessionID], e)
	}
	return e, nil
}
func (s *raceStore) SetVisibleBecause(_ context.Context, id string, seqs []int64, visible bool, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.visible[id] {
		for _, seq := range seqs {
			if s.visible[id][i].Seq == seq {
				s.visible[id][i].Visible = visible
			}
		}
	}
	return nil
}
func (s *raceStore) Branch(_ context.Context, _ string, _ int64) (event.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "prune"
	if _, ok := s.visible[id]; ok {
		id = "reanchor"
	}
	copied := append([]event.Event(nil), s.visible["parent"]...)
	for i := range copied {
		copied[i].SessionID = id
	}
	s.visible[id] = copied
	return event.Session{ID: id, Task: "test task"}, nil
}
func (s *raceStore) Visible(_ context.Context, id string) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.visible[id]...), nil
}
func (s *raceStore) List(_ context.Context, id string, _ int64) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "parent" {
		return append([]event.Event(nil), s.visible[id]...), nil
	}
	return append([]event.Event(nil), s.events[id]...), nil
}
func (s *raceStore) EndSession(_ context.Context, id string, status event.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[id] = status
	return nil
}
func hasType(es []event.Event, typ event.Type) bool {
	for _, e := range es {
		if e.Type == typ {
			return true
		}
	}
	return false
}
func eventTypes(es []event.Event) []event.Type {
	out := make([]event.Type, len(es))
	for i, e := range es {
		out[i] = e.Type
	}
	return out
}
