package score_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/n1tishc/mulch/internal/score"
)

func TestCompositeRecordsFailureCategoriesAndFreshness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var invalid any
		badJSON := json.Unmarshal([]byte("invalid"), &invalid)
		c := score.NewComposite([]score.Scorer{
			fakeScorer{name: "fresh", value: .8, deadline: time.Second},
			fakeScorer{name: "reused", err: badJSON, deadline: time.Second},
			fakeScorer{name: "missing", err: context.Canceled, deadline: time.Second},
			fakeScorer{name: "timeout", delay: time.Second, deadline: time.Millisecond},
		}, map[string]float64{"reused": .5})
		got := c.Score(t.Context(), score.Input{})
		if got.Freshness["fresh"] != "fresh" || got.Freshness["reused"] != "reused" || got.Freshness["missing"] != "unavailable" {
			t.Fatal(got.Freshness)
		}
		reasons := map[string]string{}
		for _, partial := range got.Partials {
			reasons[partial.Name] = partial.Reason
		}
		if reasons["reused"] != "invalid_response" || reasons["missing"] != "cancelled" || reasons["timeout"] != "timeout" {
			t.Fatal(reasons)
		}
	})
}

type fakeScorer struct {
	name            string
	delay, deadline time.Duration
	value           float64
	err             error
}

type stubbornScorer struct {
	name            string
	delay, deadline time.Duration
}

func (s stubbornScorer) Name() string            { return s.name }
func (s stubbornScorer) Deadline() time.Duration { return s.deadline }
func (s stubbornScorer) Score(context.Context, score.Input) (score.Result, error) {
	<-time.After(s.delay)
	return score.Result{Score: 1}, nil
}

func (s fakeScorer) Name() string            { return s.name }
func (s fakeScorer) Deadline() time.Duration { return s.deadline }
func (s fakeScorer) Score(ctx context.Context, _ score.Input) (score.Result, error) {
	select {
	case <-time.After(s.delay):
		return score.Result{Score: s.value}, s.err
	case <-ctx.Done():
		return score.Result{}, ctx.Err()
	}
}

func TestCompositeFansOutAndReusesPreviousScoreOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		composite := score.NewComposite([]score.Scorer{
			fakeScorer{name: "fast", delay: 10 * time.Millisecond, deadline: time.Second, value: .8},
			fakeScorer{name: "slow", delay: 50 * time.Millisecond, deadline: time.Second, value: .4},
			fakeScorer{name: "failed", delay: 20 * time.Millisecond, deadline: time.Second, err: errors.New("nope")},
		}, map[string]float64{"failed": .6})
		started := time.Now()
		result := composite.Score(t.Context(), score.Input{})
		if elapsed := time.Since(started); elapsed != 50*time.Millisecond {
			t.Fatalf("elapsed = %s, want slowest scorer duration", elapsed)
		}
		if result.Scores["fast"] != .8 || result.Scores["slow"] != .4 || result.Scores["failed"] != .6 {
			t.Fatalf("scores = %#v", result.Scores)
		}
		if len(result.Partials) != 1 || result.Partials[0].Name != "failed" || !result.Partials[0].UsedPrevious {
			t.Fatalf("partials = %#v", result.Partials)
		}
	})
}

func TestCompositeTimesOutScorersIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		composite := score.NewComposite([]score.Scorer{
			fakeScorer{name: "timeout", delay: time.Second, deadline: 25 * time.Millisecond},
			fakeScorer{name: "ok", delay: 40 * time.Millisecond, deadline: time.Second, value: .9},
		}, nil)
		started := time.Now()
		result := composite.Score(t.Context(), score.Input{})
		if elapsed := time.Since(started); elapsed != 40*time.Millisecond {
			t.Fatalf("elapsed = %s", elapsed)
		}
		if len(result.Partials) != 1 || result.Partials[0].TimeoutMS != 25 || result.Partials[0].UsedPrevious {
			t.Fatalf("partials = %#v", result.Partials)
		}
	})
}

func TestCompositeEnforcesDeadlineWhenScorerIgnoresContext(t *testing.T) {
	composite := score.NewComposite([]score.Scorer{stubbornScorer{name: "stuck", delay: 100 * time.Millisecond, deadline: 5 * time.Millisecond}}, nil)
	started := time.Now()
	result := composite.Score(t.Context(), score.Input{})
	if elapsed := time.Since(started); elapsed >= 50*time.Millisecond {
		t.Fatalf("elapsed = %s", elapsed)
	}
	if len(result.Partials) != 1 || result.Partials[0].Name != "stuck" {
		t.Fatalf("partials = %#v", result.Partials)
	}
}
