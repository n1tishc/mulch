package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
)

func TestSlowSubscriberDoesNotDelayPublisher(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithCancel(t.Context())
	ch := b.Subscribe(ctx, "session", 1)

	started := time.Now()
	for i := 0; i < 1_000; i++ {
		b.Publish(event.Event{SessionID: "session", Seq: int64(i + 1)})
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("publishing blocked for %s", elapsed)
	}
	if b.Dropped() == 0 {
		t.Fatal("expected notifications to be dropped")
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			select {
			case _, ok = <-ch:
				if ok {
					t.Fatal("subscription remained open")
				}
			case <-time.After(time.Second):
				t.Fatal("subscription did not close")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close")
	}
}
