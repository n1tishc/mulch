package intervene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
)

type Policy struct {
	WarnBelow     float64 `json:"warn_below"`
	PruneBelow    float64 `json:"prune_below"`
	CompactBelow  float64 `json:"compact_below"`
	ReanchorBelow float64 `json:"reanchor_below"`
	EscalateBelow float64 `json:"escalate_below"`
	Hysteresis    float64 `json:"hysteresis"`
	CooldownTurns int     `json:"cooldown_turns"`
	ConfirmTurns  int     `json:"confirm_turns"`
	MaxPruneShare float64 `json:"max_prune_share"`
}

func DefaultPolicy() Policy {
	return Policy{WarnBelow: 80, PruneBelow: 65, CompactBelow: 50, ReanchorBelow: 40, EscalateBelow: 25, Hysteresis: 5, CooldownTurns: 2, ConfirmTurns: 2, MaxPruneShare: .3}
}

type Store interface {
	Append(context.Context, event.Event) (event.Event, error)
	SetVisibleBecause(context.Context, string, []int64, bool, string, string) error
}
type healthRecord struct{ health event.ScoreHealth }
type CoherenceRequester interface{ Request() }
type Summarizer interface {
	Summarize(context.Context, []event.Event) (summary string, tokens int, err error)
}

type Action string

const (
	ActionWarn     Action = "warn"
	ActionPrune    Action = "prune"
	ActionCompact  Action = "compact"
	ActionReanchor Action = "reanchor"
	ActionEscalate Action = "escalate"
)

type Ladder struct {
	store       Store
	policy      Policy
	mu          sync.Mutex
	history     []healthRecord
	lastApplied int
	coherence   CoherenceRequester
	summarizer  Summarizer
	race        *Race
	sessionID   string
}

func (l *Ladder) WithSummarizer(s Summarizer) *Ladder { l.summarizer = s; return l }
func (l *Ladder) WithRace(r *Race) *Ladder            { l.race = r; return l }
func (l *Ladder) WithSession(id string) *Ladder       { l.sessionID = id; return l }

func New(store Store, policy Policy, requester ...CoherenceRequester) *Ladder {
	d := DefaultPolicy()
	if policy.WarnBelow != 0 {
		d.WarnBelow = policy.WarnBelow
	}
	if policy.PruneBelow != 0 {
		d.PruneBelow = policy.PruneBelow
	}
	if policy.CompactBelow != 0 {
		d.CompactBelow = policy.CompactBelow
	}
	if policy.ReanchorBelow != 0 {
		d.ReanchorBelow = policy.ReanchorBelow
	}
	if policy.EscalateBelow != 0 {
		d.EscalateBelow = policy.EscalateBelow
	}
	if policy.Hysteresis != 0 {
		d.Hysteresis = policy.Hysteresis
	}
	if policy.CooldownTurns != 0 {
		d.CooldownTurns = policy.CooldownTurns
	}
	if policy.ConfirmTurns != 0 {
		d.ConfirmTurns = policy.ConfirmTurns
	}
	if policy.MaxPruneShare != 0 {
		d.MaxPruneShare = policy.MaxPruneShare
	}
	return newLadder(store, d, requester...)
}

// NewConfigured uses a complete, validated policy without treating zero values as omitted.
func NewConfigured(store Store, policy Policy, requester ...CoherenceRequester) *Ladder {
	return newLadder(store, policy, requester...)
}

