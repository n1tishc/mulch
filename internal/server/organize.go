package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type workspaceStore interface {
	Workspaces(context.Context) ([]string, error)
	AddWorkspace(context.Context, string) error
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(workspaceStore)
	if !ok {
		http.Error(w, "workspace storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		paths, err := store.Workspaces(r.Context())
		writeJSON(w, paths, err)
		return
	}
	if !s.currentConfig().Manage {
		http.Error(w, "this browser is read-only", http.StatusForbidden)
		return
	}
	var request struct {
		Path string `json:"path"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Path) == "" || !filepath.IsAbs(request.Path) {
		http.Error(w, "enter an absolute directory path", http.StatusBadRequest)
		return
	}
	path, err := filepath.EvalSymlinks(request.Path)
	if err != nil {
		http.Error(w, "directory is unavailable", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		http.Error(w, "path must be an existing directory", http.StatusBadRequest)
		return
	}
	if err = store.AddWorkspace(r.Context(), path); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"path": path}, nil)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if !s.currentConfig().Manage {
		http.Error(w, "this browser is read-only", http.StatusForbidden)
		return
	}
	store, ok := s.store.(interface {
		DeleteConversation(context.Context, string) error
	})
	if !ok {
		http.Error(w, "deletion unavailable", http.StatusServiceUnavailable)
		return
	}
	if control, ok := s.control.(webControl); ok && control.Running(r.PathValue("id")) {
		http.Error(w, "stop the task before deleting", http.StatusConflict)
		return
	}
	if err := store.DeleteConversation(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true}, nil)
}
