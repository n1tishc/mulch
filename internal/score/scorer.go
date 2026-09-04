package score

import (
	"context"
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
}

type Result struct {
	Score   float64
	Details map[string]any
}

type Scorer interface {
	Name() string
	Score(context.Context, Input) (Result, error)
	Deadline() time.Duration
}

type Partial struct {
	Name         string
	TimeoutMS    int64
	UsedPrevious bool
}

type CompositeResult struct {
	Scores   map[string]float64
	Details  map[string]map[string]any
	Partials []Partial
	Latency  time.Duration
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
			partial := &Partial{Name: s.Name(), TimeoutMS: deadline.Milliseconds(), UsedPrevious: ok}
			if errors.Is(err, context.DeadlineExceeded) {
				partial.TimeoutMS = deadline.Milliseconds()
			}
			outcomes <- outcome{name: s.Name(), result: Result{Score: previous}, partial: partial, available: ok}
		}(scorer)
	}
	answer := CompositeResult{Scores: map[string]float64{}, Details: map[string]map[string]any{}}
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
				answer.Partials = append(answer.Partials, Partial{Name: name, TimeoutMS: deadline.Milliseconds(), UsedPrevious: ok})
			}
			c.mu.Unlock()
			clear(remaining)
			continue
		}
		if completed.available {
			answer.Scores[completed.name] = completed.result.Score
			answer.Details[completed.name] = completed.result.Details
		}
		if completed.partial != nil {
			answer.Partials = append(answer.Partials, *completed.partial)
		}
	}
	answer.Latency = time.Since(started)
	sort.Slice(answer.Partials, func(i, j int) bool { return answer.Partials[i].Name < answer.Partials[j].Name })
	c.mu.Lock()
	for name, value := range answer.Scores {
		c.previous[name] = value
	}
	c.mu.Unlock()
	return answer
}
