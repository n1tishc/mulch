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

	"github.com/n1tishc/mulch/internal/event"
)

//go:embed all:ui_dist
var viewer embed.FS

type Store interface {
	Sessions(context.Context) ([]event.Session, error)
	Session(context.Context, string) (event.Session, error)
	List(context.Context, string, int64) ([]event.Event, error)
	Tree(context.Context, string) (event.Node, error)
}

type Metrics struct {
	Sessions int `json:"sessions"`
	Events   int `json:"events"`
	Running  int `json:"running_sessions"`
}

type Server struct {
	store   Store
	handler http.Handler
}

func New(store Store) *Server {
	s := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.sessions)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.events)
	mux.HandleFunc("GET /api/sessions/{id}/tree", s.tree)
	mux.HandleFunc("GET /api/metrics", s.metrics)
	assets, err := fs.Sub(viewer, "ui_dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", spa(http.FileServer(http.FS(assets)), assets))
	s.handler = mux
	return s
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
