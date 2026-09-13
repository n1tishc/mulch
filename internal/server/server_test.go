package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/n1tishc/mulch/internal/event"
)

func TestRecordedSessionReadAPIAndEmbeddedViewer(t *testing.T) {
	ctx := context.Background()
	store, err := event.Open(ctx, filepath.Join(t.TempDir(), "mulch.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session := event.Session{ID: "root", Task: "inspect me", Model: "test", ContextWindow: 100, Workdir: ".", CreatedAt: time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	first, err := store.Append(ctx, event.Event{SessionID: session.ID, Turn: 1, Type: event.TypeUserMessage, Payload: json.RawMessage(`{"text":"hello","unknown":{"kept":true}}`), Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(ctx, event.Event{SessionID: session.ID, Turn: 1, Type: event.TypeAssistantMessage, Payload: json.RawMessage(`{"text":"hi"}`), Visible: true}); err != nil {
		t.Fatal(err)
	}

	handler := New(store).Handler()

	t.Run("viewer", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d", response.Code)
		}
		if !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("body = %q", response.Body.String())
		}
		assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(response.Body.String(), -1)
		if len(assets) < 2 {
			t.Fatal("embedded index lacks script and stylesheet references")
		}
		for _, asset := range assets {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, asset[1], nil))
			if w.Code != http.StatusOK || w.Body.Len() == 0 || strings.Contains(w.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("embedded asset %s: status=%d content-type=%s", asset[1], w.Code, w.Header().Get("Content-Type"))
			}
		}
	})

	t.Run("sessions", func(t *testing.T) {
		var got []event.Session
		getJSON(t, handler, "/api/sessions", &got)
		if len(got) != 1 || got[0].ID != session.ID || got[0].Status != event.StatusRunning {
			t.Fatalf("sessions = %#v", got)
		}
	})

	t.Run("events from sequence preserve payload", func(t *testing.T) {
		var got []event.Event
		getJSON(t, handler, "/api/sessions/root/events?from=2", &got)
		if len(got) != 1 || got[0].Seq != first.Seq+1 {
			t.Fatalf("events = %#v", got)
		}
		var payload map[string]any
		if err := json.Unmarshal(got[0].Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["text"] != "hi" {
			t.Fatalf("payload = %#v", payload)
		}
	})

	t.Run("tree", func(t *testing.T) {
		var got event.Node
		getJSON(t, handler, "/api/sessions/root/tree", &got)
		if got.Session.ID != "root" {
			t.Fatalf("tree = %#v", got)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		var got Metrics
		getJSON(t, handler, "/api/metrics", &got)
		if got.Sessions != 1 || got.Events != 2 {
			t.Fatalf("metrics = %#v", got)
		}
	})
}

func TestSessionsAPIKeepsLiveRootsFirstAndNestsBranchSummaries(t *testing.T) {
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "mulch.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created := time.Date(2026, 9, 4, 1, 0, 0, 0, time.UTC)
	for _, session := range []event.Session{
		{ID: "done", Task: "finished", CreatedAt: created},
		{ID: "live", Task: "running", CreatedAt: created.Add(time.Minute)},
		{ID: "child", ParentID: "live", Task: "branch", CreatedAt: created.Add(2 * time.Minute)},
	} {
		if err := store.CreateSession(t.Context(), session); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.EndSession(t.Context(), "done", event.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	health, _ := json.Marshal(event.ScoreHealth{TurnScored: 3, Composite: 62, LatencyMS: 18})
	if _, err := store.Append(t.Context(), event.Event{SessionID: "live", Turn: 3, Type: event.TypeScoreHealth, Payload: health}); err != nil {
		t.Fatal(err)
	}

	var got []SessionSummary
	getJSON(t, New(store).Handler(), "/api/sessions", &got)
	if len(got) != 2 || got[0].ID != "live" || got[1].ID != "done" {
		t.Fatalf("root order = %#v", got)
	}
	if got[0].Turn != 3 || got[0].Health == nil || *got[0].Health != 62 {
		t.Fatalf("live summary = %#v", got[0])
	}
	if len(got[0].Children) != 1 || got[0].Children[0].ID != "child" {
		t.Fatalf("children = %#v", got[0].Children)
	}
}

type fakeControl struct {
	mu        sync.Mutex
	started   StartRequest
	steered   string
	cancelled string
	branched  BranchRequest
	updates   chan event.Event
}

func (f *fakeControl) Start(_ context.Context, request StartRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = request
	return "live", nil
}
func (f *fakeControl) Steer(id, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = id + ":" + text
	return nil
}
func (f *fakeControl) Cancel(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = id
	return nil
}
func (f *fakeControl) Branch(_ context.Context, id string, request BranchRequest) (event.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branched = request
	return event.Session{ID: "child", ParentID: id, ForkSeq: &request.At}, nil
}
func (f *fakeControl) Subscribe(ctx context.Context, _ string) (<-chan event.Event, error) {
	out := make(chan event.Event, 1)
	go func() {
		defer close(out)
		select {
		case item := <-f.updates:
			out <- item
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func TestLiveSessionControlAPI(t *testing.T) {
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "mulch.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	control := &fakeControl{updates: make(chan event.Event, 1)}
	handler := New(store, control).Handler()

	post := func(method, path, body string, want int, dst any) {
		t.Helper()
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s: status %d: %s", method, path, response.Code, response.Body.String())
		}
		if dst != nil && json.Unmarshal(response.Body.Bytes(), dst) != nil {
			t.Fatalf("invalid response: %s", response.Body.String())
		}
	}
	var started SessionResponse
	post(http.MethodPost, "/api/sessions", `{"task":"fix it","opts":{"workdir":"/tmp/work"}}`, http.StatusAccepted, &started)
	if started.ID != "live" || control.started.Task != "fix it" || control.started.Opts.Workdir != "/tmp/work" {
		t.Fatalf("start = %#v / %#v", started, control.started)
	}
	post(http.MethodPost, "/api/sessions/live/steer", `{"text":"check tests"}`, http.StatusAccepted, nil)
	post(http.MethodPost, "/api/sessions/live/branch", `{"at":7}`, http.StatusAccepted, nil)
	post(http.MethodDelete, "/api/sessions/live", ``, http.StatusAccepted, nil)
	if control.steered != "live:check tests" || control.cancelled != "live" || control.branched.At != 7 {
		t.Fatalf("control = %#v", control)
	}
}

func TestSessionWebSocketReplaysHistoryThenStreamsCommittedEvents(t *testing.T) {
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "mulch.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateSession(t.Context(), event.Session{ID: "live", Task: "task"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.Append(t.Context(), event.Event{SessionID: "live", Turn: 1, Type: event.TypeUserMessage, Payload: json.RawMessage(`{"text":"one"}`), Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	control := &fakeControl{updates: make(chan event.Event, 1)}
	httpServer := httptest.NewServer(New(store, control).Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws/sessions/live?from=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var replay event.Event
	if err := wsjson.Read(ctx, conn, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Seq != first.Seq {
		t.Fatalf("replay seq = %d", replay.Seq)
	}
	committed, err := store.Append(t.Context(), event.Event{SessionID: "live", Turn: 1, Type: event.TypeSessionEnd, Payload: json.RawMessage(`{"status":"completed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	control.updates <- committed
	var live event.Event
	if err := wsjson.Read(ctx, conn, &live); err != nil {
		t.Fatal(err)
	}
	if live.Seq != first.Seq+1 || live.Type != event.TypeSessionEnd {
		t.Fatalf("live = %#v", live)
	}
}

func getJSON(t *testing.T, handler http.Handler, path string, dst any) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), dst); err != nil {
		t.Fatal(err)
	}
}

func TestReadAPIRejectsInvalidRoutesAndSequences(t *testing.T) {
	store, err := event.Open(context.Background(), filepath.Join(t.TempDir(), "mulch.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := New(store).Handler()
	for _, path := range []string{"/api/sessions/missing/events", "/api/sessions/id/events?from=nope", "/api/sessions/id/nope"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code < 400 {
			t.Errorf("GET %s status = %d", path, response.Code)
		}
	}
}
