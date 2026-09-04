package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

type Options struct {
	Runs, Concurrency int
	OutputDir, Model  string
	LLM               func() provider.LLM
}
type RunResult struct {
	Mode                string         `json:"mode"`
	Success             bool           `json:"success"`
	Turns               int            `json:"turns"`
	InputTokens         int            `json:"input_tokens"`
	Interventions       int            `json:"interventions"`
	InterventionCounts  map[string]int `json:"intervention_counts"`
	MinHealth           float64        `json:"min_health"`
	ScoringLatencyMS    float64        `json:"scoring_latency_ms"`
	CriticalPathRate    float64        `json:"critical_path_rate"`
	ToolParallelismGain float64        `json:"tool_parallelism_gain"`
	SessionID           string         `json:"session_id"`
}
type ModeResult struct {
	Mode                 string         `json:"mode"`
	SampleSize           int            `json:"sample_size"`
	SuccessRate          float64        `json:"success_rate"`
	MeanTurns            float64        `json:"mean_turns"`
	MeanInputTokens      float64        `json:"mean_input_tokens"`
	MinimumHealth        float64        `json:"minimum_health"`
	MeanScoringLatencyMS float64        `json:"mean_scoring_latency_ms"`
	CriticalPathRate     float64        `json:"critical_path_rate"`
	ToolParallelismGain  float64        `json:"tool_parallelism_gain"`
	InterventionCounts   map[string]int `json:"intervention_counts"`
}
type Report struct {
	Scenario    string       `json:"scenario"`
	SampleSize  int          `json:"sample_size_per_mode"`
	TotalWallMS int64        `json:"total_wall_ms"`
	Modes       []ModeResult `json:"modes"`
	Runs        []RunResult  `json:"runs"`
}

func (r Report) Markdown() string {
	out := "| Mode | N | Success | Mean turns | Input tokens | Min health | Interventions | Score latency ms | Critical path | Tool gain |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n"
	for _, m := range r.Modes {
		out += fmt.Sprintf("| %s | %d | %.1f%% | %.2f | %.2f | %.2f | %d | %.2f | %.1f%% | %.2fx |\n", m.Mode, m.SampleSize, 100*m.SuccessRate, m.MeanTurns, m.MeanInputTokens, m.MinimumHealth, totalCounts(m.InterventionCounts), m.MeanScoringLatencyMS, 100*m.CriticalPathRate, m.ToolParallelismGain)
	}
	return out + fmt.Sprintf("\nTotal wall time: %d ms\n", r.TotalWallMS)
}

type dispatcher struct {
	mu    sync.RWMutex
	hooks map[string][]hook.OnEvent
}

func (d *dispatcher) set(id string, hs []hook.OnEvent) { d.mu.Lock(); d.hooks[id] = hs; d.mu.Unlock() }
func (d *dispatcher) Publish(e event.Event) {
	d.mu.RLock()
	hs := append([]hook.OnEvent(nil), d.hooks[e.SessionID]...)
	d.mu.RUnlock()
	for _, h := range hs {
		h.OnEvent(context.Background(), e)
	}
}

type injector struct {
	store      *event.SQLiteStore
	injections []Injection
}

func (*injector) Name() string { return "eval" }
func (i *injector) BeforeTurn(ctx context.Context, turn *hook.Turn) error {
	for _, injection := range i.injections {
		if injection.Turn != turn.Turn {
			continue
		}
		payload, _ := json.Marshal(event.EvalInject{Kind: injection.Kind, Text: injection.Text})
		if _, err := i.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeEvalInject, Payload: payload, Visible: false}); err != nil {
			return err
		}
		if injection.Kind == "stale_tool_result" {
			offset, err := time.ParseDuration(injection.SourceTSOffset)
			if err != nil {
				return fmt.Errorf("stale_tool_result source_ts_offset: %w", err)
			}
			sourceTS := time.Now().Add(offset)
			callID := fmt.Sprintf("eval-%d", turn.Turn)
			call, err := json.Marshal(event.AssistantToolCall{CallID: callID, Name: "eval", Input: json.RawMessage(`{}`)})
			if err != nil {
				return err
			}
			if _, err = i.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeAssistantToolCall, Payload: call, Visible: true}); err != nil {
				return err
			}
			result, err := json.Marshal(event.ToolResult{CallID: callID, Name: "eval", Output: injection.Text, SourceTS: &sourceTS})
			if err != nil {
				return err
			}
			if _, err = i.store.Append(ctx, event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeToolResult, Payload: result, Visible: true}); err != nil {
				return err
			}
			continue
		}
		turn.Inject = append(turn.Inject, injection.Text)
	}
	return nil
}

