package score

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

type Store interface {
	Append(context.Context, event.Event) (event.Event, error)
}

type scoreJob struct {
	sessionID string
	turn      int
	input     Input
}

type Runner struct {
	store     Store
	session   event.Session
	composite *Composite
	jobs      chan scoreJob
	ctx       context.Context
	cancel    context.CancelFunc
	pending   sync.WaitGroup
	waiting   atomic.Bool
	closeOnce sync.Once
	mu        sync.Mutex
	events    []event.Event
	responses map[int]event.LLMResponse
	err       error
}

func NewRunner(store Store, session event.Session, scorers []Scorer, history ...event.Event) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	previous := map[string]float64{}
	for _, candidate := range history {
		if candidate.Type != event.TypeScoreHealth {
			continue
		}
		var health event.ScoreHealth
		if candidate.Decode(&health) != nil {
			continue
		}
		for name, value := range map[string]*float64{"saturation": health.Saturation, "staleness": health.Staleness, "relevance": health.Relevance, "coherence": health.Coherence} {
			if value != nil {
				previous[name] = *value
			}
		}
	}
	r := &Runner{store: store, session: session, composite: NewComposite(scorers, previous), jobs: make(chan scoreJob, 1), ctx: ctx, cancel: cancel, responses: map[int]event.LLMResponse{}}
	for _, candidate := range history {
		r.events = append(r.events, candidate)
		if candidate.Type == event.TypeLLMResponse {
			var response event.LLMResponse
			if candidate.Decode(&response) == nil {
				r.responses[candidate.Turn] = response
			}
		}
	}
	go r.run()
	return r
}

func (*Runner) Name() string                    { return "score" }
func (r *Runner) Publish(candidate event.Event) { r.OnEvent(context.Background(), candidate) }

func (r *Runner) OnEvent(_ context.Context, candidate event.Event) {
	r.mu.Lock()
	r.events = append(r.events, candidate)
	if candidate.Type == event.TypeContextVisibility {
		var visibility event.ContextVisibility
		if candidate.Decode(&visibility) == nil {
			for _, change := range visibility.Changes {
				for i := range r.events {
					if r.events[i].Seq == change.Seq {
						r.events[i].Visible = change.To
					}
				}
			}
		}
	}
	if candidate.Type == event.TypeLLMResponse {
		var response event.LLMResponse
		if candidate.Decode(&response) == nil {
			r.responses[candidate.Turn] = response
		}
	}
	if candidate.Type != event.TypeTurnCompleted {
		r.mu.Unlock()
		return
	}
	events := append([]event.Event(nil), r.events...)
	visible := make([]event.Event, 0, len(events))
	for _, recorded := range events {
		if recorded.Visible {
			visible = append(visible, recorded)
		}
	}
	response := r.responses[candidate.Turn]
	r.mu.Unlock()
	r.enqueue(scoreJob{sessionID: candidate.SessionID, turn: candidate.Turn, input: Input{Session: r.session, Events: events, Visible: visible, LastResponse: provider.Response{InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, Model: response.Model}, Now: candidate.CreatedAt, Turn: candidate.Turn}})
}

func (r *Runner) enqueue(job scoreJob) {
	r.pending.Add(1)
	for {
		select {
		case r.jobs <- job:
			return
		default:
		}
		select {
		case <-r.jobs:
			r.pending.Done()
		default:
		}
	}
}

func (r *Runner) Wait(ctx context.Context) error {
	r.waiting.Store(true)
	done := make(chan struct{})
	go func() { r.pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		r.cancel()
		<-done
	}
	r.closeOnce.Do(func() { r.cancel(); close(r.jobs) })
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(ctx.Err(), r.err)
}

func (r *Runner) run() {
	for job := range r.jobs {
		r.score(job)
		r.pending.Done()
	}
}

func (r *Runner) score(job scoreJob) {
	result := r.composite.Score(r.ctx, job.input)
	for _, partial := range result.Partials {
		r.append(job, event.TypeScorePartial, event.ScorePartial{Name: partial.Name, TimeoutMS: partial.TimeoutMS, UsedPrevious: partial.UsedPrevious})
	}
	health := event.ScoreHealth{TurnScored: job.turn, Details: result.Details, LatencyMS: result.Latency.Milliseconds(), OnCriticalPath: r.waiting.Load()}
	weights := map[string]float64{"saturation": .3, "staleness": .2, "relevance": .3, "coherence": .2}
	var weighted, totalWeight float64
	for name, value := range result.Scores {
		copy := value
		switch name {
		case "saturation":
			health.Saturation = &copy
		case "staleness":
			health.Staleness = &copy
		case "relevance":
			health.Relevance = &copy
		case "coherence":
			health.Coherence = &copy
		}
		if weight := weights[name]; weight > 0 {
			weighted += weight * value
			totalWeight += weight
		}
	}
	if totalWeight > 0 {
		health.Composite = 100 * weighted / totalWeight
	}
	r.append(job, event.TypeScoreHealth, health)
}

func (r *Runner) append(job scoreJob, typ event.Type, payload any) {
	b, err := json.Marshal(payload)
	if err == nil {
		_, err = r.store.Append(context.Background(), event.Event{SessionID: job.sessionID, Turn: job.turn, Type: typ, Payload: b, Visible: false})
	}
	if err != nil {
		r.mu.Lock()
		r.err = errors.Join(r.err, err)
		r.mu.Unlock()
	}
}
