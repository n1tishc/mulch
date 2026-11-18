package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/session"
)

type launched struct {
	id, task string
	opts     session.RunOpts
}

// blockingRunner records each launch and holds the session open until release
// is closed, so tests can observe Running and Cancel on a live session.
func blockingRunner(starts chan<- launched, release <-chan struct{}) session.Runner {
	return func(ctx context.Context, id, task string, opts session.RunOpts, _ *session.Steering) error {
		starts <- launched{id, task, opts}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func startRequest(task, workdir string) StartRequest {
	request := StartRequest{Task: task}
	request.Opts.Workdir = workdir
	return request
}

func openStore(t *testing.T) *event.SQLiteStore {
	t.Helper()
	store, err := event.Open(t.Context(), filepath.Join(t.TempDir(), "events.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func receive(t *testing.T, starts <-chan launched) launched {
	t.Helper()
	select {
	case got := <-starts:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("runner was not launched")
		return launched{}
	}
}

func TestManagerControlStartsInResolvedWorkspaceAndRejectsInvalidOnes(t *testing.T) {
	starts, release := make(chan launched, 4), make(chan struct{})
	manager := session.New(session.Options{Run: blockingRunner(starts, release)})
	t.Cleanup(func() { close(release); _ = manager.Close() })
	control := NewControl(manager, openStore(t))

	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Start(t.Context(), startRequest("t", file)); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("file workspace error = %v", err)
	}
	if _, err := control.Start(t.Context(), startRequest("t", filepath.Join(dir, "missing"))); err == nil {
		t.Fatal("missing workspace accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	id, err := control.Start(t.Context(), startRequest("task", link))
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, starts)
	want, _ := filepath.EvalSymlinks(dir)
	if got.id != id || got.task != "task" || got.opts.Workdir != want {
		t.Fatalf("launched %#v, want id %s in resolved workspace %s", got, id, want)
	}
	if !control.Running(id) {
		t.Fatal("live session not reported as running")
	}
	if err := control.Steer(id, "steer"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := control.Subscribe(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := control.Cancel(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for control.Running(id) {
		if time.Now().After(deadline) {
			t.Fatal("cancelled session still running")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManagerControlResumesAndBranchesFromSavedWorkspace(t *testing.T) {
	starts, release := make(chan launched, 4), make(chan struct{})
	close(release)
	runner := blockingRunner(starts, release)
	manager := session.New(session.Options{Run: runner, Resume: runner})
	t.Cleanup(func() { _ = manager.Close() })
	store := openStore(t)
	control := NewControl(manager, store)
	workdir := t.TempDir()
	for _, id := range []string{"saved", "live"} {
		if err := store.CreateSession(t.Context(), event.Session{ID: id, Task: "task", Model: "fake", Workdir: workdir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.EndSession(t.Context(), "saved", event.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Resume(t.Context(), "live", "follow up"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("resuming a running session: %v", err)
	}
	if _, err := control.Resume(t.Context(), "saved", "follow up"); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, starts); got.id != "saved" || got.task != "follow up" || got.opts.Workdir != workdir {
		t.Fatalf("resumed %#v", got)
	}
	payload, _ := json.Marshal(event.UserMessage{Text: "task", Origin: "task"})
	recorded, err := store.Append(t.Context(), event.Event{SessionID: "saved", Turn: 1, Type: event.TypeUserMessage, Payload: payload, Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := control.Branch(t.Context(), "saved", BranchRequest{At: recorded.Seq})
	if err != nil {
		t.Fatal(err)
	}
	if got := receive(t, starts); got.id != child.ID || child.ID == "saved" || got.opts.Workdir != workdir {
		t.Fatalf("branch launched %#v for child %#v", got, child)
	}
	if _, err := control.Branch(t.Context(), "missing", BranchRequest{At: 1}); err == nil {
		t.Fatal("branching a missing session succeeded")
	}
}

func TestManagerControlResumeNeedsSessionMetadata(t *testing.T) {
	control := NewControl(session.New(session.Options{}), branchOnly{})
	if _, err := control.Resume(t.Context(), "id", "text"); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("error = %v", err)
	}
}

type branchOnly struct{}

func (branchOnly) Branch(context.Context, string, int64) (event.Session, error) {
	return event.Session{}, errors.New("unused")
}

func TestRuntimeConfigIsReadOnlyWithoutHandlerAndAppliesValidChanges(t *testing.T) {
	store := openStore(t)
	readOnly := New(store, &webFakeControl{}).WithConfig(Config{Model: "a"}).Handler()
	if w := webMutation(readOnly, http.MethodPatch, "/api/config", `{"model":"b"}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("read-only config status = %d", w.Code)
	}
	var requested []string
	onChange := func(provider, model, mode string) (Config, error) {
		requested = append(requested, provider+"/"+model+"/"+mode)
		if model == "unknown" {
			return Config{}, errors.New("unknown model")
		}
		return Config{Provider: provider, Model: model, Mode: mode, Ready: true}, nil
	}
	h := New(store, &webFakeControl{}).WithConfig(Config{Model: "a", OnChange: onChange}).Handler()
	if w := webMutation(h, http.MethodPatch, "/api/config", `{"model":"unknown"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown model") {
		t.Fatalf("rejected change: %d %s", w.Code, w.Body.String())
	}
	if w := webMutation(h, http.MethodPatch, "/api/config", `{"provider":"zen","model":"b","mode":"plain"}`); w.Code != http.StatusOK {
		t.Fatalf("valid change: %d %s", w.Code, w.Body.String())
	}
	var current Config
	getJSON(t, h, "/api/config", &current)
	if current.Model != "b" || current.Provider != "zen" || current.Mode != "plain" || len(requested) != 2 {
		t.Fatalf("config = %#v requests = %v", current, requested)
	}
}

func TestRenameValidatesLabelAndRefusesRunningSessions(t *testing.T) {
	store := openStore(t)
	for _, id := range []string{"done", "live"} {
		if err := store.CreateSession(t.Context(), event.Session{ID: id, Task: "task", Model: "fake", Workdir: "."}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.EndSession(t.Context(), "done", event.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if w := webMutation(New(store).Handler(), http.MethodPatch, "/api/sessions/done", `{"label":"x"}`); w.Code == http.StatusAccepted {
		t.Fatal("rename accepted without session control")
	}
	h := New(store, &webFakeControl{}).Handler()
	if w := webMutation(h, http.MethodPatch, "/api/sessions/done", `{"label":"`+strings.Repeat("x", 201)+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("long label status = %d", w.Code)
	}
	if w := webMutation(h, http.MethodPatch, "/api/sessions/live", `{"label":"x"}`); w.Code != http.StatusConflict {
		t.Fatalf("running rename status = %d", w.Code)
	}
	if w := webMutation(h, http.MethodPatch, "/api/sessions/done", `{"label":"  Renamed  "}`); w.Code != http.StatusAccepted {
		t.Fatalf("rename status = %d %s", w.Code, w.Body.String())
	}
	saved, err := store.Session(t.Context(), "done")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Label != "Renamed" {
		t.Fatalf("label = %q", saved.Label)
	}
}
