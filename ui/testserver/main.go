// A deterministic browser-test fixture. No network provider or real repository is used.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/n1tishc/mulch/internal/cli"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	root, err := os.MkdirTemp("", "mulch-web-fixture-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	db := filepath.Join(root, "fixture.db")
	if err = seed(ctx, db, root); err != nil {
		panic(err)
	}
	err = cli.Execute(ctx, []string{"web", "--no-open", "--addr", "127.0.0.1:4144", "--db", db, "--workdir", root}, cli.Options{Getenv: func(name string) string {
		if name == "MULCH_PROVIDER_API_KEY" {
			return "deterministic-test-only"
		}
		if name == "MULCH_MODEL" {
			return "fixture-model"
		}
		return ""
	}, LLMFactory: func(string, string) provider.LLM { return &fakeLLM{} }})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type fakeLLM struct{ calls int }

func (f *fakeLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	f.calls++
	latest := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			for _, b := range m.Blocks {
				if b.Text != "" {
					latest = b.Text
				}
			}
		}
	}
	if strings.Contains(latest, "slow") {
		select {
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	if strings.Contains(latest, "provider failure") {
		return provider.Response{}, fmt.Errorf("deterministic provider failure")
	}
	if len(req.Tools) > 0 && f.calls == 1 {
		return provider.Response{Model: req.Model, InputTokens: 20, OutputTokens: 12, Blocks: []provider.Block{{Type: "tool_use", CallID: "fixture-write", Name: "write", Input: `{"path":"result.txt","content":"deterministic browser fixture\n"}`}}}, nil
	}
	answer := "Implemented the fixture change.\n\n```go\nfmt.Println(\"hello, mulch\")\n```\n\nThe tool wrote `result.txt`. This is synthetic test output."
	for _, word := range strings.Split(answer, " ") {
		select {
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		case out <- provider.Delta{Text: word + " "}:
		}
		select {
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		case <-time.After(8 * time.Millisecond):
		}
	}
	return provider.Response{Model: req.Model, InputTokens: 20, OutputTokens: 30, Blocks: []provider.Block{{Type: "text", Text: answer}}, StopReason: "end_turn"}, nil
}

func seed(ctx context.Context, db, wd string) error {
	s, err := event.Open(ctx, db, nil)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	for _, id := range []string{"evidence", "candidate-prune", "candidate-reanchor", "external", "load"} {
		parent := ""
		if strings.HasPrefix(id, "candidate-") {
			parent = "evidence"
		}
		if err = s.CreateSession(ctx, event.Session{ID: id, ParentID: parent, Task: "Synthetic fixture: " + id, Label: "Fixture · " + id, Model: "fixture-model", Workdir: wd, ContextWindow: 200000}); err != nil {
			return err
		}
		if id != "external" {
			if err = s.EndSession(ctx, id, event.StatusCompleted); err != nil {
				return err
			}
		}
	}
	add := func(id string, turn int, typ event.Type, p any) (int64, error) {
		data, e := json.Marshal(p)
		if e != nil {
			return 0, e
		}
		record, e := s.Append(ctx, event.Event{SessionID: id, Turn: turn, Type: typ, Payload: data, Visible: true})
		return record.Seq, e
	}
	appendRecord := func(id string, turn int, typ event.Type, p any) int64 {
		seq, e := add(id, turn, typ, p)
		if e != nil {
			panic(e)
		}
		return seq
	}
	appendRecord("evidence", 1, event.TypeUserMessage, event.UserMessage{Text: "Synthetic repair evidence. Inspect duplicate occurrences and candidate selection."})
	appendRecord("evidence", 1, event.TypeRunConfig, map[string]any{"version": 1, "mode": "race", "policy": intervene.DefaultPolicy()})
	one := appendRecord("evidence", 1, event.TypeToolResult, event.ToolResult{CallID: "one", Output: "Identical payload, separate occurrence."})
	two := appendRecord("evidence", 1, event.TypeToolResult, event.ToolResult{CallID: "two", Output: "Identical payload, separate occurrence."})
	appendRecord("evidence", 1, event.TypeLLMRequest, event.LLMRequest{VisibleEventSeqs: []int64{one, two}})
	value := .4
	health := appendRecord("evidence", 1, event.TypeScoreHealth, event.ScoreHealth{TurnScored: 1, Composite: 42, Coherence: &value, Saturation: &value, Freshness: map[string]string{"coherence": "reused", "saturation": "fresh"}})
	appendRecord("evidence", 1, event.TypeScorePartial, event.ScorePartial{Name: "coherence", Reason: "invalid_response", UsedPrevious: true})
	mutation := appendRecord("evidence", 2, event.TypeContextVisibility, event.ContextVisibility{Changes: []event.VisibilityChange{{Seq: two, From: true, To: false}}, Reason: "synthetic prune"})
	appendRecord("evidence", 2, event.TypeInterveneFire, event.InterveneFire{Action: "prune", Reason: "Synthetic confirmed low relevance", TurnScored: 1, AppliedBeforeTurn: 2, ScoreSeq: health, ContextSeq: mutation})
	compact := appendRecord("evidence", 3, event.TypeContextCompact, event.ContextCompact{ReplacedSeqs: []int64{one}, Summary: "Synthetic summary"})
	appendRecord("evidence", 3, event.TypeInterveneFire, event.InterveneFire{Action: "compact", Reason: "Synthetic compaction", ScoreSeq: health, ContextSeq: compact})
	anchor := appendRecord("evidence", 4, event.TypeContextInject, event.ContextInject{Text: "Original task restored", Reason: "synthetic reanchor"})
	appendRecord("evidence", 4, event.TypeInterveneFire, event.InterveneFire{Action: "reanchor", Reason: "Synthetic reanchor", ScoreSeq: health, ContextSeq: anchor})
	appendRecord("evidence", 5, event.TypeInterveneFire, event.InterveneFire{Action: "escalate", Reason: "Synthetic critical health", ScoreSeq: health})
	ids := []string{"candidate-prune", "candidate-reanchor"}
	appendRecord("evidence", 5, event.TypeRaceStart, event.RaceStart{Branches: ids, Actions: []string{"prune", "reanchor"}})
	appendRecord("evidence", 5, event.TypeRaceEnd, event.RaceEnd{Branches: ids, Scores: map[string]float64{ids[0]: 62, ids[1]: 62}, Winner: ids[0], Loser: ids[1]})
	appendRecord("evidence", 5, event.TypeSessionEnd, event.SessionEnd{Status: event.StatusCompleted})
	for _, id := range ids {
		appendRecord(id, 1, event.TypeContextInject, event.ContextInject{Text: "Synthetic candidate mutation"})
		appendRecord(id, 1, event.TypeSessionEnd, event.SessionEnd{Status: event.StatusCompleted, TotalInputTokens: 12, TotalOutputTokens: 4})
	}
	appendRecord("external", 1, event.TypeUserMessage, event.UserMessage{Text: "Synthetic externally owned task"})
	for i := 1; i <= 10000; i++ {
		appendRecord("load", 1, event.TypeAssistantDelta, event.AssistantDelta{Text: "x"})
	}
	return nil
}