type mode string

const (
	controlMode      mode = "control"
	interventionMode mode = "intervention"
)

type job struct{ mode mode }

func Run(ctx context.Context, scenario Scenario, opts Options) (Report, error) {
	if opts.Runs < 1 || opts.Concurrency < 1 || opts.LLM == nil {
		return Report{}, errors.New("eval requires positive runs/concurrency and an LLM factory")
	}
	started := time.Now()
	root, err := os.MkdirTemp("", "mulch-eval-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(root)
	db := filepath.Join(root, "eval.db")
	eventBus := bus.New()
	dispatch := &dispatcher{hooks: map[string][]hook.OnEvent{}}
	publisher := &multiPublisher{all: []event.Publisher{eventBus, dispatch}}
	store, err := event.Open(context.WithoutCancel(ctx), db, publisher)
	if err != nil {
		return Report{}, err
	}
	jobs := map[string]job{}
	var jobsMu sync.RWMutex
	manager := session.New(session.Options{MaxSessions: opts.Concurrency, Bus: eventBus, Close: store.Close, Run: func(runCtx context.Context, id, task string, ro session.RunOpts, steering *session.Steering) error {
		jobsMu.RLock()
		j := jobs[ro.Workdir]
		jobsMu.RUnlock()
		coherence := score.NewCoherenceWithModel(opts.LLM(), opts.Model)
		scoring := score.NewRunner(store, event.Session{ID: id, Task: task, Model: opts.Model, Workdir: ro.Workdir, ContextWindow: 200000}, []score.Scorer{score.Saturation{}, score.Staleness{}, coherence})
		hs := []hook.Hook{scoring, &injector{store: store, injections: scenario.Injections}, steering.Bind(store)}
		listeners := []hook.OnEvent{scoring}
		if j.mode == interventionMode {
			ladder := intervene.NewConfigured(store, intervene.DefaultPolicy(), coherence)
			hs = append(hs, ladder)
			listeners = append(listeners, ladder)
		}
		dispatch.set(id, listeners)
		ex := tool.NewExecutor([]tool.Tool{tool.NewRead(ro.Workdir), tool.NewWrite(ro.Workdir), tool.NewEdit(ro.Workdir), tool.NewBash(ro.Workdir)})
		_, e := agent.RunSession(runCtx, agent.Dependencies{Store: store, LLM: opts.LLM(), Tools: ex, Model: opts.Model, Workdir: ro.Workdir, ContextWindow: 200000, MaxTurns: scenario.MaxTurns, Hooks: hs}, id, task, nil)
		return e
	}})
	var ids []string
	for n := 0; n < opts.Runs; n++ {
		for _, runMode := range []mode{controlMode, interventionMode} {
			wd := filepath.Join(root, fmt.Sprintf("%s-%d", runMode, n))
			if err = copyTree(scenario.WorkdirFixture, wd); err != nil {
				manager.Close()
				return Report{}, err
			}
			jobsMu.Lock()
			jobs[wd] = job{mode: runMode}
			jobsMu.Unlock()
			id, startErr := manager.StartWith(ctx, scenario.Task, session.RunOpts{Workdir: wd})
			if startErr != nil {
				manager.Close()
				return Report{}, startErr
			}
			ids = append(ids, id)
		}
	}
	var results []RunResult
	for _, id := range ids {
		waitErr := manager.Wait(ctx, id)
		recordedSession, sessionErr := store.Session(ctx, id)
		if sessionErr != nil {
			manager.Close()
			return Report{}, sessionErr
		}
		jobsMu.RLock()
		evaluationJob := jobs[recordedSession.Workdir]
		jobsMu.RUnlock()
		events, listErr := store.List(ctx, id, 1)
		if listErr != nil {
			manager.Close()
			return Report{}, listErr
		}
		success := waitErr == nil && checkSuccess(ctx, recordedSession.Workdir, scenario.SuccessCommands)
		results = append(results, summarizeRun(id, string(evaluationJob.mode), success, events))
	}
	if err = manager.Close(); err != nil {
		return Report{}, err
	}
	report := Report{Scenario: scenario.Name, SampleSize: opts.Runs, TotalWallMS: time.Since(started).Milliseconds(), Runs: results, Modes: aggregate(results)}
	if err = os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return Report{}, err
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(opts.OutputDir, "eval_results.json"), append(data, '\n'), 0644); err != nil {
		return Report{}, err
	}
	if err = os.WriteFile(filepath.Join(opts.OutputDir, "eval_results.md"), []byte(report.Markdown()), 0644); err != nil {
		return Report{}, err
	}
	return report, nil
}

type multiPublisher struct{ all []event.Publisher }

