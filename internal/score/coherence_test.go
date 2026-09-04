package score_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

func TestCoherenceJudgesBoundedPairsEveryThreeTurns(t *testing.T) {
	judge := &judgeLLM{response: `{"pairs":[{"left_seq":2,"right_seq":5,"verdict":"contradictory","reason":"different ports"},{"left_seq":3,"right_seq":6,"verdict":"consistent"}]}`}
	scorer := score.NewCoherence(judge)
	input := score.Input{Turn: 3, Visible: []event.Event{
		textEvent(t, 2, 1, event.TypeAssistantMessage, event.AssistantMessage{Text: "server listens on port 8080 in config.go"}),
		textEvent(t, 3, 1, event.TypeToolResult, event.ToolResult{Output: "config.go saved"}),
		textEvent(t, 5, 2, event.TypeContextInject, event.ContextInject{Text: "server listens on port 9090 in config.go"}),
		textEvent(t, 6, 2, event.TypeAssistantMessage, event.AssistantMessage{Text: "config.go contains the server port"}),
	}}
	result, err := scorer.Score(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != .5 {
		t.Fatalf("score = %v", result.Score)
	}
	if judge.calls != 1 || judge.pairs > 6 {
		t.Fatalf("calls=%d pairs=%d", judge.calls, judge.pairs)
	}
	contradictions, ok := result.Details["contradictory_pairs"].([]score.ContradictoryPair)
	if !ok || len(contradictions) != 1 || contradictions[0].LeftSeq != 2 || contradictions[0].RightSeq != 5 {
		t.Fatalf("details = %#v", result.Details)
	}
	if _, err := scorer.Score(t.Context(), score.Input{Turn: 4, Visible: input.Visible}); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 1 {
		t.Fatalf("judge called on unscheduled turn: %d", judge.calls)
	}
	scorer.Request()
	if _, err := scorer.Score(t.Context(), score.Input{Turn: 4, Visible: input.Visible}); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 2 {
		t.Fatalf("requested calls = %d", judge.calls)
	}
}

func TestCoherenceCancelsBlockingJudge(t *testing.T) {
	scorer := score.NewCoherence(blockingJudge{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := scorer.Score(ctx, score.Input{Turn: 3, Visible: []event.Event{textEvent(t, 1, 1, event.TypeAssistantMessage, event.AssistantMessage{Text: "same path a.go"}), textEvent(t, 2, 2, event.TypeAssistantMessage, event.AssistantMessage{Text: "same path a.go changed"})}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

type blockingJudge struct{}

func (blockingJudge) Stream(ctx context.Context, _ provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

type judgeLLM struct {
	response     string
	calls, pairs int
}

func (j *judgeLLM) Stream(_ context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	j.calls++
	var body struct {
		Pairs []any `json:"pairs"`
	}
	_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Blocks[0].Text), &body)
	j.pairs = len(body.Pairs)
	return provider.Response{Blocks: []provider.Block{{Type: "text", Text: j.response}}}, nil
}

func textEvent(t *testing.T, seq int64, turn int, typ event.Type, value any) event.Event {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Seq: seq, Turn: turn, Type: typ, Payload: payload, Visible: true}
}
