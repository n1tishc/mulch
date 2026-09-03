package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

const defaultSystemPrompt = "You are Mulch, a concise and helpful coding agent."

type Store interface {
	CreateSession(context.Context, event.Session) error
	Append(context.Context, event.Event) (event.Event, error)
	Visible(context.Context, string) ([]event.Event, error)
	EndSession(context.Context, string, event.Status) error
}
type Dependencies struct {
	Store          Store
	LLM            provider.LLM
	Model, Workdir string
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
	if err = deps.Store.CreateSession(ctx, event.Session{ID: id, Task: task, Model: deps.Model, Workdir: deps.Workdir}); err != nil {
		return "", err
	}
	appendPayload := func(typ event.Type, turn int, visible bool, payload any) error {
		b, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		_, e = deps.Store.Append(context.WithoutCancel(ctx), event.Event{SessionID: id, Turn: turn, Type: typ, Payload: b, Visible: visible})
		return e
	}
	finish := func(cause error, status event.Status, turns, inputTokens, outputTokens int) error {
		endEventErr := appendPayload(event.TypeSessionEnd, turns, false, event.SessionEnd{Status: status, Turns: turns, TotalInputTokens: inputTokens, TotalOutputTokens: outputTokens, WallMS: time.Since(started).Milliseconds()})
		if endEventErr != nil && status == event.StatusCompleted {
			status = event.StatusFailed
		}
		endSessionErr := deps.Store.EndSession(context.WithoutCancel(ctx), id, status)
		return errors.Join(cause, endEventErr, endSessionErr)
	}
	fail := func(cause error) (string, error) {
		status := event.StatusFailed
		if errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			status = event.StatusCancelled
		}
		return id, finish(cause, status, 0, 0, 0)
	}
	if err = appendPayload(event.TypeSessionStart, 0, false, event.SessionStart{Task: task, Model: deps.Model, Workdir: deps.Workdir}); err != nil {
		return fail(err)
	}
	if err = appendPayload(event.TypeSystemPrompt, 0, true, event.SystemPrompt{Text: defaultSystemPrompt}); err != nil {
		return fail(err)
	}
	if err = appendPayload(event.TypeUserMessage, 0, true, event.UserMessage{Text: task, Origin: "task"}); err != nil {
		return fail(err)
	}
	visible, err := deps.Store.Visible(ctx, id)
	if err != nil {
		return fail(err)
	}
	seqs := make([]int64, 0, len(visible))
	for _, e := range visible {
		seqs = append(seqs, e.Seq)
	}
	messages, err := event.BuildMessages(visible, seqs)
	if err != nil {
		return fail(err)
	}
	if err = appendPayload(event.TypeLLMRequest, 1, false, event.LLMRequest{MessageCount: len(messages), VisibleEventSeqs: seqs}); err != nil {
		return fail(err)
	}
	deltas := make(chan provider.Delta, 64)
	requestStarted := time.Now()
	type result struct {
		response provider.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		r, e := deps.LLM.Stream(ctx, provider.Request{Messages: messages, Model: deps.Model}, deltas)
		close(deltas)
		done <- result{r, e}
	}()
	for delta := range deltas {
		emit(delta.Text)
	}
	streamed := <-done
	runErr := streamed.err
	status := event.StatusCompleted
	if runErr != nil {
		status = event.StatusFailed
		if errors.Is(runErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			status = event.StatusCancelled
		}
	}
	record := func(err error) {
		if err != nil && runErr == nil {
			runErr = fmt.Errorf("record run: %w", err)
			status = event.StatusFailed
		}
	}
	record(appendPayload(event.TypeLLMResponse, 1, false, event.LLMResponse{InputTokens: streamed.response.InputTokens, OutputTokens: streamed.response.OutputTokens, LatencyMS: time.Since(requestStarted).Milliseconds(), Model: streamed.response.Model, Cancelled: status == event.StatusCancelled}))
	for _, block := range streamed.response.Blocks {
		if block.Type == "text" && block.Text != "" {
			record(appendPayload(event.TypeAssistantMessage, 1, true, event.AssistantMessage{Text: block.Text, StopReason: streamed.response.StopReason}))
		}
	}
	return id, finish(runErr, status, 1, streamed.response.InputTokens, streamed.response.OutputTokens)
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
