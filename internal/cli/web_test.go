package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	harness "github.com/n1tishc/mulch/internal/runtime"
)

func TestInspectHandoffKeepsTerminalOwnership(t *testing.T) {
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved := event.Session{ID: "inspect", Workdir: t.TempDir(), Model: "fake"}
	if err = store.CreateSession(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	existing := true
	c := &chatControl{store: store, recorded: &saved, existing: &existing, config: &harness.Config{Mode: harness.Plain}}
	defer c.closeInspector()
	text, _, err := c.command(context.Background(), "/inspect --no-open")
	if err != nil || !strings.Contains(text, "?session=inspect") {
		t.Fatal(text, err)
	}
	r, err := http.Get(c.inspectorURL + "/api/sessions/inspect")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var d map[string]any
	if err = json.NewDecoder(r.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d["owner"] != "external" || d["can_stop"] != false || d["can_resume"] != false {
		t.Fatal(d)
	}
	previous := c.inspectorURL
	if _, _, err = c.command(t.Context(), "/web --no-open"); err != nil || previous != c.inspectorURL {
		t.Fatal("inspector not reused", err)
	}
}
