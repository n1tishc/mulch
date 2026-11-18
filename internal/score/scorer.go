package score

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

type Input struct {
	Session      event.Session
	Events       []event.Event
	Visible      []event.Event
	LastResponse provider.Response
	Now          time.Time
	Turn         int
}

type Result struct {
	Freshness string
	Score     float64
	Details   map[string]any
}

type Scorer interface {
	Name() string
	Score(context.Context, Input) (Result, error)
	Deadline() time.Duration
}

type Partial struct {
	Reason       string
	Name         string
	TimeoutMS    int64
	UsedPrevious bool
}

type CompositeResult struct {
	Freshness map[string]string
	Scores    map[string]float64
	Details   map[string]map[string]any
	Partials  []Partial
	Latency   time.Duration
}

type Composite struct {
	scorers  []Scorer
	previous map[string]float64
	mu       sync.Mutex
}

func NewComposite(scorers []Scorer, previous map[string]float64) *Composite {
	copyPrevious := make(map[string]float64, len(previous))
	for name, value := range previous {
		copyPrevious[name] = value
	}
	return &Composite{scorers: scorers, previous: copyPrevious}
}

func (c *Composite) Score(ctx context.Context, input Input) CompositeResult {
	started := time.Now()
	type outcome struct {
		name      string
		result    Result
		partial   *Partial
		available bool
	}
	outcomes := make(chan outcome, len(c.scorers))
	remaining := make(map[string]time.Duration, len(c.scorers))
	for _, scorer := range c.scorers {
		remaining[scorer.Name()] = scorer.Deadline()
		go func(s Scorer) {
			deadline := s.Deadline()
			scoreCtx, cancel := context.WithTimeout(ctx, deadline)
			defer cancel()
			type callResult struct {
				result Result
				err    error
			}
			called := make(chan callResult, 1)
			go func() { result, err := s.Score(scoreCtx, input); called <- callResult{result: result, err: err} }()
			var result Result
			var err error
			select {
			case completed := <-called:
				result, err = completed.result, completed.err
			case <-scoreCtx.Done():
				err = scoreCtx.Err()
			}
			if err == nil {
				outcomes <- outcome{name: s.Name(), result: result, available: true}
				return
			}
			c.mu.Lock()
			previous, ok := c.previous[s.Name()]
			c.mu.Unlock()
			partial := &Partial{Name: s.Name(), TimeoutMS: deadline.Milliseconds(), UsedPrevious: ok, Reason: failureReason(err)}
			if errors.Is(err, context.DeadlineExceeded) {
				partial.TimeoutMS = deadline.Milliseconds()
			}
			outcomes <- outcome{name: s.Name(), result: Result{Score: previous}, partial: partial, available: ok}
		}(scorer)
	}
	answer := CompositeResult{Scores: map[string]float64{}, Details: map[string]map[string]any{}, Freshness: map[string]string{}}
	for len(remaining) > 0 {
		var completed outcome
		select {
		case completed = <-outcomes:
			delete(remaining, completed.name)
		case <-ctx.Done():
			c.mu.Lock()
			for name, deadline := range remaining {
				previous, ok := c.previous[name]
				if ok {
					answer.Scores[name] = previous
				}
				state := "unavailable"
				if ok {
					state = "reused"
				}
				answer.Freshness[name] = state
				answer.Partials = append(answer.Partials, Partial{Name: name, TimeoutMS: deadline.Milliseconds(), UsedPrevious: ok, Reason: failureReason(ctx.Err())})
			}
			c.mu.Unlock()
			clear(remaining)
			continue
		}
		if completed.available {
			state := completed.result.Freshness
			if state == "" {
				state = "fresh"
			}
			answer.Freshness[completed.name] = state
			answer.Scores[completed.name] = completed.result.Score
			answer.Details[completed.name] = completed.result.Details
		}
		if completed.partial != nil {
			state := "unavailable"
			if completed.partial.UsedPrevious {
				state = "reused"
			}
			answer.Freshness[completed.name] = state
			answer.Partials = append(answer.Partials, *completed.partial)
		}
	}
	answer.Latency = time.Since(started)
	sort.Slice(answer.Partials, func(i, j int) bool { return answer.Partials[i].Name < answer.Partials[j].Name })
	c.mu.Lock()
	for name, value := range answer.Scores {
		// A placeholder that was never checked must not become the fallback a
		// later failure reuses; otherwise "reused" would launder "unverified".
		if answer.Freshness[name] == FreshnessUnverified {
			continue
		}
		c.previous[name] = value
	}
	c.mu.Unlock()
	return answer
}

// Freshness states recorded per dimension on each health observation.
const (
	FreshnessFresh         = "fresh"
	FreshnessReused        = "reused"
	FreshnessUnavailable   = "unavailable"
	FreshnessUnverified    = "unverified"
	FreshnessNotApplicable = "not_applicable"
)

// Measured reports whether a dimension in this freshness state carries a real
// observation that may contribute to composite health.
func Measured(freshness string) bool {
	return freshness != FreshnessUnverified && freshness != FreshnessUnavailable
}

var (
	errJudgeProvider  = errors.New("judge provider call failed")
	errJudgeTruncated = errors.New("judge response truncated at the output token limit")
	errJudgeEmpty     = errors.New("judge response contained no text")
)

func failureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, provider.ErrBudgetExhausted):
		return "budget_denied"
	case errors.Is(err, errJudgeTruncated):
		return "truncated"
	case errors.Is(err, errJudgeEmpty):
		return "empty_response"
	}
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &mismatch) {
		return "invalid_response"
	}
	if errors.Is(err, errJudgeProvider) {
		return "provider_error"
	}
	return "scorer_error"
}