func newLadder(store Store, policy Policy, requester ...CoherenceRequester) *Ladder {
	ladder := &Ladder{store: store, policy: policy}
	if len(requester) > 0 {
		ladder.coherence = requester[0]
	}
	return ladder
}
func (*Ladder) Name() string                    { return "intervention-ladder" }
func (l *Ladder) Publish(candidate event.Event) { l.OnEvent(context.Background(), candidate) }
func (l *Ladder) OnEvent(_ context.Context, candidate event.Event) {
	if l.sessionID != "" && candidate.SessionID != l.sessionID {
		return
	}
	if candidate.Type == event.TypeInterveneFire {
		var fired event.InterveneFire
		if candidate.Decode(&fired) == nil {
			l.mu.Lock()
			if fired.AppliedBeforeTurn > l.lastApplied {
				l.lastApplied = fired.AppliedBeforeTurn
			}
			l.mu.Unlock()
		}
		return
	}
	if candidate.Type != event.TypeScoreHealth {
		return
	}
	var health event.ScoreHealth
	if candidate.Decode(&health) != nil {
		return
	}
	l.mu.Lock()
	l.history = append(l.history, healthRecord{health})
	if len(l.history) > 3 {
		l.history = l.history[len(l.history)-3:]
	}
	l.mu.Unlock()
}
func (l *Ladder) BeforeTools(ctx context.Context, turn *hook.Turn) error {
	l.mu.Lock()
	history := append([]healthRecord(nil), l.history...)
	lastApplied := l.lastApplied
	l.mu.Unlock()
	if len(history) == 0 {
		return l.skip(ctx, turn, 0, "no health score available")
	}
	latest := history[len(history)-1].health
	if turn.Turn-latest.TurnScored < 1 || turn.Turn-latest.TurnScored > 2 {
		return l.skip(ctx, turn, latest.TurnScored, "score is too old")
	}
	if lastApplied > 0 && turn.Turn-lastApplied <= l.policy.CooldownTurns {
		action, _ := l.route(latest)
		return l.skip(ctx, turn, latest.TurnScored, "intervention cooldown skipped "+string(action))
	}
	confirm := l.policy.ConfirmTurns
	if confirm < 1 {
		confirm = 1
	}
	if len(history) < confirm {
		if l.coherence != nil {
			l.coherence.Request()
		}
		action, _ := l.route(latest)
		return l.skip(ctx, turn, latest.TurnScored, "awaiting score confirmation for "+string(action))
	}
	threshold := l.policy.WarnBelow
	if lastApplied > 0 {
		threshold -= l.policy.Hysteresis
	}
	window := history[len(history)-confirm:]
	for _, record := range window {
		if record.health.Composite >= threshold {
			weakest := window[0].health
			for _, candidate := range window[1:] {
				if candidate.health.Composite < weakest.Composite {
					weakest = candidate.health
				}
			}
			action, _ := l.route(weakest)
			return l.skip(ctx, turn, latest.TurnScored, "health recovered within confirmation window; skipped "+string(action))
		}
	}
	action, reason := l.route(latest)
	if l.race != nil && action != ActionWarn {
		raced, raceErr := l.race.Compete(ctx, turn, latest)
		if raceErr != nil {
			return fmt.Errorf("race interventions: %w", raceErr)
		}
		if !raced {
			return l.skip(ctx, turn, latest.TurnScored, "race cooldown skipped "+string(action))
		}
		turn.ToolCalls = nil
		if err := l.append(ctx, turn, event.TypeInterveneFire, event.InterveneFire{Action: string(action), Reason: reason + "; selected by race", TurnScored: latest.TurnScored, AppliedBeforeTurn: turn.Turn}); err != nil {
			return err
		}
		l.mu.Lock()
		l.lastApplied = turn.Turn
		l.mu.Unlock()
		return nil
	}
	affected, applied, err := l.apply(ctx, turn, latest, action, reason)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	turn.ToolCalls = nil
	if err = l.append(ctx, turn, event.TypeInterveneFire, event.InterveneFire{Action: string(action), Reason: reason, TurnScored: latest.TurnScored, AppliedBeforeTurn: turn.Turn, AffectedSeqs: affected}); err != nil {
		return err
	}
	if action == ActionEscalate && turn.Cancel != nil {
		turn.Cancel(reason)
	}
	l.mu.Lock()
	l.lastApplied = turn.Turn
	l.mu.Unlock()
	return nil
}

