package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
)

func TestWorkspaceAndDeletionPermissions(t *testing.T) {
	root := t.TempDir()
	s, err := event.Open(t.Context(), filepath.Join(root, "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateSession(t.Context(), event.Session{ID: "delete", Workdir: root}); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession(t.Context(), "delete", event.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "keep.txt")
	if err = os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	readOnly := New(s).Handler()
	if w := webMutation(readOnly, "DELETE", "/api/sessions/delete/history", ""); w.Code != 403 {
		t.Fatal("read-only deletion accepted")
	}
	h := New(s).WithConfig(Config{Manage: true}).Handler()
	if w := webMutation(h, "POST", "/api/workspaces", `{"path":"relative"}`); w.Code != 400 {
		t.Fatal("relative path accepted")
	}
	data, _ := json.Marshal(map[string]string{"path": root})
	if w := webMutation(h, "POST", "/api/workspaces", string(data)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := webMutation(h, "DELETE", "/api/sessions/delete/history", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep" {
		t.Fatal("workspace files affected")
	}
}
