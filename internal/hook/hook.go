package hook

import (
	"context"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

type Turn struct {
	SessionID string
	Turn      int
	Visible   []event.Event
	Inject    []string
	ToolCalls []provider.Block
	Cancel    func(string)
}

type Hook interface{ Name() string }

type OnEvent interface {
	Hook
	OnEvent(context.Context, event.Event)
}

type BeforeTurn interface {
	Hook
	BeforeTurn(context.Context, *Turn) error
}

type BeforeTools interface {
	Hook
	BeforeTools(context.Context, *Turn) error
}

// Waiter lets the session drain asynchronous hook work before its store closes.
type Waiter interface {
	Hook
	Wait(context.Context) error
}

// WaitBudgeter is an optional Waiter extension declaring how long its pending
// work may legitimately need, so the session does not cancel the final turn's
// work before that work's own deadline.
type WaitBudgeter interface {
	WaitBudget() time.Duration
}