func (l *Ladder) route(h event.ScoreHealth) (Action, string) {
	if h.Composite < l.policy.EscalateBelow {
		return ActionEscalate, fmt.Sprintf("health is critically low at %.0f", h.Composite)
	}
	weak, value := "composite", h.Composite/100
	for _, score := range []struct {
		name  string
		value *float64
	}{{"saturation", h.Saturation}, {"staleness", h.Staleness}, {"relevance", h.Relevance}, {"coherence", h.Coherence}} {
		name, candidate := score.name, score.value
		if candidate != nil && *candidate < value {
			weak, value = name, *candidate
		}
	}
	switch weak {
	case "relevance":
		if h.Composite >= l.policy.PruneBelow {
			return ActionWarn, fmt.Sprintf("relevance is %.2f", value)
		}
		return ActionPrune, fmt.Sprintf("relevance is %.2f", value)
	case "coherence":
		if h.Composite >= l.policy.ReanchorBelow {
			return ActionWarn, fmt.Sprintf("coherence is %.2f", value)
		}
		return ActionReanchor, fmt.Sprintf("coherence is %.2f", value)
	case "staleness":
		if h.Composite >= l.policy.ReanchorBelow {
			return ActionWarn, fmt.Sprintf("staleness is %.2f", value)
		}
		return ActionReanchor, fmt.Sprintf("staleness is %.2f", value)
	case "saturation":
		if h.Composite < l.policy.CompactBelow && l.summarizer != nil {
			return ActionCompact, fmt.Sprintf("saturation is %.2f", value)
		}
		return ActionWarn, fmt.Sprintf("saturation is %.2f", value)
	}
	return ActionWarn, fmt.Sprintf("health is %.0f", h.Composite)
}
func (l *Ladder) apply(ctx context.Context, turn *hook.Turn, h event.ScoreHealth, action Action, reason string) ([]int64, bool, error) {
	switch action {
	case ActionEscalate:
		return nil, true, nil
	case ActionCompact:
		seqs, span, tokensBefore := compactCandidates(turn.Visible)
		if len(seqs) == 0 {
			return nil, false, l.skip(ctx, turn, h.TurnScored, "no eligible context to compact")
		}
		summary, tokensAfter, err := l.summarizer.Summarize(ctx, span)
		if err != nil {
			return nil, false, fmt.Errorf("summarize compacted context: %w", err)
		}
		if summary == "" {
			return nil, false, l.skip(ctx, turn, h.TurnScored, "compaction produced an empty summary")
		}
		if err := l.store.SetVisibleBecause(ctx, turn.SessionID, seqs, false, reason, l.Name()); err != nil {
			return nil, false, err
		}
		payload, _ := json.Marshal(event.ContextCompact{ReplacedSeqs: seqs, Summary: summary, TokensBefore: tokensBefore, TokensAfter: tokensAfter, By: l.Name()})
		_, err = l.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeContextCompact, Payload: payload, Visible: true})
		if err != nil {
			rollbackErr := l.store.SetVisibleBecause(ctx, turn.SessionID, seqs, true, "compaction replacement failed", l.Name())
			return nil, false, errors.Join(err, rollbackErr)
		}
		return seqs, true, err
	case ActionPrune:
		seqs := pruneCandidates(turn.Visible, h, l.policy.MaxPruneShare)
		if len(seqs) == 0 {
			return nil, false, l.skip(ctx, turn, h.TurnScored, "no eligible context to prune")
		}
		return seqs, true, l.store.SetVisibleBecause(ctx, turn.SessionID, seqs, false, reason, l.Name())
	case ActionReanchor:
		task := ""
		for _, candidate := range turn.Visible {
			if candidate.Type == event.TypeUserMessage {
				var p event.UserMessage
				if candidate.Decode(&p) == nil && p.Origin == "task" {
					task = p.Text
					break
				}
			}
		}
		note := "Reanchor on the original task: " + task + ". Resolve conflicting context before continuing."
		if pairs := h.Details["coherence"]["contradictory_pairs"]; pairs != nil {
			note += fmt.Sprintf(" Contradictions: %v.", pairs)
		}
		payload, _ := json.Marshal(event.ContextInject{Reason: reason, Text: note, By: l.Name()})
		_, err := l.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeContextInject, Payload: payload, Visible: true})
		return nil, true, err
	default:
		note := "Context health warning: " + reason + ". Verify the current task and evidence before using tools."
		payload, _ := json.Marshal(event.ContextInject{Reason: reason, Text: note, By: l.Name()})
		_, err := l.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeContextInject, Payload: payload, Visible: true})
		return nil, true, err
	}
}
func compactCandidates(visible []event.Event) ([]int64, []event.Event, int) {
	latest := 0
	for _, candidate := range visible {
		if candidate.Turn > latest {
			latest = candidate.Turn
		}
	}
	var eligible []event.Event
	var turns []int
	seenTurn := map[int]bool{}
	for _, candidate := range visible {
		if candidate.Type == event.TypeSystemPrompt || candidate.Type == event.TypeUserMessage || candidate.Type == event.TypeContextCompact || candidate.Turn >= latest-1 {
			continue
		}
		eligible = append(eligible, candidate)
		if !seenTurn[candidate.Turn] {
			seenTurn[candidate.Turn] = true
			turns = append(turns, candidate.Turn)
		}
	}
	if len(eligible) == 0 {
		return nil, nil, 0
	}
	n := len(turns) / 2
	if n == 0 {
		n = 1
	}
	selected := map[int]bool{}
	for _, turn := range turns[:n] {
		selected[turn] = true
	}
	var span []event.Event
	for _, candidate := range eligible {
		if selected[candidate.Turn] {
			span = append(span, candidate)
		}
	}
	seqs := make([]int64, 0, len(span))
	tokens := 0
	for _, candidate := range span {
		seqs = append(seqs, candidate.Seq)
		if candidate.Tokens != nil {
			tokens += *candidate.Tokens
		} else {
			tokens++
		}
	}
	return seqs, span, tokens
}
func pruneCandidates(visible []event.Event, h event.ScoreHealth, share float64) []int64 {
	if share <= 0 {
		return nil
	}
	latest := 0
	for _, candidate := range visible {
		if candidate.Turn > latest {
			latest = candidate.Turn
		}
	}
	protected := map[int64]bool{}
	eligible := map[int64]bool{}
	totalWeight := 0
	for _, candidate := range visible {
		weight := 1
		if candidate.Tokens != nil && *candidate.Tokens > 0 {
			weight = *candidate.Tokens
		}
		totalWeight += weight
		if candidate.Type == event.TypeSystemPrompt || candidate.Type == event.TypeUserMessage || candidate.Turn >= latest-1 {
			protected[candidate.Seq] = true
		} else {
			eligible[candidate.Seq] = true
		}
	}
	limit := int(math.Floor(float64(totalWeight) * share))
	used := 0
	var ranked []struct {
		seq int64
		sim float64
	}
	if detail := h.Details["relevance"]; detail != nil {
		encoded, _ := json.Marshal(detail["lowest"])
		var decoded []struct {
			Seq        int64   `json:"seq"`
			Similarity float64 `json:"similarity"`
			Sim        float64 `json:"sim"`
		}
		if json.Unmarshal(encoded, &decoded) == nil {
			for _, item := range decoded {
				if item.Similarity == 0 {
					item.Similarity = item.Sim
				}
				ranked = append(ranked, struct {
					seq int64
					sim float64
				}{item.Seq, item.Similarity})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].sim < ranked[j].sim })
	var result []int64
	for _, item := range ranked {
		if !eligible[item.seq] || protected[item.seq] {
			continue
		}
		weight := 1
		for _, candidate := range visible {
			if candidate.Seq == item.seq && candidate.Tokens != nil && *candidate.Tokens > 0 {
				weight = *candidate.Tokens
			}
		}
		if used+weight > limit {
			continue
		}
		result = append(result, item.seq)
		used += weight
	}
	return result
}
func (l *Ladder) skip(ctx context.Context, turn *hook.Turn, scored int, reason string) error {
	return l.append(ctx, turn, event.TypeInterveneSkip, event.InterveneSkip{Reason: reason, TurnScored: scored, BeforeTurn: turn.Turn})
}
func (l *Ladder) append(ctx context.Context, turn *hook.Turn, typ event.Type, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = l.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: typ, Payload: b, Visible: false})
	return err
}

var _ hook.BeforeTools = (*Ladder)(nil)
var _ hook.OnEvent = (*Ladder)(nil)
