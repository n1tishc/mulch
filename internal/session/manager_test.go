package session_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/session"
)

func TestManagerAppliesBackpressureAndRunsThreeSessionsConcurrently(t *testing.T) {
	var active, peak atomic.Int32
	release := make(chan struct{})
	m := session.New(session.Options{MaxSessions: 3, Run: func(ctx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	defer m.Close()

	for i := 0; i < 3; i++ {
		if _, err := m.Start(t.Context(), "task"); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan error, 1)
	go func() { _, err := m.Start(t.Context(), "blocked"); started <- err }()
	select {
	case err := <-started:
		t.Fatalf("fourth start did not backpressure: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 3 {
		t.Fatalf("peak concurrency = %d, want 3", peak.Load())
	}
}

func TestCancelIsPerSessionAndShutdownJoinsAllLoops(t *testing.T) {
	exited := make(chan string, 2)
	m := session.New(session.Options{MaxSessions: 3, Run: func(ctx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		<-ctx.Done()
		exited <- id
		return ctx.Err()
	}})
	a, _ := m.Start(t.Context(), "a")
	b, _ := m.Start(t.Context(), "b")
	if err := m.Cancel(a); err != nil {
		t.Fatal(err)
	}
	if got := <-exited; got != a {
		t.Fatalf("cancelled %s, want %s", got, a)
	}
	if status, ok := m.Status(b); !ok || status.Done {
		t.Fatal("cancelling one session affected another")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := <-exited; got != b {
		t.Fatalf("shutdown joined %s, want %s", got, b)
	}
}

func TestSteerQueuesMessagesAndSubscriptionFiltersCommittedEvents(t *testing.T) {
	bus := session.NewBus()
	reached := make(chan *session.Steering, 1)
	m := session.New(session.Options{Bus: bus, Run: func(ctx context.Context, id, task string, _ session.RunOpts, steering *session.Steering) error {
		reached <- steering
		<-ctx.Done()
		return ctx.Err()
	}})
	defer m.Close()
	id, _ := m.Start(t.Context(), "task")
	steering := <-reached
	if err := m.Steer(id, "new direction"); err != nil {
		t.Fatal(err)
	}
	if got := steering.Drain(); len(got) != 1 || got[0] != "new direction" {
		t.Fatalf("steers = %#v", got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	events, err := m.Subscribe(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(event.Event{SessionID: "other", Type: event.TypeUserMessage})
	bus.Publish(event.Event{SessionID: id, Type: event.TypeUserMessage})
	select {
	case got := <-events:
		if got.SessionID != id {
			t.Fatalf("session = %q", got.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("missing committed event")
	}
	cancel()
	if err := m.Steer("missing", "x"); !errors.Is(err, session.ErrNotRunning) {
		t.Fatalf("error = %v", err)
	}
}

func TestConcurrentStatusAccess(t *testing.T) {
	m := session.New(session.Options{Run: func(context.Context, string, string, session.RunOpts, *session.Steering) error { return nil }})
	defer m.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); id, _ := m.Start(t.Context(), "x"); _, _ = m.Status(id); _ = m.List() }()
	}
	wg.Wait()
}

func TestStartAfterShutdownIsRejected(t *testing.T) {
	m := session.New(session.Options{Run: func(context.Context, string, string, session.RunOpts, *session.Steering) error { return nil }})
	if err := m.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(t.Context(), "late"); err == nil {
		t.Fatal("start succeeded after shutdown")
	}
}
