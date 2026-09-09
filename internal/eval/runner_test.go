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

	"github.com/n1tishc/mulch/internal/provider"
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
	for _, name := range []string{"invoice", "pagination"} {
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
