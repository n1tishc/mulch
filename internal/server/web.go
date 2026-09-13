package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/n1tishc/mulch/internal/event"
)

type Config struct {
	Workspace      string                                       `json:"workspace"`
	Provider       string                                       `json:"provider,omitempty"`
	Providers      []ProviderConfig                             `json:"providers,omitempty"`
	Model          string                                       `json:"model"`
	Mode           string                                       `json:"mode"`
	Policy         any                                          `json:"policy"`
	Ready          bool                                         `json:"ready"`
	Manage         bool                                         `json:"can_manage"`
	ReadOnlyReason string                                       `json:"read_only_reason,omitempty"`
	OnChange       func(string, string, string) (Config, error) `json:"-"`
}

type ProviderConfig struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

func (s *Server) WithConfig(config Config) *Server {
	s.configMu.Lock()
	s.config = config
	s.configMu.Unlock()
	return s
}
func (s *Server) currentConfig() Config {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.config
}

func (s *Server) runtimeConfig(w http.ResponseWriter, r *http.Request) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current := s.config
	if current.OnChange == nil {
		http.Error(w, "runtime configuration is read-only", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Mode     string `json:"mode"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	next, err := current.OnChange(request.Provider, request.Model, request.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.config = next
	writeJSON(w, next, nil)
}

type webControl interface {
	Resume(context.Context, string, string) (string, error)
	Running(string) bool
}

func (s *Server) sessionDetail(w http.ResponseWriter, r *http.Request) {
	saved, err := s.store.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, nil, err)
		return
	}
	owned := false
	if control, ok := s.control.(webControl); ok {
		owned = control.Running(saved.ID)
	}
	running := saved.Status == event.StatusRunning || owned
	owner := "inactive"
	if running {
		owner = "external"
	}
	if owned {
		owner = "daemon"
	}
	config := s.currentConfig()
	writeJSON(w, map[string]any{"session": saved, "owner": owner, "can_resume": s.control != nil && config.Ready && !running, "can_stop": owned, "can_steer": owned, "can_rename": s.control != nil && !running, "can_delete": config.Manage && !running}, nil)
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	control, ok := s.control.(webControl)
	if !ok {
		http.Error(w, "resume unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Text      string `json:"text"`
		RequestID string `json:"request_id"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	s.mutate(w, r, body.RequestID, body, func(ctx context.Context) (string, error) { return control.Resume(ctx, r.PathValue("id"), body.Text) })
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	if !s.requireControl(w) {
		return
	}
	store, ok := s.store.(interface {
		SetLabel(context.Context, string, string) error
	})
	if !ok {
		http.Error(w, "rename unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if len(body.Label) > 200 {
		http.Error(w, "label is too long", http.StatusBadRequest)
		return
	}
	saved, err := s.store.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, nil, err)
		return
	}
	if saved.Status == event.StatusRunning {
		http.Error(w, "wait for the task to finish before renaming", http.StatusConflict)
		return
	}
	err = store.SetLabel(r.Context(), saved.ID, strings.TrimSpace(body.Label))
	writeAccepted(w, SessionResponse{ID: saved.ID}, err)
}

type requestStore interface {
	ReserveRequest(context.Context, string, string) (event.RequestReceipt, error)
	FinishRequest(context.Context, string, string, string) error
}

// Reservation survives reloads and process restarts. A crash between reservation
// and settlement stays indeterminate rather than silently executing twice.
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, key string, body any, run func(context.Context) (string, error)) {
	ctx := context.WithoutCancel(r.Context())
	store, durable := s.store.(requestStore)
	if len(key) > 200 {
		http.Error(w, "request_id is too long", http.StatusBadRequest)
		return
	}
	if key != "" && durable {
		data, _ := json.Marshal(body)
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(append([]byte(r.URL.Path+":"), data...)))
		receipt, err := store.ReserveRequest(ctx, key, fingerprint)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if !receipt.Fresh {
			if receipt.Error != "" {
				http.Error(w, receipt.Error, http.StatusConflict)
				return
			}
			if receipt.Response == "" {
				http.Error(w, "submission is pending or was interrupted; inspect sessions before starting new work", http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(receipt.Response))
			return
		}
	}
	id, err := run(ctx)
	response, _ := json.Marshal(SessionResponse{ID: id})
	if key != "" && durable {
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		err = errors.Join(err, store.FinishRequest(ctx, key, string(response), failure))
	}
	writeAccepted(w, SessionResponse{ID: id}, err)
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					http.Error(w, "cross-origin mutation rejected", http.StatusForbidden)
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "cross-site mutation rejected", http.StatusForbidden)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodDelete && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
