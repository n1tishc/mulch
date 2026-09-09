package intervene_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
)

// Real SQLite branching renumbers visible history. Repeated payloads must not
// cause the selected repair to hide a different event in the parent.
func TestRacePrunesExactOccurrenceAndReplaysFromSQLite(t *testing.T) {
	for _, hiddenTarget := range []bool{false, true} {
		name := "selected occurrence"
		if hiddenTarget {
			name = "stale reference"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "race.db")
			store, err := event.Open(t.Context(), path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			if err := store.CreateSession(t.Context(), event.Session{ID: "parent", Task: "retain the task", Model: "fake", Workdir: "."}); err != nil {
				t.Fatal(err)
			}
			appendEvent := func(turn int, typ event.Type, payload any) event.Event {
				body, _ := json.Marshal(payload)
				e, err := store.Append(t.Context(), event.Event{SessionID: "parent", Turn: turn, Type: typ, Payload: body, Visible: true})
				if err != nil {
					t.Fatal(err)
				}
				return e
			}
			appendEvent(0, event.TypeUserMessage, event.UserMessage{Text: "retain the task", Origin: "task"})
			first := appendEvent(1, event.TypeAssistantMessage, event.AssistantMessage{Text: "duplicate"})
			target := appendEvent(1, event.TypeAssistantMessage, event.AssistantMessage{Text: "duplicate"})
			appendEvent(3, event.TypeAssistantMessage, event.AssistantMessage{Text: "recent"})
			appendEvent(3, event.TypeAssistantMessage, event.AssistantMessage{Text: "more recent"})
			if hiddenTarget {
				if err := store.SetVisibleBecause(t.Context(), "parent", []int64{target.Seq}, false, "previous repair", "test"); err != nil {
					t.Fatal(err)
				}
			}
			visible, err := store.Visible(t.Context(), "parent")
			if err != nil {
				t.Fatal(err)
			}
			race := intervene.NewRace(store, intervene.DefaultPolicy(), func(_ context.Context, child event.Session, action intervene.Action) (event.ScoreHealth, error) {
				score := 70.0
				if action == intervene.ActionPrune {
					score = 90
				}
				return event.ScoreHealth{Composite: score}, nil
			})
			h := event.ScoreHealth{Composite: 40, TurnScored: 3, Details: map[string]map[string]any{"relevance": {"lowest": []map[string]any{{"seq": target.Seq, "similarity": 0.1}}}}}
			if _, err := race.Compete(t.Context(), &hook.Turn{SessionID: "parent", Turn: 4, Visible: visible}, h); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = event.Open(t.Context(), path, nil)
			if err != nil {
				t.Fatal(err)
			}
			history, err := store.List(t.Context(), "parent", 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range history {
				if e.Seq == first.Seq && !e.Visible {
					t.Fatal("race hid the first duplicate instead of selected occurrence")
				}
				if e.Seq == target.Seq && e.Visible {
					t.Fatal("race failed to hide the selected occurrence")
				}
			}
			if !hasType(history, event.TypeRaceEnd) || !hasType(history, event.TypeContextVisibility) {
				t.Fatal("race decision was not durable")
			}
		})
	}
}
