package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/prompt"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/tool"
)

type Store interface {
	CreateSession(context.Context, event.Session) error
	Append(context.Context, event.Event) (event.Event, error)
	Visible(context.Context, string) ([]event.Event, error)
	EndSession(context.Context, string, event.Status) error
}

type resumableStore interface {
	Store
	List(context.Context, string, int64) ([]event.Event, error)
	ResumeSession(context.Context, string) error
}
type Dependencies struct {
	Store          Store
	LLM            provider.LLM
	Tools          *tool.Executor
	Model, Workdir string
	MaxTurns       int
	ContextWindow  int
	Hooks          []hook.Hook
}

func Run(ctx context.Context, deps Dependencies, task string, emit func(string)) (string, error) {
	id, err := event.NewSessionID()
	if err != nil {
		return "", err
	}
	started := time.Now()
	if deps.Workdir == "" {
		deps.Workdir = "."
	}
	if deps.MaxTurns <= 0 {
		deps.MaxTurns = 20
	}
	if emit == nil {
		emit = func(string) {}
	}
	if err = deps.Store.CreateSession(ctx, event.Session{ID: id, Task: task, Model: deps.Model, Workdir: deps.Workdir, ContextWindow: deps.ContextWindow}); err != nil {
		return "", err
	}
	appendPayload := func(typ event.Type, turn int, visible bool, payload any) error {
		b, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		_, appendErr := deps.Store.Append(context.WithoutCancel(ctx), event.Event{SessionID: id, Turn: turn, Type: typ, Payload: b, Visible: visible})
		return appendErr
	}
	finish := func(cause error, status event.Status, turns, inputTokens, outputTokens int) error {
		waitForHooks(deps.Hooks)
		endErr := appendPayload(event.TypeSessionEnd, turns, false, event.SessionEnd{Status: status, Turns: turns, TotalInputTokens: inputTokens, TotalOutputTokens: outputTokens, WallMS: time.Since(started).Milliseconds()})
		if endErr != nil && status == event.StatusCompleted {
			status = event.StatusFailed
		}
		return errors.Join(cause, endErr, deps.Store.EndSession(context.WithoutCancel(ctx), id, status))
	}
	statusFor := func(cause error) event.Status {
		if errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return event.StatusCancelled
		}
		return event.StatusFailed
	}
	fail := func(cause error, turns, inputTokens, outputTokens int) (string, error) {
		return id, finish(cause, statusFor(cause), turns, inputTokens, outputTokens)
	}
	if err = appendPayload(event.TypeSessionStart, 0, false, event.SessionStart{Task: task, Model: deps.Model, Workdir: deps.Workdir}); err != nil {
		return fail(err, 0, 0, 0)
	}
	home, _ := os.UserHomeDir()
	assembled, err := prompt.Assemble(deps.Workdir, home)
	if err != nil {
		return fail(err, 0, 0, 0)
	}
	sources := make([]event.PromptSource, len(assembled.Sources))
	for i, source := range assembled.Sources {
		sources[i] = event.PromptSource{Path: source.Path, ModifiedAt: source.ModifiedAt}
	}
	if err = appendPayload(event.TypeSystemPrompt, 0, true, event.SystemPrompt{Text: assembled.Text, Sources: sources}); err != nil {
		return fail(err, 0, 0, 0)
	}
	if err = appendPayload(event.TypeUserMessage, 0, true, event.UserMessage{Text: task, Origin: "task"}); err != nil {
		return fail(err, 0, 0, 0)
	}

	return continueSession(ctx, deps, id, started, 1, 0, 0, emit)
}

// Resume reconstructs the visible context of an existing session and appends a
// new model turn. An optional task is recorded as a new user message.
func Resume(ctx context.Context, deps Dependencies, id, task string, emit func(string)) (string, error) {
	if deps.MaxTurns <= 0 {
		deps.MaxTurns = 20
	}
	if emit == nil {
		emit = func(string) {}
	}
	store, ok := deps.Store.(resumableStore)
	if !ok {
		return id, errors.New("event store does not support resuming sessions")
	}
	events, err := store.List(ctx, id, 1)
	if err != nil {
		return id, err
	}
	startTurn, totalInput, totalOutput := 1, 0, 0
	for _, candidate := range events {
		if candidate.Turn >= startTurn {
			startTurn = candidate.Turn + 1
		}
		if candidate.Type == event.TypeSessionEnd {
			var ended event.SessionEnd
			if candidate.Decode(&ended) == nil {
				totalInput, totalOutput = ended.TotalInputTokens, ended.TotalOutputTokens
			}
		}
	}
	if err := store.ResumeSession(ctx, id); err != nil {
		return id, err
	}
	if task != "" {
		if err := appendPayload(ctx, deps.Store, id, event.TypeUserMessage, startTurn-1, true, event.UserMessage{Text: task, Origin: "task"}); err != nil {
			return id, err
		}
	}
	return continueSession(ctx, deps, id, time.Now(), startTurn, totalInput, totalOutput, emit)
}

