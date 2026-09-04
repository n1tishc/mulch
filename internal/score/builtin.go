package score

import (
	"context"
	"math"
	"time"

	"github.com/n1tishc/mulch/internal/event"
)

type Saturation struct{}

func (Saturation) Name() string            { return "saturation" }
func (Saturation) Deadline() time.Duration { return 5 * time.Millisecond }
func (Saturation) Score(_ context.Context, input Input) (Result, error) {
	if input.Session.ContextWindow <= 0 {
		return Result{Score: 1}, nil
	}
	ratio := float64(input.LastResponse.InputTokens) / float64(input.Session.ContextWindow)
	value := 1.0
	if ratio > .5 {
		value = 1 - (ratio-.5)/.45
	}
	if value < 0 {
		value = 0
	}
	return Result{Score: value, Details: map[string]any{"ratio": ratio}}, nil
}

type Staleness struct{ HalfLife time.Duration }

func (Staleness) Name() string            { return "staleness" }
func (Staleness) Deadline() time.Duration { return 5 * time.Millisecond }
func (s Staleness) Score(_ context.Context, input Input) (Result, error) {
	halfLife := s.HalfLife
	if halfLife <= 0 {
		halfLife = 30 * time.Minute
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	var weighted, weights float64
	add := func(at time.Time, weight float64) {
		if at.IsZero() {
			return
		}
		age := now.Sub(at)
		if age < 0 {
			age = 0
		}
		weighted += math.Exp(-float64(age)/float64(halfLife)) * weight
		weights += weight
	}
	for _, candidate := range input.Visible {
		weight := 1.0
		if candidate.Tokens != nil && *candidate.Tokens > 0 {
			weight = float64(*candidate.Tokens)
		}
		switch candidate.Type {
		case event.TypeSystemPrompt:
			var prompt event.SystemPrompt
			if candidate.Decode(&prompt) == nil {
				for _, source := range prompt.Sources {
					add(source.ModifiedAt, weight)
				}
			}
		case event.TypeToolResult:
			var result event.ToolResult
			if candidate.Decode(&result) == nil && result.SourceTS != nil {
				add(*result.SourceTS, weight)
			}
		case event.TypeContextInject:
			add(candidate.CreatedAt, weight)
		}
	}
	if weights == 0 {
		return Result{Score: 1}, nil
	}
	return Result{Score: weighted / weights, Details: map[string]any{"sources": weights}}, nil
}
