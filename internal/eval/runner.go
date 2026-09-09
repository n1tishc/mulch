package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	harness "github.com/n1tishc/mulch/internal/runtime"
	"github.com/n1tishc/mulch/internal/score"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

type Options struct {
	Runs, Concurrency int
	OutputDir, Model  string
	LLM               func() provider.LLM
	Modes             []harness.Mode
	Seed              int64
	TokenBudget       int
}
type RunResult struct {
	Status              event.Status     `json:"status"`
	Mode                string           `json:"mode"`
	Error               string           `json:"error,omitempty"`
	GraderOutput        string           `json:"grader_output"`
	GraderPassed        bool             `json:"grader_passed"`
	InjectionsSeen      int              `json:"injections_seen"`
	Races               int              `json:"races"`
	WallMS              int64            `json:"wall_ms"`
	Usage               map[string]Usage `json:"usage"`
	ChargedTokens       int              `json:"charged_tokens"`
	Success             bool             `json:"success"`
	Turns               int              `json:"turns"`
	InputTokens         int              `json:"input_tokens"`
	Interventions       int              `json:"interventions"`
	InterventionCounts  map[string]int   `json:"intervention_counts"`
	MinHealth           float64          `json:"min_health"`
	ScoringLatencyMS    float64          `json:"scoring_latency_ms"`
	CriticalPathRate    float64          `json:"critical_path_rate"`
	ToolParallelismGain float64          `json:"tool_parallelism_gain"`
	SessionID           string           `json:"session_id"`
}
type ModeResult struct {
	Mode                 string         `json:"mode"`
	SuccessLower95       float64        `json:"success_lower_95"`
	SuccessUpper95       float64        `json:"success_upper_95"`
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
	Model          string           `json:"model"`
	ScenarioConfig Scenario         `json:"scenario_config"`
	GraderSHA256   string           `json:"grader_sha256,omitempty"`
	Policy         intervene.Policy `json:"policy"`
	Scenario       string           `json:"scenario"`
	TraceDB        string           `json:"trace_db"`
	Seed           int64            `json:"seed"`
	TokenBudget    int              `json:"token_budget_per_run"`
	SampleSize     int              `json:"sample_size_per_mode"`
	TotalWallMS    int64            `json:"total_wall_ms"`
	Modes          []ModeResult     `json:"modes"`
	Runs           []RunResult      `json:"runs"`
}

func (r Report) Markdown() string {
	out := "Correctness = completed execution AND external grader passed. Health is diagnostic, not correctness.\n\n| Mode | N | Success | Mean turns | Input tokens | Min health | Interventions | Score latency ms | Critical path | Tool gain |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n"
	for _, m := range r.Modes {
		out += fmt.Sprintf("| %s | %d | %.1f%% | %.2f | %.2f | %.2f | %d | %.2f | %.1f%% | %.2fx |\n", m.Mode, m.SampleSize, 100*m.SuccessRate, m.MeanTurns, m.MeanInputTokens, m.MinimumHealth, totalCounts(m.InterventionCounts), m.MeanScoringLatencyMS, 100*m.CriticalPathRate, m.ToolParallelismGain)
	}
	out += "\nHealth -1 means unavailable. Control means scoring only.\n\n| Session | Mode | Correct | Grader | Injections | Races | All-role tokens | Error |\n|---|---|---|---|---:|---:|---:|---|\n"
	for _, run := range r.Runs {
		out += fmt.Sprintf("| %s | %s | %t | %t | %d | %d | %d | %s |\n", run.SessionID, run.Mode, run.Success, run.GraderPassed, run.InjectionsSeen, run.Races, run.ChargedTokens, strings.ReplaceAll(strings.ReplaceAll(run.Error, "|", "/"), "\n", " "))
	}
	out += "\n| Mode | 95% Wilson interval |\n|---|---|\n"
	for _, m := range r.Modes {
		out += fmt.Sprintf("| %s | %.1f%%–%.1f%% |\n", m.Mode, 100*m.SuccessLower95, 100*m.SuccessUpper95)
	}
	return out + fmt.Sprintf("\nTotal wall time: %d ms. Trace database: `%s`. Seed: %d. Token budget per run (all roles): %d.\n", r.TotalWallMS, r.TraceDB, r.Seed, r.TokenBudget)
}

