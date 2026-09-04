// Package mulch embeds the Mulch agent harness in Go applications.
package mulch

import (
	"context"
	"errors"
	"os"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/server"
	"github.com/n1tishc/mulch/internal/session"
	"github.com/n1tishc/mulch/internal/tool"
)

type Event = event.Event
type EventType = event.Type
type Status = session.Status
type UserMessage = event.UserMessage
type SessionEnd = event.SessionEnd
type Hook = hook.Hook
type BeforeTurnHook = hook.BeforeTurn
type BeforeToolsHook = hook.BeforeTools
type EventHook = hook.OnEvent
type Turn = hook.Turn

const (
	EventSessionStart     = event.TypeSessionStart
	EventUserMessage      = event.TypeUserMessage
	EventAssistantMessage = event.TypeAssistantMessage
	EventToolStart        = event.TypeToolStart
	EventToolResult       = event.TypeToolResult
	EventScoreHealth      = event.TypeScoreHealth
	EventIntervention     = event.TypeInterveneFire
	EventSessionEnd       = event.TypeSessionEnd
)

type Options struct {
	DB, APIKey, BaseURL, Model           string
	ContextWindow, MaxTurns, MaxSessions int
	Hooks                                []Hook
}
type RunOpts struct{ Workdir string }
type Harness struct {
	manager *session.Manager
	server  *server.Server
}

func Open(opts Options) (*Harness, error) {
	if opts.DB == "" {
		return nil, errors.New("mulch: DB is required")
	}
	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("MULCH_PROVIDER_API_KEY")
	}
	if opts.APIKey == "" {
		return nil, errors.New("mulch: APIKey is required")
	}
	if opts.Model == "" {
		opts.Model = "glm-5.3-flash"
	}
	eventBus := bus.New()
	store, err := event.Open(context.Background(), opts.DB, eventBus)
	if err != nil {
		return nil, err
	}
	runDeps := func(run session.RunOpts, steering *session.Steering) agent.Dependencies {
		workdir := run.Workdir
		if workdir == "" {
			workdir = "."
		}
		executor := tool.NewExecutor([]tool.Tool{tool.NewRead(workdir), tool.NewWrite(workdir), tool.NewEdit(workdir), tool.NewBash(workdir)})
		hooks := append([]hook.Hook{steering.Bind(store)}, opts.Hooks...)
		return agent.Dependencies{Store: store, LLM: provider.NewOpenAI(opts.APIKey, opts.BaseURL), Tools: executor, Model: opts.Model, Workdir: workdir, ContextWindow: opts.ContextWindow, MaxTurns: opts.MaxTurns, Hooks: hooks}
	}
	m := session.New(session.Options{MaxSessions: opts.MaxSessions, Bus: eventBus, Close: store.Close, Run: func(ctx context.Context, id, task string, run session.RunOpts, steering *session.Steering) error {
		_, runErr := agent.RunSession(ctx, runDeps(run, steering), id, task, nil)
		return runErr
	}, Resume: func(ctx context.Context, id, task string, run session.RunOpts, steering *session.Steering) error {
		_, runErr := agent.Resume(ctx, runDeps(run, steering), id, task, nil)
		return runErr
	}})
	h := &Harness{manager: m}
	h.server = server.New(store, server.NewControl(m, store))
	return h, nil
}

func (h *Harness) Run(ctx context.Context, task string, opts RunOpts) (string, error) {
	return h.manager.StartWith(ctx, task, session.RunOpts{Workdir: opts.Workdir})
}
func (h *Harness) Subscribe(ctx context.Context, id string) (<-chan Event, error) {
	return h.manager.Subscribe(ctx, id)
}
func (h *Harness) Steer(id, text string) error               { return h.manager.Steer(id, text) }
func (h *Harness) Cancel(id string) error                    { return h.manager.Cancel(id) }
func (h *Harness) List() []Status                            { return h.manager.List() }
func (h *Harness) Wait(ctx context.Context, id string) error { return h.manager.Wait(ctx, id) }

// Serve runs the embedded viewer and durable event-log API until ctx is cancelled.
func (h *Harness) Serve(ctx context.Context, address string) error {
	return h.server.Serve(ctx, address)
}
func (h *Harness) Shutdown(ctx context.Context) error {
	if err := h.manager.Shutdown(ctx); err != nil {
		return err
	}
	return h.manager.Close()
}
func (h *Harness) Close() error { return h.manager.Close() }
