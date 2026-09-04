package event_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
)

func TestBranchCopiesOnlyVisibleHistoryAtForkPoint(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.CreateSession(ctx, event.Session{ID: "parent", Task: "original", Model: "model", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if _, err := store.Append(ctx, event.Event{SessionID: "parent", Turn: i, Type: event.TypeUserMessage, Visible: true, Payload: []byte(fmt.Sprintf(`{"text":"event-%d"}`, i+1))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetVisible(ctx, "parent", []int64{2}, false); err != nil {
		t.Fatal(err)
	}

	child, err := store.Branch(ctx, "parent", 5)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != "parent" || child.ForkSeq == nil || *child.ForkSeq != 5 {
		t.Fatalf("branch metadata = %+v", child)
	}
	copied, err := store.List(ctx, child.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, candidate := range copied {
		if candidate.Type == event.TypeSessionStart {
			var start event.SessionStart
			if err := candidate.Decode(&start); err != nil {
				t.Fatal(err)
			}
			if start.ParentID != "parent" || start.ForkSeq == nil || *start.ForkSeq != 5 {
				t.Fatalf("branch start = %+v", start)
			}
			continue
		}
		var payload event.UserMessage
		if err := candidate.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, payload.Text)
	}
	if want := []string{"event-1", "event-3", "event-4"}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("copied history = %v, want %v", texts, want)
	}
	parent, err := store.List(ctx, "parent", 1)
	if err != nil || len(parent) != 5 {
		t.Fatalf("parent history changed: len=%d err=%v", len(parent), err)
	}
}

func TestBranchUsesVisibilityAtRequestedSequence(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.CreateSession(ctx, event.Session{ID: "parent", Task: "task", Model: "m", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if _, err := store.Append(ctx, event.Event{SessionID: "parent", Type: event.TypeUserMessage, Visible: true, Payload: []byte(fmt.Sprintf(`{"text":"event-%d"}`, i+1))}); err != nil {
			t.Fatal(err)
		}
	}
	// SetVisible appends the transition as event 6. A fork before that marker
	// sees the old context; a fork at the marker sees the updated context.
	if err := store.SetVisible(ctx, "parent", []int64{2}, false); err != nil {
		t.Fatal(err)
	}
	earlier, err := store.Branch(ctx, "parent", 3)
	if err != nil {
		t.Fatal(err)
	}
	later, err := store.Branch(ctx, "parent", 6)
	if err != nil {
		t.Fatal(err)
	}
	earlierVisible, err := store.Visible(ctx, earlier.ID)
	if err != nil {
		t.Fatal(err)
	}
	laterVisible, err := store.Visible(ctx, later.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(earlierVisible) != 3 {
		t.Fatalf("earlier visible events = %d, want 3", len(earlierVisible))
	}
	if len(laterVisible) != 4 {
		t.Fatalf("later visible events = %d, want 4", len(laterVisible))
	}
}

func TestSessionsLabelsAndTree(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.CreateSession(ctx, event.Session{ID: "root", Task: "root task", Model: "m", Workdir: "."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(ctx, event.Event{SessionID: "root", Type: event.TypeUserMessage, Visible: true, Payload: []byte(`{"text":"root"}`)}); err != nil {
		t.Fatal(err)
	}
	child, err := store.Branch(ctx, "root", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLabel(ctx, child.ID, "experiment"); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[1].Label != "experiment" {
		t.Fatalf("sessions = %+v", sessions)
	}
	tree, err := store.Tree(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	if tree.Session.ID != "root" || len(tree.Children) != 1 || tree.Children[0].Session.ID != child.ID || tree.Children[0].Session.Label != "experiment" {
		t.Fatalf("tree = %+v", tree)
	}
}

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
