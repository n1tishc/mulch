// Package runtime wires one execution policy for CLI, daemon and evaluation.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/tool"
)

type Mode string

const (
	Plain   Mode = "plain"
	Observe Mode = "control"
	Repair  Mode = "intervention"
	Race    Mode = "race"
)

var Modes = []Mode{Plain, Observe, Repair, Race}

func ValidMode(m Mode) bool {
	for _, candidate := range Modes {
		if m == candidate {
			return true
		}
	}
	return false
}

// Router unregisters finished sessions so long-lived daemons do not retain scorers.
type Router struct {
	mu        sync.RWMutex
	listeners map[string][]hook.OnEvent
}

func (r *Router) Publish(e event.Event) {
	r.mu.RLock()
	targets := append([]hook.OnEvent(nil), r.listeners[e.SessionID]...)
	r.mu.RUnlock()
	for _, target := range targets {
		target.OnEvent(context.Background(), e)
	}
}
func (r *Router) bind(id string, targets []hook.OnEvent) func() {
	r.mu.Lock()
	if r.listeners == nil {
		r.listeners = map[string][]hook.OnEvent{}
	}
	r.listeners[id] = targets
	r.mu.Unlock()
	return func() { r.mu.Lock(); delete(r.listeners, id); r.mu.Unlock() }
}

type Config struct {
	IsolatedPrompt bool
	Store          *event.SQLiteStore
	Router         *Router
	// Every provider call is created through this seam, including candidate/judge/summary calls.
	LLM                     func(sessionID, role string) provider.LLM
	Scorers                 func(*score.Coherence) []score.Scorer
	Weights                 score.Weights
	Policy                  intervene.Policy
	Model, JudgeModel       string
	ContextWindow, MaxTurns int
	Mode                    Mode
}

func (c Config) Execute(ctx context.Context, recorded event.Session, resume bool, task string, extra []hook.Hook, emit func(string)) error {
	release, err := c.Store.AcquireExecution(ctx, recorded.ID)
	if err != nil {
		return err
	}
	defer release()
	if !ValidMode(c.Mode) {
		return fmt.Errorf("unknown runtime mode %q", c.Mode)
	}
	if c.Mode != Plain && c.Mode != Observe {
		if err := c.Policy.Validate(); err != nil {
			return err
		}
	}
	factory := c.LLM
	c.LLM = func(id, role string) provider.LLM { return provider.WithSession(factory(id, role), id) }
	var history []event.Event
	if resume {
		var err error
		history, err = c.Store.List(ctx, recorded.ID, 1)
		if err != nil {
			return err
		}
	}
	if c.JudgeModel == "" {
		c.JudgeModel = c.Model
	}
	metadata := &runMetadata{config: c}
	hs := append([]hook.Hook{metadata}, extra...)
	var listeners []hook.OnEvent
	if c.Mode != Plain {
		judgeModel := c.JudgeModel
		if judgeModel == "" {
			judgeModel = c.Model
		}
		coherence := score.NewCoherenceWithModel(c.LLM(recorded.ID, "judge"), judgeModel)
		scorers := []score.Scorer{score.Saturation{}, score.Staleness{}, coherence}
		if c.Scorers != nil {
			scorers = c.Scorers(coherence)
		}
		for _, scorer := range scorers {
			metadata.scorers = append(metadata.scorers, scorer.Name())
		}
		scoring := score.NewRunnerWithWeights(c.Store, recorded, scorers, c.Weights, history...)
		hs = append(hs, scoring)
		listeners = append(listeners, scoring)
		if c.Mode != Observe {
			ladder := intervene.NewConfigured(c.Store, c.Policy, coherence).WithSession(recorded.ID).WithSummarizer(intervene.NewLLMSummarizer(c.LLM(recorded.ID, "summary"), judgeModel))
			for _, e := range history {
				ladder.OnEvent(ctx, e)
			}
			if c.Mode == Race {
				ladder.WithRace(intervene.NewRace(c.Store, c.Policy, c.candidate))
			}
			hs = append(hs, ladder)
			listeners = append(listeners, ladder)
		}
	}
	unbind := c.Router.bind(recorded.ID, listeners)
	defer unbind()
	ex := tool.NewExecutor([]tool.Tool{tool.NewRead(recorded.Workdir), tool.NewWrite(recorded.Workdir), tool.NewEdit(recorded.Workdir), tool.NewBash(recorded.Workdir)})
	deps := agent.Dependencies{IsolatedPrompt: c.IsolatedPrompt, Store: c.Store, LLM: c.LLM(recorded.ID, "agent"), Tools: ex, Model: c.Model, Workdir: recorded.Workdir, ContextWindow: c.ContextWindow, MaxTurns: c.MaxTurns, Hooks: hs}
	if resume {
		_, err = agent.Resume(ctx, deps, recorded.ID, task, emit)
	} else {
		_, err = agent.RunSession(ctx, deps, recorded.ID, task, emit)
	}
	return err
}

type runMetadata struct {
	config  Config
	scorers []string
	written bool
}

func (*runMetadata) Name() string { return "run-config" }
func (m *runMetadata) BeforeTurn(ctx context.Context, turn *hook.Turn) error {
	if m.written {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"version": 1, "mode": m.config.Mode, "model": m.config.Model, "judge_model": m.config.JudgeModel, "policy": m.config.Policy, "weights": m.config.Weights, "scorers": m.scorers, "context_window": m.config.ContextWindow})
	if err != nil {
		return err
	}
	_, err = m.config.Store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeRunConfig, Payload: payload})
	m.written = err == nil
	return err
}
func (c Config) candidate(ctx context.Context, child event.Session, _ intervene.Action) (event.ScoreHealth, error) {
	wd, cleanup, err := snapshotWorkdir(child.Workdir)
	if err != nil {
		return event.ScoreHealth{}, err
	}
	defer cleanup()
	child.Workdir = wd
	candidate := c
	candidate.Mode = Observe
	candidate.MaxTurns = 1
	factory := c.LLM
	candidate.LLM = func(id, role string) provider.LLM { return factory(id, "candidate-"+role) }
	runErr := candidate.Execute(ctx, child, true, "", nil, nil)
	es, err := c.Store.List(context.WithoutCancel(ctx), child.ID, 1)
	if err != nil {
		return event.ScoreHealth{}, err
	}
	var health event.ScoreHealth
	found := false
	for _, e := range es {
		if e.Type == event.TypeScoreHealth && e.Decode(&health) == nil {
			found = true
		}
	}
	if !found {
		return health, errors.Join(runErr, errors.New("race candidate produced no health score"))
	}
	// A tool-using candidate deliberately exhausts its one-turn budget.
	if runErr != nil && !errors.Is(runErr, agent.ErrMaxTurns) {
		return health, runErr
	}
	return health, nil
}
