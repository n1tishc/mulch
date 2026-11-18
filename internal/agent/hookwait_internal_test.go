package agent

import (
	"context"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/hook"
)

type budgetedWaiter struct{ budget time.Duration }

func (budgetedWaiter) Name() string                { return "budgeted" }
func (budgetedWaiter) Wait(context.Context) error  { return nil }
func (w budgetedWaiter) WaitBudget() time.Duration { return w.budget }

type plainWaiter struct{}

func (plainWaiter) Name() string               { return "plain" }
func (plainWaiter) Wait(context.Context) error { return nil }

func TestHookWaitCoversTheLongestDeclaredBudget(t *testing.T) {
	if got := hookWaitBudget([]hook.Hook{plainWaiter{}}); got != minimumHookWait {
		t.Fatalf("undeclared budget = %s, want minimum %s", got, minimumHookWait)
	}
	if got := hookWaitBudget([]hook.Hook{plainWaiter{}, budgetedWaiter{9 * time.Second}, budgetedWaiter{2 * time.Second}}); got != 9*time.Second {
		t.Fatalf("budget = %s, want the longest declared 9s", got)
	}
}
