package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/n1tishc/mulch/internal/event"
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
type Dependencies struct {
	Store          Store
	LLM            provider.LLM
	Tools          *tool.Executor
	Model, Workdir string
	MaxTurns       int
}

func Run(ctx context.Context, deps Dependencies, task string, emit func(string)) (string, error) {
	id, err := newID()
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
	if err = deps.Store.CreateSession(ctx, event.Session{ID: id, Task: task, Model: deps.Model, Workdir: deps.Workdir}); err != nil {
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

	var totalInput, totalOutput int
	for turn := 1; turn <= deps.MaxTurns; turn++ {
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
		response, streamErr := stream(ctx, deps.LLM, request, emit)
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
			return id, finish(nil, event.StatusCompleted, turn, totalInput, totalOutput)
		}
		if deps.Tools == nil {
			return fail(errors.New("model requested tools but no executor is configured"), turn, totalInput, totalOutput)
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
	}
	return fail(fmt.Errorf("maximum turns (%d) reached", deps.MaxTurns), deps.MaxTurns, totalInput, totalOutput)
}

type timedResponse struct {
	provider.Response
	latency time.Duration
}

func stream(ctx context.Context, llm provider.LLM, request provider.Request, emit func(string)) (timedResponse, error) {
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
	for delta := range deltas {
		emit(delta.Text)
	}
	completed := <-done
	return timedResponse{Response: completed.response, latency: time.Since(started)}, completed.err
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
