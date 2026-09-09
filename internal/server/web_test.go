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

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/n1tishc/mulch/internal/event"
)

type webFakeControl struct {
	fakeControl
	starts, resumes int
	running         bool
}

func (c *webFakeControl) Start(ctx context.Context, r StartRequest) (string, error) {
	c.starts++
	return c.fakeControl.Start(ctx, r)
}
func (c *webFakeControl) Resume(context.Context, string, string) (string, error) {
	c.resumes++
	return "saved", nil
}
func (c *webFakeControl) Running(string) bool { return c.running }
func webMutation(h http.Handler, method, path, data string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestWebIdempotencyCapabilitiesAndOrigin(t *testing.T) {
	s, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateSession(t.Context(), event.Session{ID: "saved", Workdir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession(t.Context(), "saved", event.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	c := &webFakeControl{}
	h := New(s, c).WithConfig(Config{Workspace: "/workspace", Ready: true, Model: "test"}).Handler()
	data := `{"task":"hello","request_id":"unique","opts":{}}`
	for range 2 {
		if w := webMutation(h, "POST", "/api/sessions", data); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if c.starts != 1 || c.started.Opts.Workdir != "/workspace" {
		t.Fatalf("starts=%d, request=%#v", c.starts, c.started)
	}
	if w := webMutation(h, "POST", "/api/sessions", strings.Replace(data, "hello", "different", 1)); w.Code != 409 {
		t.Fatal("changed identity accepted")
	}
	for range 2 {
		if w := webMutation(h, "POST", "/api/sessions/saved/resume", `{"text":"follow up","request_id":"resume"}`); w.Code != 202 {
			t.Fatal(w.Body.String())
		}
	}
	if c.resumes != 1 {
		t.Fatal("duplicate resume")
	}
	// A new server object reuses durable acceptance instead of calling the runner.
	h = New(s, c).Handler()
	if w := webMutation(h, "POST", "/api/sessions/saved/resume", `{"text":"follow up","request_id":"resume"}`); w.Code != 202 || c.resumes != 1 {
		t.Fatal("restart lost receipt")
	}
	var detail map[string]any
	getJSON(t, h, "/api/sessions/saved", &detail)
	if detail["can_resume"] != true || detail["owner"] != "inactive" {
		t.Fatal(detail)
	}
	c.running = true
	getJSON(t, h, "/api/sessions/saved", &detail)
	if detail["can_resume"] != false || detail["can_stop"] != true {
		t.Fatal(detail)
	}
	getJSON(t, New(s).Handler(), "/api/sessions/saved", &detail)
	if detail["can_resume"] != false {
		t.Fatal("read-only controls enabled")
	}
	r := httptest.NewRequest("POST", "/api/sessions", strings.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions", strings.NewReader(data)))
	if w.Code != 415 {
		t.Fatal("simple form accepted")
	}
}

func TestFollowStreamSurvivesEndAndExternalResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	db := filepath.Join(t.TempDir(), "db")
	s, err := event.Open(ctx, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writer, err := event.Open(ctx, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err = s.CreateSession(ctx, event.Session{ID: "external"}); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(New(s).Handler())
	defer h.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.URL, "http")+"/ws/sessions/external?follow=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, typ := range []event.Type{event.TypeSessionEnd, event.TypeUserMessage, event.TypeAssistantMessage} {
		p, _ := json.Marshal(map[string]string{"text": "same session again"})
		written, err := writer.Append(ctx, event.Event{SessionID: "external", Type: typ, Payload: p})
		if err != nil {
			t.Fatal(err)
		}
		var received event.Event
		if err = wsjson.Read(ctx, conn, &received); err != nil {
			t.Fatal(err)
		}
		if received.Seq != written.Seq || received.Type != typ {
			t.Fatalf("got %#v", received)
		}
	}
}
