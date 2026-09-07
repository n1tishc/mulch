package intervene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
)

// RaceStore is the durable seam needed to compare intervention candidates.
// Candidate execution is injected so this package remains independent of the
// agent and scoring packages.
type RaceStore interface {
	Store
	Branch(context.Context, string, int64) (event.Session, error)
	List(context.Context, string, int64) ([]event.Event, error)
	Visible(context.Context, string) ([]event.Event, error)
	EndSession(context.Context, string, event.Status) error
}

type CandidateRunner func(context.Context, event.Session, Action) (event.ScoreHealth, error)

type Race struct {
	store  RaceStore
	policy Policy
	run    CandidateRunner
	mu     sync.Mutex
	last   map[string]int
}

func NewRace(store RaceStore, policy Policy, run CandidateRunner) *Race {
	return &Race{store: store, policy: policy, run: run, last: map[string]int{}}
}

type raceOutcome struct {
	branch   event.Session
	action   Action
	health   event.ScoreHealth
	affected []int64
	err      error
}

// Compete forks two candidates, applies Prune and Reanchor, and mirrors the
// healthier candidate's context mutation into the parent as new events.
func (r *Race) Compete(ctx context.Context, turn *hook.Turn, health event.ScoreHealth) (bool, error) {
	r.mu.Lock()
	last := r.last[turn.SessionID]
	if last > 0 && turn.Turn-last <= 2*r.policy.CooldownTurns {
		r.mu.Unlock()
		return false, nil
	}
	r.last[turn.SessionID] = turn.Turn
	r.mu.Unlock()

	history, err := r.store.List(ctx, turn.SessionID, 1)
	if err != nil {
		return false, err
	}
	at := int64(0)
	for _, candidate := range history {
		if candidate.Seq > at {
			at = candidate.Seq
		}
	}
	first, err := r.store.Branch(ctx, turn.SessionID, at)
	if err != nil {
		return false, fmt.Errorf("race prune branch: %w", err)
	}
	second, err := r.store.Branch(ctx, turn.SessionID, at)
	if err != nil {
		_ = r.store.EndSession(context.WithoutCancel(ctx), first.ID, event.StatusAbandoned)
		return false, fmt.Errorf("race reanchor branch: %w", err)
	}
	branches := []event.Session{first, second}
	actions := []Action{ActionPrune, ActionReanchor}
	if err := r.append(ctx, turn.SessionID, turn.Turn, event.TypeRaceStart, event.RaceStart{Branches: []string{first.ID, second.ID}, Actions: []string{string(actions[0]), string(actions[1])}, AtSeq: at, Turn: turn.Turn}); err != nil {
		return false, err
	}
	for _, branch := range branches {
		for _, call := range turn.ToolCalls {
			if err := r.append(ctx, branch.ID, turn.Turn, event.TypeToolResult, event.ToolResult{CallID: call.CallID, Name: call.Name, Output: "tool call withheld by intervention race; reconsider using the candidate context", IsError: true, Cancelled: true}); err != nil {
				return false, err
			}
		}
	}

	results := make(chan raceOutcome, 2)
	for i := range branches {
		branch, action := branches[i], actions[i]
		go func() {
			visible, visibleErr := r.store.Visible(ctx, branch.ID)
			if visibleErr != nil {
				results <- raceOutcome{branch: branch, action: action, err: visibleErr}
				return
			}
			candidateTurn := &hook.Turn{SessionID: branch.ID, Turn: turn.Turn, Visible: visible}
			ladder := &Ladder{store: r.store, policy: r.policy}
			branchHealth := remapHealthSeqs(health, turn.Visible, visible)
			affected, applied, applyErr := ladder.apply(ctx, candidateTurn, branchHealth, action, "race candidate")
			if applyErr != nil {
				results <- raceOutcome{branch: branch, action: action, err: applyErr}
				return
			}
			_ = applied // an inapplicable prune is a valid unchanged-context candidate
			score, runErr := r.run(ctx, branch, action)
			results <- raceOutcome{branch: branch, action: action, health: score, affected: affected, err: runErr}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		_ = r.abandon(ctx, a.branch.ID, turn.Turn)
		_ = r.abandon(ctx, b.branch.ID, turn.Turn)
		return false, errors.Join(a.err, b.err)
	}
	winner, loser := a, b
	if b.health.Composite > a.health.Composite {
		winner, loser = b, a
	}
	if err := r.mirror(ctx, turn, winner); err != nil {
		return false, err
	}
	if err := r.abandon(ctx, loser.branch.ID, turn.Turn); err != nil {
		return false, err
	}
	scores := map[string]float64{a.branch.ID: a.health.Composite, b.branch.ID: b.health.Composite}
	return true, r.append(ctx, turn.SessionID, turn.Turn, event.TypeRaceEnd, event.RaceEnd{Branches: []string{first.ID, second.ID}, Scores: scores, Winner: winner.branch.ID, Loser: loser.branch.ID})
}

func remapHealthSeqs(health event.ScoreHealth, parent, child []event.Event) event.ScoreHealth {
	body, err := json.Marshal(health)
	var cloned event.ScoreHealth
	if err != nil || json.Unmarshal(body, &cloned) != nil {
		return health
	}
	health = cloned
	relevance := health.Details["relevance"]
	items, ok := relevance["lowest"].([]any)
	if !ok {
		return health
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parentSeq, ok := item["seq"].(float64)
		if !ok {
			continue
		}
		for _, source := range parent {
			if source.Seq != int64(parentSeq) {
				continue
			}
			for _, target := range child {
				if target.Type == source.Type && target.Turn == source.Turn && string(target.Payload) == string(source.Payload) {
					item["seq"] = float64(target.Seq)
					break
				}
			}
		}
	}
	return health
}

func (r *Race) mirror(ctx context.Context, turn *hook.Turn, winner raceOutcome) error {
	switch winner.action {
	case ActionPrune:
		// Branch sequence numbers can differ because only visible history is
		// copied. Match affected branch events to parent events by payload.
		child, err := r.store.List(ctx, winner.branch.ID, 1)
		if err != nil {
			return err
		}
		parentSeqs := make([]int64, 0, len(winner.affected))
		for _, childSeq := range winner.affected {
			for _, ce := range child {
				if ce.Seq != childSeq {
					continue
				}
				for _, pe := range turn.Visible {
					if pe.Type == ce.Type && string(pe.Payload) == string(ce.Payload) {
						parentSeqs = append(parentSeqs, pe.Seq)
						break
					}
				}
			}
		}
		if len(parentSeqs) == 0 {
			return r.append(ctx, turn.SessionID, turn.Turn, event.TypeContextVisibility, event.ContextVisibility{Reason: "healthier unchanged prune candidate", By: "intervention-race"})
		}
		return r.store.SetVisibleBecause(ctx, turn.SessionID, parentSeqs, false, "healthier race candidate", "intervention-race")
	case ActionReanchor:
		visible, err := r.store.Visible(ctx, winner.branch.ID)
		if err != nil {
			return err
		}
		for i := len(visible) - 1; i >= 0; i-- {
			if visible[i].Type != event.TypeContextInject {
				continue
			}
			var payload event.ContextInject
			if visible[i].Decode(&payload) != nil {
				continue
			}
			payload.By = "intervention-race"
			return r.append(ctx, turn.SessionID, turn.Turn, event.TypeContextInject, payload)
		}
		return errors.New("race winner had no reanchor context")
	default:
		return fmt.Errorf("unsupported race winner action %q", winner.action)
	}
}

func (r *Race) abandon(ctx context.Context, id string, turn int) error {
	if err := r.append(ctx, id, turn, event.TypeSessionEnd, event.SessionEnd{Status: event.StatusAbandoned, Turns: turn}); err != nil {
		return err
	}
	return r.store.EndSession(context.WithoutCancel(ctx), id, event.StatusAbandoned)
}

func (r *Race) append(ctx context.Context, id string, turn int, typ event.Type, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	visible := typ == event.TypeContextInject || typ == event.TypeToolResult
	_, err = r.store.Append(context.WithoutCancel(ctx), event.Event{SessionID: id, Turn: turn, Type: typ, Payload: body, Visible: visible})
	return err
}
