package score_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

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

func payload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
