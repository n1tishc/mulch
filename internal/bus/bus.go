package bus

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/n1tishc/mulch/internal/event"
)

type subscription struct {
	sessionID string
	ch        chan event.Event
}
type Bus struct {
	mu            sync.Mutex
	next          uint64
	subscriptions map[uint64]subscription
	dropped       atomic.Uint64
}

func New() *Bus { return &Bus{subscriptions: make(map[uint64]subscription)} }
func (b *Bus) Subscribe(ctx context.Context, sessionID string, buffer int) <-chan event.Event {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan event.Event, buffer)
	b.subscriptions[id] = subscription{sessionID: sessionID, ch: ch}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if _, ok := b.subscriptions[id]; ok {
			delete(b.subscriptions, id)
			close(ch)
		}
		b.mu.Unlock()
	}()
	return ch
}
func (b *Bus) Publish(e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subscriptions {
		if s.sessionID != "" && s.sessionID != e.SessionID {
			continue
		}
		select {
		case s.ch <- e:
		default:
			b.dropped.Add(1)
		}
	}
}
func (b *Bus) Dropped() uint64 { return b.dropped.Load() }
