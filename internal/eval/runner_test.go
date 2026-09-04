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
	if len(report.Runs) != 6 || len(report.Modes) != 2 || report.SampleSize != 3 {
		t.Fatalf("report = %+v", report)
	}
	if peak.Load() > 2 {
		t.Fatalf("peak concurrency = %d", peak.Load())
	}
	if !sawPoison.Load() {
		t.Fatal("included poison did not reach provider context")
	}
	for _, run := range report.Runs {
		if !run.Success || run.InputTokens != 14 {
			t.Fatalf("run = %+v", run)
		}
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