func continueSession(ctx context.Context, deps Dependencies, id string, started time.Time, startTurn, totalInput, totalOutput int, emit func(string)) (string, error) {
	appendPayload := func(typ event.Type, turn int, visible bool, payload any) error {
		return appendPayload(ctx, deps.Store, id, typ, turn, visible, payload)
	}
	finish := func(cause error, status event.Status, turns, inputTokens, outputTokens int) error {
		waitForHooks(deps.Hooks)
		endErr := appendPayload(event.TypeSessionEnd, turns, false, event.SessionEnd{Status: status, Turns: turns, TotalInputTokens: inputTokens, TotalOutputTokens: outputTokens, WallMS: time.Since(started).Milliseconds()})
		if endErr != nil && status == event.StatusCompleted {
			status = event.StatusFailed
		}
		return errors.Join(cause, endErr, deps.Store.EndSession(context.WithoutCancel(ctx), id, status))
	}
	statusFor := func(cause error) event.Status {
		if errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return event.StatusCancelled
		}
		return event.StatusFailed
	}
	fail := func(cause error, turns, inputTokens, outputTokens int) (string, error) {
		return id, finish(cause, statusFor(cause), turns, inputTokens, outputTokens)
	}
	var err error
	lastTurn := startTurn - 1
	for offset := 0; offset < deps.MaxTurns; offset++ {
		turn := startTurn + offset
		lastTurn = turn
		visible, visibleErr := deps.Store.Visible(ctx, id)
		if visibleErr != nil {
			return fail(visibleErr, turn-1, totalInput, totalOutput)
		}
		seqs := make([]int64, 0, len(visible))
		for _, candidate := range visible {
			seqs = append(seqs, candidate.Seq)
		}
		messages, buildErr := event.BuildMessages(visible, seqs)
		if buildErr != nil {
			return fail(buildErr, turn-1, totalInput, totalOutput)
		}
		request := provider.Request{Messages: messages, Model: deps.Model}
		if deps.Tools != nil {
			for _, available := range deps.Tools.Specs() {
				request.Tools = append(request.Tools, provider.ToolSpec{Name: available.Name(), Description: available.Description(), InputSchema: available.InputSchema()})
			}
		}
		if err = appendPayload(event.TypeLLMRequest, turn, false, event.LLMRequest{MessageCount: len(messages), VisibleEventSeqs: seqs}); err != nil {
			return fail(err, turn-1, totalInput, totalOutput)
		}
		response, streamErr := stream(ctx, deps.LLM, request, func(delta provider.Delta) error {
			if err := appendPayload(event.TypeAssistantDelta, turn, false, event.AssistantDelta{Text: delta.Text}); err != nil {
				return err
			}
			emit(delta.Text)
			return nil
		})
		totalInput += response.InputTokens
		totalOutput += response.OutputTokens
		status := event.StatusCompleted
		if streamErr != nil {
			status = statusFor(streamErr)
		}
		recordErr := appendPayload(event.TypeLLMResponse, turn, false, event.LLMResponse{InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, LatencyMS: response.latency.Milliseconds(), Model: response.Model, Cancelled: status == event.StatusCancelled})
		if streamErr != nil || recordErr != nil {
			return fail(errors.Join(streamErr, recordErr), turn, totalInput, totalOutput)
		}
		var calls []tool.Call
		for _, block := range response.Blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					if err = appendPayload(event.TypeAssistantMessage, turn, true, event.AssistantMessage{Text: block.Text, StopReason: response.StopReason}); err != nil {
						return fail(err, turn, totalInput, totalOutput)
					}
				}
			case "tool_use":
				input := json.RawMessage(block.Input)
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				if err = appendPayload(event.TypeAssistantToolCall, turn, true, event.AssistantToolCall{CallID: block.CallID, Name: block.Name, Input: input}); err != nil {
					return fail(err, turn, totalInput, totalOutput)
				}
				calls = append(calls, tool.Call{ID: block.CallID, Name: block.Name, Input: input})
			}
		}
		if len(calls) == 0 {
			if err = appendPayload(event.TypeTurnCompleted, turn, false, event.TurnCompleted{}); err != nil {
				return fail(err, turn, totalInput, totalOutput)
			}
			return id, finish(nil, event.StatusCompleted, turn, totalInput, totalOutput)
		}
		if deps.Tools == nil {
			return fail(errors.New("model requested tools but no executor is configured"), turn, totalInput, totalOutput)
		}
		toolBlocks := make([]provider.Block, 0, len(calls))
		for _, block := range response.Blocks {
			if block.Type == "tool_use" {
				toolBlocks = append(toolBlocks, block)
			}
		}
		cancelReason := ""
		hookTurn := &hook.Turn{SessionID: id, Turn: turn, Visible: visible, ToolCalls: toolBlocks, Cancel: func(reason string) { cancelReason = reason }}
		for _, extension := range deps.Hooks {
			if before, ok := extension.(hook.BeforeTools); ok {
				if err = before.BeforeTools(ctx, hookTurn); err != nil {
					return fail(fmt.Errorf("before tools hook %q: %w", extension.Name(), err), turn, totalInput, totalOutput)
				}
			}
		}
		if cancelReason != "" {
			return id, finish(nil, event.StatusEscalated, turn, totalInput, totalOutput)
		}
		if len(hookTurn.ToolCalls) == 0 {
			for _, call := range calls {
				if err = appendPayload(event.TypeToolResult, turn, true, event.ToolResult{CallID: call.ID, Name: call.Name, Output: "tool call withheld by a before-tools hook; reconsider using the updated context", IsError: true, Cancelled: true}); err != nil {
					return fail(err, turn, totalInput, totalOutput)
				}
			}
			if err = appendPayload(event.TypeTurnCompleted, turn, false, event.TurnCompleted{}); err != nil {
				return fail(err, turn, totalInput, totalOutput)
			}
			continue
		}
		var toolEventErr error
		deps.Tools.RunAll(ctx, calls, func(execution tool.ExecutionEvent) {
			if toolEventErr != nil {
				return
			}
			if execution.Kind == tool.EventStart {
				toolEventErr = appendPayload(event.TypeToolStart, turn, false, event.ToolStart{CallID: execution.Call.ID, Name: execution.Call.Name, Input: execution.Call.Input, StartedAt: execution.StartedAt})
				return
			}
			var sourceTS *time.Time
			if !execution.Result.SourceTS.IsZero() {
				value := execution.Result.SourceTS
				sourceTS = &value
			}
			toolEventErr = appendPayload(event.TypeToolResult, turn, true, event.ToolResult{CallID: execution.Call.ID, Name: execution.Call.Name, Output: execution.Result.Output, IsError: execution.Result.IsError, Cancelled: execution.Cancelled, TimedOut: execution.TimedOut, DurationMS: execution.Duration.Milliseconds(), SourceTS: sourceTS})
		})
		if toolEventErr != nil {
			return fail(toolEventErr, turn, totalInput, totalOutput)
		}
		if ctx.Err() != nil {
			return fail(ctx.Err(), turn, totalInput, totalOutput)
		}
		if err = appendPayload(event.TypeTurnCompleted, turn, false, event.TurnCompleted{}); err != nil {
			return fail(err, turn, totalInput, totalOutput)
		}
	}
	return fail(fmt.Errorf("maximum turns (%d) reached", deps.MaxTurns), lastTurn, totalInput, totalOutput)
}

func waitForHooks(hooks []hook.Hook) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, extension := range hooks {
		if waiter, ok := extension.(hook.Waiter); ok {
			_ = waiter.Wait(ctx)
		}
	}
}

func appendPayload(ctx context.Context, store Store, id string, typ event.Type, turn int, visible bool, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = store.Append(context.WithoutCancel(ctx), event.Event{SessionID: id, Turn: turn, Type: typ, Payload: b, Visible: visible})
	return err
}

type timedResponse struct {
	provider.Response
	latency time.Duration
}

func stream(ctx context.Context, llm provider.LLM, request provider.Request, consume func(provider.Delta) error) (timedResponse, error) {
	started := time.Now()
	deltas := make(chan provider.Delta, 64)
	type result struct {
		response provider.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		response, err := llm.Stream(ctx, request, deltas)
		close(deltas)
		done <- result{response, err}
	}()
	var consumeErr error
	for delta := range deltas {
		if consumeErr == nil {
			consumeErr = consume(delta)
		}
	}
	completed := <-done
	return timedResponse{Response: completed.response, latency: time.Since(started)}, errors.Join(completed.err, consumeErr)
}
