// Package server exposes the durable event log through a small HTTP API and
// serves the embedded trace viewer.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/n1tishc/mulch/internal/event"
)

//go:embed all:ui_dist
var viewer embed.FS

type Store interface {
	Sessions(context.Context) ([]event.Session, error)
	Session(context.Context, string) (event.Session, error)
	List(context.Context, string, int64) ([]event.Event, error)
	Tree(context.Context, string) (event.Node, error)
	Branch(context.Context, string, int64) (event.Session, error)
}

type StartRequest struct {
	Task string `json:"task"`
	Opts struct {
		Workdir string `json:"workdir"`
	} `json:"opts"`
}
type BranchRequest struct {
	At int64 `json:"at"`
}
type SessionResponse struct {
	ID string `json:"id"`
}

type Control interface {
	Start(context.Context, StartRequest) (string, error)
	Steer(string, string) error
	Branch(context.Context, string, BranchRequest) (event.Session, error)
	Cancel(string) error
	Subscribe(context.Context, string) (<-chan event.Event, error)
}

type Metrics struct {
	Sessions int `json:"sessions"`
	Events   int `json:"events"`
	Running  int `json:"running_sessions"`
}

type Server struct {
	store   Store
	control Control
	handler http.Handler
}

func New(store Store, controls ...Control) *Server {
	s := &Server{store: store}
	if len(controls) > 0 {
		s.control = controls[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.sessions)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.events)
	mux.HandleFunc("GET /api/sessions/{id}/tree", s.tree)
	mux.HandleFunc("POST /api/sessions", s.start)
	mux.HandleFunc("POST /api/sessions/{id}/steer", s.steer)
	mux.HandleFunc("POST /api/sessions/{id}/branch", s.branch)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.cancel)
	mux.HandleFunc("GET /ws/sessions/{id}", s.sessionStream)
	mux.HandleFunc("GET /api/metrics", s.metrics)
	assets, err := fs.Sub(viewer, "ui_dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", spa(http.FileServer(http.FS(assets)), assets))
	s.handler = mux
	return s
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	var request StartRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Task) == "" {
		http.Error(w, "task is required", http.StatusBadRequest)
		return
	}
	id, err := s.control.Start(r.Context(), request)
	writeAccepted(w, SessionResponse{ID: id}, err)
}

func (s *Server) steer(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	var request struct {
		Text string `json:"text"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	writeAccepted(w, SessionResponse{ID: r.PathValue("id")}, s.control.Steer(r.PathValue("id"), request.Text))
}

func (s *Server) branch(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	var request BranchRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if request.At < 0 {
		http.Error(w, "at must be a non-negative sequence", http.StatusBadRequest)
		return
	}
	child, err := s.control.Branch(r.Context(), r.PathValue("id"), request)
	writeAccepted(w, SessionResponse{ID: child.ID}, err)
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	writeAccepted(w, SessionResponse{ID: r.PathValue("id")}, s.control.Cancel(r.PathValue("id")))
}

func (s *Server) requireControl(w http.ResponseWriter) bool {
	if s.control == nil {
		http.Error(w, "live session control is unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeAccepted(w http.ResponseWriter, value any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) sessionStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.Session(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	from := int64(1)
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "from must be a non-negative sequence", http.StatusBadRequest)
			return
		}
		from = parsed
	}
	var live <-chan event.Event
	if s.control != nil {
		live, _ = s.control.Subscribe(r.Context(), id)
	}
	history, err := s.store.List(r.Context(), id, from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	last := from - 1
	for _, item := range history {
		if err = wsjson.Write(r.Context(), conn, item); err != nil {
			return
		}
		last = item.Seq
	}
	if live == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "history complete")
		return
	}
	flush := func() (bool, error) {
		items, listErr := s.store.List(r.Context(), id, last+1)
		if listErr != nil {
			return false, listErr
		}
		ended := false
		for _, item := range items {
			if writeErr := wsjson.Write(r.Context(), conn, item); writeErr != nil {
				return false, writeErr
			}
			last = item.Seq
			ended = ended || item.Type == event.TypeSessionEnd
		}
		return ended, nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case _, ok := <-live:
			ended, flushErr := flush()
			if flushErr != nil {
				return
			}
			if ended {
				_ = conn.Close(websocket.StatusNormalClosure, "session complete")
				return
			}
			if !ok {
				_ = conn.Close(websocket.StatusNormalClosure, "subscription closed")
				return
			}
		case <-ticker.C:
			ended, flushErr := flush()
			if flushErr != nil {
				return
			}
			if ended {
				_ = conn.Close(websocket.StatusNormalClosure, "session complete")
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Serve(ctx context.Context, address string) error {
	httpServer := &http.Server{Addr: address, Handler: s.handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Sessions(r.Context())
	writeJSON(w, items, err)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Session(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, nil, err)
		return
	}
	from := int64(1)
	if raw := r.URL.Query().Get("from"); raw != "" {
		var err error
		from, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || from < 0 {
			http.Error(w, "from must be a non-negative sequence", http.StatusBadRequest)
			return
		}
	}
	items, err := s.store.List(r.Context(), r.PathValue("id"), from)
	writeJSON(w, items, err)
}

func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.Tree(r.Context(), r.PathValue("id"))
	writeJSON(w, item, err)
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.Sessions(r.Context())
	metrics := Metrics{Sessions: len(sessions)}
	if err == nil {
		for _, item := range sessions {
			if item.Status == event.StatusRunning {
				metrics.Running++
			}
			items, listErr := s.store.List(r.Context(), item.ID, 1)
			if listErr != nil {
				err = listErr
				break
			}
			metrics.Events += len(items)
		}
	}
	writeJSON(w, metrics, err)
}

func writeJSON(w http.ResponseWriter, value any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func spa(files http.Handler, assets fs.FS) http.Handler {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(assets, path); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
