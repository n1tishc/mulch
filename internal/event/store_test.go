package event_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
)

func TestOpenContextStopsWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store, err := event.Open(ctx, filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = store.Append(context.Background(), event.Event{SessionID: "missing", Type: event.TypeUserMessage})
	if err == nil || !errors.Is(err, context.Canceled) && err.Error() != "event store closed" {
		t.Fatalf("append error = %v, want closed store", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAppendsAreMonotonicPerSession(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	const sessions, perSession = 4, 250
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("s%d", i)
		if err := store.CreateSession(ctx, event.Session{ID: id, Task: "task", Model: "model", Workdir: "."}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < sessions; i++ {
		id := fmt.Sprintf("s%d", i)
		for j := 0; j < perSession; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := store.Append(ctx, event.Event{SessionID: id, Type: event.TypeUserMessage, Payload: []byte(`{"text":"hi"}`)}); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	for i := 0; i < sessions; i++ {
		events, err := store.List(ctx, fmt.Sprintf("s%d", i), 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != perSession {
			t.Fatalf("events = %d, want %d", len(events), perSession)
		}
		for j, got := range events {
			if got.Seq != int64(j+1) {
				t.Fatalf("seq[%d] = %d", j, got.Seq)
			}
		}
	}
}

func openStore(t *testing.T) *event.SQLiteStore {
	t.Helper()
	s, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
