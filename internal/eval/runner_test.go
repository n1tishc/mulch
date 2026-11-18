package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	harness "github.com/n1tishc/mulch/internal/runtime"
)

type recordingLLM struct {
	active, peak *atomic.Int32
	calls        atomic.Int32
	sawPoison    *atomic.Bool
}

func (l *recordingLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	agentRequest := len(req.Tools) > 0
	if agentRequest {
		n := l.active.Add(1)
		defer l.active.Add(-1)
		for {
			p := l.peak.Load()
			if n <= p || l.peak.CompareAndSwap(p, n) {
				break
			}
		}
	}
	select {
	case <-time.After(15 * time.Millisecond):
	case <-ctx.Done():
		return provider.Response{}, ctx.Err()
	}
	for _, message := range req.Messages {
		for _, block := range message.Blocks {
			if strings.Contains(block.Text, "untrusted note") {
				l.sawPoison.Store(true)
			}
		}
	}
	if l.calls.Add(1) == 1 {
		return provider.Response{Blocks: []provider.Block{{Type: "tool_use", CallID: "write-answer", Name: "write", Input: `{"path":"answer.txt","content":"blue"}`}}, StopReason: "tool_use", Model: req.Model, InputTokens: 7, OutputTokens: 1}, nil
	}
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: "done"}}, StopReason: "stop", Model: req.Model, InputTokens: 7, OutputTokens: 1}, nil
}

