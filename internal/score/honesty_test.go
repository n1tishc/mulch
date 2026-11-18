package score_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

// placeholderScorer returns the never-checked value a scorer reports before
// its first successful observation, then fails, like a judge that never answers.
type placeholderScorer struct {
	name  string
	calls *int
}

func (s placeholderScorer) Name() string            { return s.name }
func (s placeholderScorer) Deadline() time.Duration { return time.Second }
func (s placeholderScorer) Score(context.Context, score.Input) (score.Result, error) {
	*s.calls++
	if *s.calls == 1 {
		return score.Result{Score: 1, Freshness: score.FreshnessUnverified}, nil
	}
	return score.Result{}, errors.New("judge failed")
}

func TestUnverifiedPlaceholderNeverBecomesReusedFallback(t *testing.T) {
	calls := 0
	composite := score.NewComposite([]score.Scorer{placeholderScorer{name: "coherence", calls: &calls}}, nil)
	first := composite.Score(t.Context(), score.Input{})
	if first.Freshness["coherence"] != score.FreshnessUnverified {
		t.Fatalf("first freshness = %q", first.Freshness["coherence"])
	}
	second := composite.Score(t.Context(), score.Input{})
	if second.Freshness["coherence"] != score.FreshnessUnavailable {
		t.Fatalf("failed check after a placeholder must be unavailable, got %q", second.Freshness["coherence"])
	}
	if _, ok := second.Scores["coherence"]; ok {
		t.Fatalf("placeholder value was reused as a score: %#v", second.Scores)
	}
	if len(second.Partials) != 1 || second.Partials[0].UsedPrevious {
		t.Fatalf("partials = %#v", second.Partials)
	}
}

func healthFor(t *testing.T, scorers []score.Scorer, history ...event.Event) event.ScoreHealth {
	t.Helper()
	store, err := event.Open(t.Context(), t.TempDir()+"/events.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(t.Context(), event.Session{ID: "s", Task: "task", Model: "fake", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	runner := score.NewRunner(store, event.Session{ID: "s"}, scorers, history...)
	runner.OnEvent(t.Context(), event.Event{SessionID: "s", Turn: 2, Type: event.TypeTurnCompleted, Payload: payload(t, event.TurnCompleted{}), CreatedAt: time.Now()})
	if err := runner.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	events, err := store.List(t.Context(), "s", 1)
	if err != nil {
		t.Fatal(err)
	}
	var health event.ScoreHealth
	if err := findType(t, events, event.TypeScoreHealth).Decode(&health); err != nil {
		t.Fatal(err)
	}
	return health
}

func TestUnverifiedCoherenceDoesNotLiftCompositeHealth(t *testing.T) {
	// The pilot's failure mode: a judge that never succeeds kept coherence at
	// its initial 1.0, so the composite read healthier than the evidence.
	health := healthFor(t, []score.Scorer{
		fakeScorer{name: "saturation", value: .4, deadline: time.Second},
		score.NewCoherence(&judgeLLM{response: `{"pairs":[]}`}),
	})
	if health.Coherence != nil {
		t.Fatalf("unverified coherence was reported as a value: %v", *health.Coherence)
	}
	if health.Freshness["coherence"] != score.FreshnessUnverified {
		t.Fatalf("freshness = %#v", health.Freshness)
	}
	if health.Composite != 40 || health.Unmeasured {
		t.Fatalf("composite = %v unmeasured = %v, want saturation alone (40)", health.Composite, health.Unmeasured)
	}
}

func TestHealthWithoutMeasuredDimensionIsMarkedUnmeasured(t *testing.T) {
	health := healthFor(t, []score.Scorer{score.NewCoherence(&judgeLLM{response: `{"pairs":[]}`})})
	if !health.Unmeasured {
		t.Fatalf("health = %#v, want unmeasured rather than a zero composite", health)
	}
}

func TestRunnerDoesNotResumeFromUnverifiedHistory(t *testing.T) {
	placeholder := 1.0
	history := event.Event{SessionID: "s", Turn: 1, Type: event.TypeScoreHealth, Payload: payload(t, event.ScoreHealth{TurnScored: 1, Coherence: &placeholder, Freshness: map[string]string{"coherence": score.FreshnessUnverified}})}
	health := healthFor(t, []score.Scorer{
		fakeScorer{name: "saturation", value: .5, deadline: time.Second},
		fakeScorer{name: "coherence", err: errors.New("judge failed"), deadline: time.Second},
	}, history)
	if health.Coherence != nil || health.Freshness["coherence"] != score.FreshnessUnavailable {
		t.Fatalf("resumed an unverified placeholder: coherence=%v freshness=%#v", health.Coherence, health.Freshness)
	}
}

type scriptedJudge struct {
	response provider.Response
	err      error
}

func (j scriptedJudge) Stream(context.Context, provider.Request, chan<- provider.Delta) (provider.Response, error) {
	return j.response, j.err
}

func TestCoherenceJudgeFailuresAreCategorized(t *testing.T) {
	visible := []event.Event{
		textEvent(t, 1, 1, event.TypeAssistantMessage, event.AssistantMessage{Text: "config.go sets port 8080"}),
		textEvent(t, 2, 2, event.TypeContextInject, event.ContextInject{Text: "config.go sets port 9090"}),
	}
	text := func(s string) []provider.Block { return []provider.Block{{Type: "text", Text: s}} }
	for name, tc := range map[string]struct {
		judge provider.LLM
		want  string
	}{
		"truncated":      {scriptedJudge{response: provider.Response{Blocks: text(`{"pairs":[{"left_seq":1`), StopReason: "length"}}, "truncated"},
		"empty_response": {scriptedJudge{response: provider.Response{}}, "empty_response"},
		"invalid":        {scriptedJudge{response: provider.Response{Blocks: text("both statements agree")}}, "invalid_response"},
		"provider_error": {scriptedJudge{err: errors.New("502 bad gateway")}, "provider_error"},
		"budget_denied":  {scriptedJudge{err: fmt.Errorf("evaluation %w", provider.ErrBudgetExhausted)}, "budget_denied"},
		"timeout":        {scriptedJudge{err: fmt.Errorf("stream: %w", context.DeadlineExceeded)}, "timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			composite := score.NewComposite([]score.Scorer{score.NewCoherence(tc.judge)}, nil)
			got := composite.Score(t.Context(), score.Input{Turn: 3, Visible: visible})
			if len(got.Partials) != 1 || got.Partials[0].Reason != tc.want {
				t.Fatalf("partials = %#v, want reason %q", got.Partials, tc.want)
			}
		})
	}
}

func TestCoherenceAcceptsFencedJudgeJSON(t *testing.T) {
	judge := &judgeLLM{response: "```json\n{\"pairs\":[{\"left_seq\":1,\"right_seq\":2,\"verdict\":\"contradictory\",\"reason\":\"ports differ\"}]}\n```"}
	result, err := score.NewCoherence(judge).Score(t.Context(), score.Input{Turn: 3, Visible: []event.Event{
		textEvent(t, 1, 1, event.TypeAssistantMessage, event.AssistantMessage{Text: "config.go sets port 8080"}),
		textEvent(t, 2, 2, event.TypeContextInject, event.ContextInject{Text: "config.go sets port 9090"}),
	}})
	if err != nil || result.Score != 0 {
		t.Fatalf("score = %v err = %v", result.Score, err)
	}
}
