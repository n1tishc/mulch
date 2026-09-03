package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch/internal/agent"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestRunRecordsCompletedSessionAndExactRequest(t *testing.T) {
	store := openStore(t)
	llm := &fakeLLM{response: provider.Response{Blocks: []provider.Block{{Type: "text", Text: "answer"}}, StopReason: "stop", Model: "fake", InputTokens: 2, OutputTokens: 1}}
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: "."}, "hello", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Session(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != event.StatusCompleted {
		t.Fatalf("status = %s", s.Status)
	}
	events, err := store.List(t.Context(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	requestEvent := find(t, events, event.TypeLLMRequest)
	var request event.LLMRequest
	if err := requestEvent.Decode(&request); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := event.BuildMessages(events, request.VisibleEventSeqs)
	if err != nil {
		t.Fatal(err)
	}
	if !equalMessages(rebuilt, llm.request.Messages) {
		t.Fatalf("rebuilt request differs: %#v != %#v", rebuilt, llm.request.Messages)
	}
}

func TestRunRecordsFailedAndCancelledStatuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status event.Status
	}{{"failed", errors.New("boom"), event.StatusFailed}, {"cancelled", context.Canceled, event.StatusCancelled}} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			llm := &fakeLLM{err: tc.err}
			id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: llm, Model: "fake", Workdir: "."}, "hello", func(string) {})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v", err)
			}
			s, err := store.Session(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if s.Status != tc.status {
				t.Fatalf("status = %s, want %s", s.Status, tc.status)
			}
			events, err := store.List(t.Context(), id, 1)
			if err != nil {
				t.Fatal(err)
			}
			end := find(t, events, event.TypeSessionEnd)
			var payload event.SessionEnd
			if err := end.Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Status != tc.status {
				t.Fatalf("session.end status = %s, want %s", payload.Status, tc.status)
			}
		})
	}
}

func TestRunEndsSessionWhenSetupFails(t *testing.T) {
	store := &setupFailureStore{visibleErr: errors.New("read failed")}
	id, err := agent.Run(t.Context(), agent.Dependencies{Store: store, LLM: &fakeLLM{}, Model: "fake", Workdir: "."}, "hello", func(string) {})
	if !errors.Is(err, store.visibleErr) {
		t.Fatalf("error = %v", err)
	}
	if id == "" {
		t.Fatal("session ID is empty")
	}
	if store.status != event.StatusFailed {
		t.Fatalf("status = %s, want failed", store.status)
	}
	end := find(t, store.events, event.TypeSessionEnd)
	var payload event.SessionEnd
	if err := end.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != event.StatusFailed {
		t.Fatalf("session.end status = %s, want failed", payload.Status)
	}
}

type fakeLLM struct {
	request  provider.Request
	response provider.Response
	err      error
}

type setupFailureStore struct {
	events     []event.Event
	visibleErr error
	status     event.Status
}

func (s *setupFailureStore) CreateSession(context.Context, event.Session) error { return nil }
func (s *setupFailureStore) Append(_ context.Context, e event.Event) (event.Event, error) {
	e.Seq = int64(len(s.events) + 1)
	s.events = append(s.events, e)
	return e, nil
}
func (s *setupFailureStore) Visible(context.Context, string) ([]event.Event, error) {
	return nil, s.visibleErr
}
func (s *setupFailureStore) EndSession(_ context.Context, _ string, status event.Status) error {
	s.status = status
	return nil
}

func (f *fakeLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	f.request = req
	if f.err != nil {
		return provider.Response{}, f.err
	}
	out <- provider.Delta{Text: "answer"}
	return f.response, nil
}
func openStore(t *testing.T) *event.SQLiteStore {
	t.Helper()
	s, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func find(t *testing.T, es []event.Event, typ event.Type) event.Event {
	t.Helper()
	for _, e := range es {
		if e.Type == typ {
			return e
		}
	}
	t.Fatalf("missing %s", typ)
	return event.Event{}
}
func equalMessages(a, b []provider.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || len(a[i].Blocks) != len(b[i].Blocks) {
			return false
		}
		for j := range a[i].Blocks {
			if a[i].Blocks[j] != b[i].Blocks[j] {
				return false
			}
		}
	}
	return true
}