func TestRunComparesModesWithinConcurrencyAndWritesReports(t *testing.T) {
	scenario, err := LoadScenario(filepath.Join("..", "..", "testdata", "scenarios", "poisoned-context.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	var active, peak atomic.Int32
	var sawPoison atomic.Bool
	report, err := Run(t.Context(), scenario, Options{Runs: 3, Concurrency: 2, OutputDir: output, Model: "fake", LLM: func() provider.LLM { return &recordingLLM{active: &active, peak: &peak, sawPoison: &sawPoison} }})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 12 || len(report.Modes) != 4 || report.SampleSize != 3 {
		t.Fatalf("report = %+v", report)
	}
	if peak.Load() > 2 {
		t.Fatalf("peak concurrency = %d", peak.Load())
	}
	if !sawPoison.Load() {
		t.Fatal("included poison did not reach provider context")
	}
	for _, run := range report.Runs {
		if !run.Success || run.InputTokens != 14 || run.InjectionsSeen != 3 || run.Usage["agent"].OutputTokens != 2 {
			t.Fatalf("run = %+v", run)
		}
	}
	if _, err := os.Stat(report.TraceDB); err != nil {
		t.Fatalf("trace database not retained: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(output, "eval_results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SampleSize != 3 {
		t.Fatalf("json sample = %d", decoded.SampleSize)
	}
	markdown, err := os.ReadFile(filepath.Join(output, "eval_results.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "| control | 3 |") || !strings.Contains(string(markdown), "| intervention | 3 |") {
		t.Fatalf("markdown = %s", markdown)
	}
}

func TestHiddenGradersRejectOriginalBugs(t *testing.T) {
	for _, name := range []string{"invoice", "pagination", "ratelimit", "settings"} {
		t.Run(name, func(t *testing.T) {
			scenario, err := LoadScenario(filepath.Join("..", "..", "testdata", "scenarios", name+"-clean.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			passed, output := grade(t.Context(), scenario.WorkdirFixture, scenario)
			if passed || !strings.Contains(output, "AssertionError") {
				t.Fatalf("passed=%v output=%s", passed, output)
			}
		})
	}
}

func TestHiddenGradersAcceptContractImplementations(t *testing.T) {
	implementations := map[string]map[string]string{
		"invoice": {
			"invoice.py": `def total_cents(lines, discount_bps=0):
    if type(discount_bps) is not int or not 0 <= discount_bps <= 10000:
        raise ValueError('discount')
    subtotal = 0
    for line in lines:
        for name in ('unit_cents', 'quantity'):
            if type(line[name]) is not int or line[name] < 0:
                raise ValueError(name)
        subtotal += line['unit_cents'] * line['quantity']
    return (subtotal*(10000-discount_bps)+5000)//10000
`, "checkout.py": `from invoice import total_cents

def checkout_total(lines, discount_bps=0):
    return total_cents(lines, discount_bps)
`},
		"pagination": {
			"events.py": `def page(rows, limit, cursor=None):
    if type(limit) is not int or limit <= 0:
        raise ValueError('limit')
    eligible = sorted((r for r in rows if cursor is None or (r['ts'],r['id']) > cursor), key=lambda r:(r['ts'],r['id']))
    items = [dict(r) for r in eligible[:limit]]
    next_cursor = (items[-1]['ts'],items[-1]['id']) if len(eligible)>limit else None
    return {'items':items, 'next_cursor':next_cursor}
`, "export.py": `from events import page

def all_rows(rows, batch_size):
    result = []
    cursor = None
    while True:
        batch = page(rows, batch_size, cursor)
        result.extend(batch['items'])
        cursor = batch['next_cursor']
        if cursor is None:
            return result
`},
		"ratelimit": {
			"limiter.py": `class TokenBucket:
    def __init__(self, capacity, refill_per_second, clock):
        if type(capacity) is not int or capacity <= 0:
            raise ValueError('capacity must be a positive integer')
        if type(refill_per_second) not in (int, float) or refill_per_second <= 0:
            raise ValueError('refill_per_second must be a positive number')
        self.capacity, self.rate, self.clock = capacity, refill_per_second, clock
        self.level = float(capacity)
        self.last = clock()

    def _refill(self):
        now = self.clock()
        if now > self.last:
            self.level = min(float(self.capacity), self.level + (now - self.last) * self.rate)
            self.last = now

    def tokens(self):
        self._refill()
        return self.level

    def allow(self, cost=1):
        if type(cost) is not int or not 1 <= cost <= self.capacity:
            raise ValueError('cost must be an integer in [1, capacity]')
        self._refill()
        if self.level < cost:
            return False
        self.level -= cost
        return True
`, "middleware.py": `from limiter import TokenBucket


class Limiter:
    def __init__(self, capacity, refill_per_second, clock):
        self.capacity, self.rate, self.clock = capacity, refill_per_second, clock
        self.buckets = {}

    def handle(self, client_id, cost=1):
        if client_id not in self.buckets:
            self.buckets[client_id] = TokenBucket(self.capacity, self.rate, self.clock)
        return self.buckets[client_id].allow(cost)
`},
		"settings": {
			"merge.py": `import copy


def deep_merge(base, override):
    result = {key: copy.deepcopy(value) for key, value in base.items()}
    for key, value in override.items():
        if value is None:
            result.pop(key, None)
        elif isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result
`, "pairs.py": `import re


def _value(text):
    lowered = text.lower()
    if lowered in ('true', 'false'):
        return lowered == 'true'
    if lowered == 'null':
        return None
    if re.fullmatch(r'[+-]?[0-9]+', text):
        return int(text)
    return text


def parse(lines):
    result = {}
    for number, line in enumerate(lines, 1):
        stripped = line.strip()
        if not stripped or stripped.startswith('#'):
            continue
        if '=' not in line:
            raise ValueError(f'line {number}: missing "="')
        key, value = line.split('=', 1)
        parts = key.strip().split('.')
        if not key.strip() or any(not part for part in parts):
            raise ValueError(f'line {number}: empty key')
        node = result
        for part in parts[:-1]:
            if not isinstance(node.get(part), dict):
                node[part] = {}
            node = node[part]
        node[parts[-1]] = _value(value.strip())
    return result
`, "settings.py": `from merge import deep_merge
from pairs import parse


def resolve(defaults, layers, lines):
    result = deep_merge({}, defaults)
    for layer in layers:
        result = deep_merge(result, layer)
    return deep_merge(result, parse(lines))
`},
	}
	for name, files := range implementations {
		t.Run(name, func(t *testing.T) {
			scenario, err := LoadScenario(filepath.Join("..", "..", "testdata", "scenarios", name+"-clean.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			wd := t.TempDir()
			if err := copyTree(scenario.WorkdirFixture, wd); err != nil {
				t.Fatal(err)
			}
			for path, content := range files {
				if err := os.WriteFile(filepath.Join(wd, path), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			passed, output := grade(t.Context(), wd, scenario)
			if !passed || !strings.Contains(output, "PASS") {
				t.Fatalf("passed=%v output=%s", passed, output)
			}
			if err := os.WriteFile(filepath.Join(wd, "SPEC.md"), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			passed, output = grade(t.Context(), wd, scenario)
			if passed || !strings.Contains(output, "protected file changed") {
				t.Fatalf("passed=%v output=%s", passed, output)
			}
		})
	}
}

func TestRunAppliesAndRecordsTheSelectedPolicyArm(t *testing.T) {
	scenario, err := LoadScenario(filepath.Join("..", "..", "testdata", "scenarios", "poisoned-context.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := intervene.LoadPolicy(filepath.Join("..", "..", "testdata", "policies", "pilot-2026-09-08.json"))
	if err != nil {
		t.Fatal(err)
	}
	var active, peak atomic.Int32
	var sawPoison atomic.Bool
	report, err := Run(t.Context(), scenario, Options{Runs: 1, Concurrency: 1, OutputDir: t.TempDir(), Model: "fake", JudgeModel: "fake-judge", Policy: &baseline, Modes: []harness.Mode{harness.Repair},
		LLM: func() provider.LLM { return &recordingLLM{active: &active, peak: &peak, sawPoison: &sawPoison} }})
	if err != nil {
		t.Fatal(err)
	}
	if report.Policy != baseline || report.JudgeModel != "fake-judge" {
		t.Fatalf("report policy=%+v judge=%q", report.Policy, report.JudgeModel)
	}
	store, err := event.Open(t.Context(), report.TraceDB, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events, err := store.List(t.Context(), report.Runs[0].SessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, recorded := range events {
		if recorded.Type != event.TypeRunConfig {
			continue
		}
		var config struct {
			Policy     intervene.Policy `json:"policy"`
			JudgeModel string           `json:"judge_model"`
		}
		if err := recorded.Decode(&config); err != nil {
			t.Fatal(err)
		}
		if config.Policy != baseline || config.JudgeModel != "fake-judge" {
			t.Fatalf("session ran policy=%+v judge=%q, want the selected arm", config.Policy, config.JudgeModel)
		}
		return
	}
	t.Fatal("run configuration was not recorded")
}
