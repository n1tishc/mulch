package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