func (m *multiPublisher) Publish(e event.Event) {
	for _, p := range m.all {
		p.Publish(e)
	}
}
func checkSuccess(ctx context.Context, wd string, commands []string) bool {
	for _, command := range commands {
		c := exec.CommandContext(ctx, "sh", "-c", command)
		c.Dir = wd
		c.Stdout = io.Discard
		c.Stderr = io.Discard
		if c.Run() != nil {
			return false
		}
	}
	return true
}
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	if !info.IsDir() {
		return errors.New("workdir_fixture must be a directory")
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if e != nil {
			_ = in.Close()
			return e
		}
		_, e = io.Copy(out, in)
		inErr := in.Close()
		closeErr := out.Close()
		return errors.Join(e, inErr, closeErr)
	})
}
func summarizeRun(id, mode string, success bool, es []event.Event) RunResult {
	r := RunResult{Mode: mode, Success: success, SessionID: id, MinHealth: 100, InterventionCounts: map[string]int{}}
	var scores, critical int
	type toolBatch struct {
		sumMS       int64
		first, last time.Time
	}
	batches := map[int]*toolBatch{}
	starts := map[string]time.Time{}
	for _, e := range es {
		switch e.Type {
		case event.TypeSessionEnd:
			var x event.SessionEnd
			_ = e.Decode(&x)
			r.Turns = x.Turns
			r.InputTokens = x.TotalInputTokens
		case event.TypeScoreHealth:
			var x event.ScoreHealth
			_ = e.Decode(&x)
			if x.Composite < r.MinHealth {
				r.MinHealth = x.Composite
			}
			r.ScoringLatencyMS += float64(x.LatencyMS)
			scores++
			if x.OnCriticalPath {
				critical++
			}
		case event.TypeInterveneFire:
			var x event.InterveneFire
			_ = e.Decode(&x)
			r.Interventions++
			r.InterventionCounts[x.Action]++
		case event.TypeToolStart:
			var x event.ToolStart
			_ = e.Decode(&x)
			starts[x.CallID] = x.StartedAt
			batch := batches[e.Turn]
			if batch == nil {
				batch = &toolBatch{}
				batches[e.Turn] = batch
			}
			if batch.first.IsZero() || x.StartedAt.Before(batch.first) {
				batch.first = x.StartedAt
			}
		case event.TypeToolResult:
			var x event.ToolResult
			_ = e.Decode(&x)
			batch := batches[e.Turn]
			if batch == nil {
				batch = &toolBatch{}
				batches[e.Turn] = batch
			}
			batch.sumMS += x.DurationMS
			if s := starts[x.CallID]; !s.IsZero() {
				end := s.Add(time.Duration(x.DurationMS) * time.Millisecond)
				if end.After(batch.last) {
					batch.last = end
				}
			}
		}
	}
	if scores > 0 {
		r.ScoringLatencyMS /= float64(scores)
		r.CriticalPathRate = float64(critical) / float64(scores)
	}
	var toolSum, batchWall int64
	for _, batch := range batches {
		if !batch.first.IsZero() && batch.last.After(batch.first) {
			toolSum += batch.sumMS
			batchWall += batch.last.Sub(batch.first).Milliseconds()
		}
	}
	if batchWall > 0 {
		r.ToolParallelismGain = float64(toolSum) / float64(batchWall)
	}
	return r
}
func aggregate(rs []RunResult) []ModeResult {
	out := []ModeResult{}
	for _, runMode := range []mode{controlMode, interventionMode} {
		m := ModeResult{Mode: string(runMode), InterventionCounts: map[string]int{}, MinimumHealth: 100}
		for _, r := range rs {
			if r.Mode != string(runMode) {
				continue
			}
			m.SampleSize++
			if r.Success {
				m.SuccessRate++
			}
			m.MeanTurns += float64(r.Turns)
			m.MeanInputTokens += float64(r.InputTokens)
			if r.MinHealth < m.MinimumHealth {
				m.MinimumHealth = r.MinHealth
			}
			for action, count := range r.InterventionCounts {
				m.InterventionCounts[action] += count
			}
			m.MeanScoringLatencyMS += r.ScoringLatencyMS
			m.CriticalPathRate += r.CriticalPathRate
			m.ToolParallelismGain += r.ToolParallelismGain
		}
		if m.SampleSize > 0 {
			n := float64(m.SampleSize)
			m.SuccessRate /= n
			m.MeanTurns /= n
			m.MeanInputTokens /= n
			m.MeanScoringLatencyMS /= n
			m.CriticalPathRate /= n
			m.ToolParallelismGain /= n
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mode < out[j].Mode })
	return out
}
func totalCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
