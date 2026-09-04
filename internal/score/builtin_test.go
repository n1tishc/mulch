package score_test

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

type recordingEmbedder struct {
	mu      sync.Mutex
	calls   [][]string
	vectors map[string][]float32
}

func (e *recordingEmbedder) Model() string { return "test-embedding" }
func (e *recordingEmbedder) MaxBatch() int { return 2 }
func (e *recordingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, append([]string(nil), texts...))
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i] = e.vectors[text]
	}
	return result, nil
}

type memoryEmbeddingCache struct {
	mu     sync.Mutex
	values map[string][]float32
}

func (c *memoryEmbeddingCache) Load(_ context.Context, model string, hashes []string) (map[string][]float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := map[string][]float32{}
	for _, hash := range hashes {
		if value, ok := c.values[model+":"+hash]; ok {
			result[hash] = append([]float32(nil), value...)
		}
	}
	return result, nil
}
func (c *memoryEmbeddingCache) Save(_ context.Context, model string, values map[string][]float32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for hash, value := range values {
		c.values[model+":"+hash] = append([]float32(nil), value...)
	}
	return nil
}

func TestSaturationUsesProviderInputTokensAndSessionWindow(t *testing.T) {
	scorer := score.Saturation{}
	for _, tc := range []struct {
		tokens int
		want   float64
	}{{500, 1}, {725, .5}, {950, 0}} {
		result, err := scorer.Score(t.Context(), score.Input{Session: event.Session{ContextWindow: 1000}, LastResponse: provider.Response{InputTokens: tc.tokens}})
		if err != nil || math.Abs(result.Score-tc.want) > .0001 {
			t.Fatalf("tokens %d: score=%v err=%v", tc.tokens, result.Score, err)
		}
	}
}

func TestStalenessUsesVisibleToolInjectedAndPromptSources(t *testing.T) {
	now := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	old := now.Add(-30 * time.Minute)
	events := []event.Event{
		{Type: event.TypeSystemPrompt, Visible: true, Payload: payload(t, event.SystemPrompt{Sources: []event.PromptSource{{Path: "AGENTS.md", ModifiedAt: old}}})},
		{Type: event.TypeToolResult, Visible: true, Payload: payload(t, event.ToolResult{SourceTS: &old})},
		{Type: event.TypeContextInject, Visible: true, CreatedAt: old, Payload: payload(t, event.ContextInject{Text: "facts"})},
		{Type: event.TypeToolResult, Visible: false, CreatedAt: now},
	}
	result, err := (score.Staleness{HalfLife: 30 * time.Minute}).Score(t.Context(), score.Input{Visible: events[:3], Events: events, Now: now})
	if err != nil || math.Abs(result.Score-math.Exp(-1)) > .0001 {
		t.Fatalf("score=%v err=%v", result.Score, err)
	}
}

func TestRelevanceDropsForDistractorsAndReportsLowestSequences(t *testing.T) {
	task := "repair the payment retry logic"
	relevant := "the payment retry backoff fails after the second charge"
	distractor := "garden soil recipes and orchid watering schedules"
	embedder := &recordingEmbedder{vectors: map[string][]float32{task: {1, 0}, relevant: {1, 0}, distractor: {-1, 0}}}
	scorer := score.NewRelevance(embedder, &memoryEmbeddingCache{values: map[string][]float32{}})
	events := []event.Event{
		{Seq: 10, Turn: 2, Type: event.TypeAssistantMessage, Visible: true, Payload: payload(t, event.AssistantMessage{Text: relevant})},
		{Seq: 11, Turn: 2, Type: event.TypeContextInject, Visible: true, Payload: payload(t, event.ContextInject{Text: distractor})},
	}
	result, err := scorer.Score(t.Context(), score.Input{Session: event.Session{Task: task}, Visible: events, Events: events, Turn: 2})
	if err != nil {
		t.Fatal(err)
	}
	relevantEvents := append([]event.Event(nil), events...)
	relevantEvents[1].Payload = payload(t, event.ContextInject{Text: relevant})
	relevantResult, err := scorer.Score(t.Context(), score.Input{Session: event.Session{Task: task}, Visible: relevantEvents, Events: relevantEvents, Turn: 2})
	if err != nil {
		t.Fatal(err)
	}
	if relevantResult.Score-result.Score < .4 {
		t.Fatalf("relevant score %v and distracted score %v are not materially different", relevantResult.Score, result.Score)
	}
	lowest, ok := result.Details["lowest"].([]score.RelevanceDetail)
	if !ok || len(lowest) != 2 || lowest[0].Seq != 11 || lowest[0].Similarity >= lowest[1].Similarity {
		t.Fatalf("lowest = %#v", result.Details["lowest"])
	}
}

func TestRelevanceBoundsChunksBatchesAndCachesUnchangedContent(t *testing.T) {
	task := "task"
	long := strings.Repeat("x", 2100)
	embedder := &recordingEmbedder{vectors: map[string][]float32{task: {1, 0}, strings.Repeat("x", 2000): {1, 0}, strings.Repeat("x", 100): {1, 0}, "tool": {1, 0}, "injected": {1, 0}, "summary": {1, 0}}}
	cache := &memoryEmbeddingCache{values: map[string][]float32{}}
	scorer := score.NewRelevance(embedder, cache)
	events := []event.Event{
		{Seq: 1, Turn: 1, Type: event.TypeAssistantMessage, Visible: true, Payload: payload(t, event.AssistantMessage{Text: long})},
		{Seq: 2, Turn: 1, Type: event.TypeToolResult, Visible: true, Payload: payload(t, event.ToolResult{Output: "tool"})},
		{Seq: 3, Turn: 1, Type: event.TypeContextInject, Visible: true, Payload: payload(t, event.ContextInject{Text: "injected"})},
		{Seq: 4, Turn: 1, Type: event.TypeContextCompact, Visible: true, Payload: payload(t, event.ContextCompact{Summary: "summary"})},
	}
	input := score.Input{Session: event.Session{Task: task}, Visible: events, Turn: 2}
	if _, err := scorer.Score(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := scorer.Score(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	embedder.mu.Lock()
	defer embedder.mu.Unlock()
	if len(embedder.calls) != 3 {
		t.Fatalf("embed calls = %#v, want three batches on first score and none on second", embedder.calls)
	}
	for _, call := range embedder.calls {
		if len(call) > 2 {
			t.Fatalf("batch size = %d", len(call))
		}
		for _, text := range call {
			if len(text) > 2000 {
				t.Fatalf("chunk length = %d", len(text))
			}
		}
	}
}

func payload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
