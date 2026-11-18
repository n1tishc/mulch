package intervene_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

type fixedScorer struct {
	name    string
	value   float64
	details map[string]any
}

func (s fixedScorer) Name() string            { return s.name }
func (s fixedScorer) Deadline() time.Duration { return time.Second }
func (s fixedScorer) Score(context.Context, score.Input) (score.Result, error) {
	return score.Result{Score: s.value, Details: s.details}, nil
}

// runnerHealth produces a health observation through the real score.Runner and
// default weights, so composites come from production arithmetic rather than
// hand-set numbers.
func runnerHealth(t *testing.T, degraded string, value float64, relevance bool) event.ScoreHealth {
	t.Helper()
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(t.Context(), event.Session{ID: "s", Task: "task", Model: "fake", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	names := []string{"saturation", "staleness", "coherence"}
	if relevance {
		names = append(names, "relevance")
	}
	var scorers []score.Scorer
	for _, name := range names {
		s := fixedScorer{name: name, value: 1}
		if name == degraded {
			s.value = value
		}
		if name == "relevance" {
			s.details = map[string]any{"lowest": []map[string]any{{"seq": 3, "similarity": s.value}}}
		}
		scorers = append(scorers, s)
	}
	runner := score.NewRunnerWithWeights(store, event.Session{ID: "s"}, scorers, score.DefaultWeights())
	payload, _ := json.Marshal(event.TurnCompleted{})
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 3, Type: event.TypeTurnCompleted, Payload: payload, CreatedAt: time.Now()})
	if err := runner.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), "s", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range events {
		if recorded.Type == event.TypeScoreHealth {
			var health event.ScoreHealth
			if err := recorded.Decode(&health); err != nil {
				t.Fatal(err)
			}
			return health
		}
	}
	t.Fatal("runner recorded no health")
	return event.ScoreHealth{}
}

// actionAfterConfirmation feeds the same observation on two consecutive turns
// (the default confirmation window) and reports the action taken before tools.
func actionAfterConfirmation(t *testing.T, policy intervene.Policy, health event.ScoreHealth) string {
	t.Helper()
	store := &memoryStore{}
	ladder := intervene.NewConfigured(store, policy).WithSummarizer(&fakeSummarizer{summary: "Paths: x.go. Decisions: keep. Open questions: none."})
	for i, turn := range []int{3, 4} {
		observed := health
		observed.TurnScored = turn
		b, _ := json.Marshal(observed)
		ladder.OnEvent(t.Context(), event.Event{SessionID: "s", Seq: int64(100 + i), Turn: turn, Type: event.TypeScoreHealth, Payload: b})
	}
	turn := &hook.Turn{SessionID: "s", Turn: 5, Visible: visibleHistory(t), ToolCalls: []provider.Block{{Type: "tool_use"}}, Cancel: func(string) {}}
	if err := ladder.BeforeTools(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	if store.lastType == event.TypeInterveneSkip {
		return "skip"
	}
	return store.lastFire.Action
}

// Each repair must be reachable when only its own dimension is badly degraded
// and every other dimension is healthy. The pilot showed the composite-only
// ladder could not do this: one collapsed dimension leaves composite between
// 70 and 80, so it can only warn.
func TestEachRepairIsReachableFromItsOwnDegradedDimension(t *testing.T) {
	cases := []struct {
		dimension string
		relevance bool
		want      string
	}{
		{"relevance", true, "prune"},
		{"coherence", true, "reanchor"},
		{"coherence", false, "reanchor"},
		{"staleness", true, "reanchor"},
		{"staleness", false, "reanchor"},
		{"saturation", true, "compact"},
		{"saturation", false, "compact"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/relevance=%t", tc.dimension, tc.relevance), func(t *testing.T) {
			health := runnerHealth(t, tc.dimension, .1, tc.relevance)
			if got := actionAfterConfirmation(t, intervene.DefaultPolicy(), health); got != tc.want {
				t.Fatalf("action = %q, want %q (composite %.1f)", got, tc.want, health.Composite)
			}
		})
	}
}

// The pilot's composite-only arm stays reproducible: the same single-dimension
// degradations can only warn (or not act) under it.
func TestPilotBaselinePolicyCannotRepairASingleDegradedDimension(t *testing.T) {
	baseline := baselinePolicy(t, 2)
	for _, dimension := range []string{"relevance", "coherence", "staleness", "saturation"} {
		for _, relevance := range []bool{true, false} {
			if dimension == "relevance" && !relevance {
				continue
			}
			health := runnerHealth(t, dimension, .1, relevance)
			if got := actionAfterConfirmation(t, baseline, health); got != "warn" && got != "skip" {
				t.Fatalf("%s/relevance=%t: baseline action = %q, want warn or skip", dimension, relevance, got)
			}
		}
	}
}

func coherenceHealth(t *testing.T, turn int, value float64, freshness string) event.Event {
	t.Helper()
	healthy := 1.0
	b, _ := json.Marshal(event.ScoreHealth{TurnScored: turn, Composite: 100 * (.3 + .2 + .2*value) / .7, Saturation: &healthy, Staleness: &healthy, Coherence: &value,
		Freshness: map[string]string{"saturation": "fresh", "staleness": "fresh", "coherence": freshness}})
	return event.Event{SessionID: "s", Seq: int64(turn), Turn: turn, Type: event.TypeScoreHealth, Payload: b}
}

// Coherence is judged every third turn, so a severe reading must request a
// fresh judgment to confirm; a failed recheck (reused value) must not confirm.
func TestSevereCoherenceRequestsFreshConfirmationBeforeReanchoring(t *testing.T) {
	for _, tc := range []struct {
		name, recheck string
		fires         bool
	}{{"fresh recheck confirms", "fresh", true}, {"failed recheck does not confirm", "reused", false}} {
		t.Run(tc.name, func(t *testing.T) {
			store, requester := &memoryStore{}, &requestCounter{}
			ladder := intervene.NewConfigured(store, intervene.DefaultPolicy(), requester)
			ladder.OnEvent(t.Context(), coherenceHealth(t, 5, 1, "reused"))
			ladder.OnEvent(t.Context(), coherenceHealth(t, 6, .2, "fresh"))
			first := &hook.Turn{SessionID: "s", Turn: 7, Visible: visibleHistory(t), ToolCalls: []provider.Block{{Type: "tool_use"}}}
			if err := ladder.BeforeTools(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			var skipped event.InterveneSkip
			_ = json.Unmarshal(store.lastPayload, &skipped)
			if store.lastType != event.TypeInterveneSkip || skipped.Reason != "awaiting fresh confirmation of severe coherence for reanchor" || requester.calls != 1 || len(first.ToolCalls) != 1 {
				t.Fatalf("turn 7: type=%s reason=%q requests=%d", store.lastType, skipped.Reason, requester.calls)
			}
			ladder.OnEvent(t.Context(), coherenceHealth(t, 7, .2, tc.recheck))
			second := &hook.Turn{SessionID: "s", Turn: 8, Visible: visibleHistory(t), ToolCalls: []provider.Block{{Type: "tool_use"}}}
			if err := ladder.BeforeTools(t.Context(), second); err != nil {
				t.Fatal(err)
			}
			fired := store.lastType == event.TypeInterveneFire && store.lastFire.Action == "reanchor"
			if fired != tc.fires {
				t.Fatalf("turn 8: fired=%t (type=%s action=%q), want %t", fired, store.lastType, store.lastFire.Action, tc.fires)
			}
			if !tc.fires && requester.calls != 2 {
				t.Fatalf("an unconfirmed severe reading should request another judgment; requests=%d", requester.calls)
			}
		})
	}
}