type injector struct {
	store      *event.SQLiteStore
	injections []Injection
}

func (*injector) Name() string { return "eval" }
func (i *injector) BeforeTurn(ctx context.Context, turn *hook.Turn) error {
	for index, injection := range i.injections {
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
			callID := fmt.Sprintf("eval-%d-%d", turn.Turn, index)
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

type job struct {
	mode  mode
	meter *meter
}

func Run(ctx context.Context, scenario Scenario, opts Options) (Report, error) {
	if opts.Runs < 1 || opts.Concurrency < 1 || opts.LLM == nil {
		return Report{}, errors.New("eval requires positive runs/concurrency and an LLM factory")
	}
	started := time.Now()
	if len(opts.Modes) == 0 {
		opts.Modes = append([]harness.Mode(nil), harness.Modes...)
	}
	seen := map[harness.Mode]bool{}
	for _, m := range opts.Modes {
		if !harness.ValidMode(m) || seen[m] {
			return Report{}, fmt.Errorf("invalid or duplicate mode %q", m)
		}
		seen[m] = true
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return Report{}, err
	}
	output, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return Report{}, err
	}
	root, err := os.MkdirTemp(output, "traces-")
	if err != nil {
		return Report{}, err
	}
	db := filepath.Join(root, "eval.db")
	eventBus := bus.New()
	dispatch := &harness.Router{}
	publisher := &multiPublisher{all: []event.Publisher{eventBus, dispatch}}
	store, err := event.Open(context.WithoutCancel(ctx), db, publisher)
	if err != nil {
		return Report{}, err
	}
	jobs := map[string]job{}
	var jobsMu sync.RWMutex
	manager := session.New(session.Options{MaxSessions: opts.Concurrency, Bus: eventBus, Close: store.Close, Run: func(runCtx context.Context, id, task string, ro session.RunOpts, steering *session.Steering) error {
		runCtx, cancel := context.WithTimeout(runCtx, 3*time.Minute)
		defer cancel()
		jobsMu.RLock()
		j := jobs[ro.Workdir]
		jobsMu.RUnlock()
		recorded := event.Session{ID: id, Task: task, Model: opts.Model, Workdir: ro.Workdir, ContextWindow: 200000}
		config := harness.Config{IsolatedPrompt: true, Store: store, Router: dispatch, Model: opts.Model, ContextWindow: 200000, MaxTurns: scenario.MaxTurns, Mode: harness.Mode(j.mode), Policy: intervene.DefaultPolicy(), Weights: score.DefaultWeights(),
			LLM: func(_ string, role string) provider.LLM { return j.meter.wrap(opts.LLM(), role) }}
		runErr := config.Execute(runCtx, recorded, false, task, []hook.Hook{&injector{store: store, injections: scenario.Injections}, steering.Bind(store)}, nil)
		j.meter.close()
		return runErr
	}})
	var ids []string
	for n := 0; n < opts.Runs; n++ {
		order := append([]harness.Mode(nil), opts.Modes...)
		rand.New(rand.NewSource(opts.Seed+int64(n))).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		for _, selectedMode := range order {
			runMode := mode(selectedMode)
			wd := filepath.Join(root, fmt.Sprintf("%s-%d", runMode, n))
			if err = copyTree(scenario.WorkdirFixture, wd); err != nil {
				_ = manager.Close()
				return Report{}, err
			}
			jobsMu.Lock()
			jobs[wd] = job{mode: runMode, meter: &meter{limit: opts.TokenBudget}}
			jobsMu.Unlock()
			id, startErr := manager.StartWith(ctx, scenario.Task, session.RunOpts{Workdir: wd})
			if startErr != nil {
				_ = manager.Close()
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
			_ = manager.Close()
			return Report{}, sessionErr
		}
		jobsMu.RLock()
		evaluationJob := jobs[recordedSession.Workdir]
		jobsMu.RUnlock()
		events, listErr := store.List(ctx, id, 1)
		if listErr != nil {
			_ = manager.Close()
			return Report{}, listErr
		}
		passed, graderOutput := grade(ctx, recordedSession.Workdir, scenario)
		result := summarizeRun(id, string(evaluationJob.mode), waitErr == nil && recordedSession.Status == event.StatusCompleted && passed, events)
		result.Status = recordedSession.Status
		result.GraderPassed = passed
		result.GraderOutput = graderOutput
		if waitErr != nil {
			result.Error = waitErr.Error()
		}
		result.Usage, result.ChargedTokens = evaluationJob.meter.snapshot()
		results = append(results, result)
	}
	if err = manager.Close(); err != nil {
		return Report{}, err
	}
	graderBytes, _ := os.ReadFile(scenario.Grader)
	graderHash := ""
	if scenario.Grader != "" {
		graderHash = fmt.Sprintf("%x", sha256.Sum256(graderBytes))
	}
	report := Report{Model: opts.Model, ScenarioConfig: scenario, GraderSHA256: graderHash, Policy: intervene.DefaultPolicy(), TraceDB: db, Seed: opts.Seed, TokenBudget: opts.TokenBudget, Scenario: scenario.Name, SampleSize: opts.Runs, TotalWallMS: time.Since(started).Milliseconds(), Runs: results, Modes: aggregate(results)}
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
func grade(ctx context.Context, wd string, scenario Scenario) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, name := range scenario.ProtectedFiles {
		original, err := os.ReadFile(filepath.Join(scenario.WorkdirFixture, name))
		if err != nil {
			return false, err.Error()
		}
		actual, err := os.ReadFile(filepath.Join(wd, name))
		if err != nil || string(actual) != string(original) {
			return false, "protected file changed: " + name
		}
	}
	if scenario.Grader != "" {
		// Materialize hidden tests only after the agent has finished, in a separate
		// grading copy. Execute candidate code under the normal filesystem sandbox.
		grading, err := os.MkdirTemp("", "mulch-grade-")
		if err != nil {
			return false, err.Error()
		}
		defer func() { _ = os.RemoveAll(grading) }()
		if err := copyTree(wd, filepath.Join(grading, "candidate")); err != nil {
			return false, err.Error()
		}
		script, err := os.ReadFile(scenario.Grader)
		if err != nil {
			return false, err.Error()
		}
		if err := os.WriteFile(filepath.Join(grading, "grader.py"), script, 0400); err != nil {
			return false, err.Error()
		}
		result, err := tool.NewBash(grading).Run(ctx, json.RawMessage(`{"command":"python3 -I grader.py candidate"}`))
		if err != nil {
			return false, err.Error()
		}
		return !result.IsError, result.Output
	}
	var output strings.Builder
	for _, command := range scenario.SuccessCommands {
		c := exec.CommandContext(ctx, "sh", "-c", command)
		c.Dir = wd
		data, err := c.CombinedOutput()
		output.Write(data)
		if err != nil {
			output.WriteString("\n" + err.Error())
			return false, output.String()
		}
	}
	return true, output.String()
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
	r := RunResult{Mode: mode, Success: success, SessionID: id, MinHealth: -1, InterventionCounts: map[string]int{}}
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
			r.WallMS = x.WallMS
			r.InputTokens = x.TotalInputTokens
		case event.TypeEvalInject:
			r.InjectionsSeen++
		case event.TypeRaceEnd:
			r.Races++
		case event.TypeScoreHealth:
			var x event.ScoreHealth
			_ = e.Decode(&x)
			if r.MinHealth < 0 || x.Composite < r.MinHealth {
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
	present := map[string]bool{}
	for _, r := range rs {
		present[r.Mode] = true
	}
	for runMode := range present {
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
			if r.MinHealth >= 0 && r.MinHealth < m.MinimumHealth {
				m.MinimumHealth = r.MinHealth
			}
			for action, count := range r.InterventionCounts {
				m.InterventionCounts[action] += count
			}
			m.MeanScoringLatencyMS += r.ScoringLatencyMS
			m.CriticalPathRate += r.CriticalPathRate
			m.ToolParallelismGain += r.ToolParallelismGain
		}
		if runMode == "plain" {
			m.MinimumHealth = -1
		}
		if m.SampleSize > 0 {
			n := float64(m.SampleSize)
			successes := m.SuccessRate
			m.SuccessRate /= n
			z := 1.959963984540054
			center := (successes/n + z*z/(2*n)) / (1 + z*z/n)
			half := z * math.Sqrt(successes/n*(1-successes/n)/n+z*z/(4*n*n)) / (1 + z*z/n)
			m.SuccessLower95 = math.Max(0, center-half)
			m.SuccessUpper95 = math.Min(1, center+half)
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
