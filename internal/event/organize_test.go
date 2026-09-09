package event

import (
	"path/filepath"
	"testing"
)

func TestDeleteConversationRejectsRunningSubtreeAndLeases(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db")
	s, err := Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other, err := Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, x := range []Session{{ID: "parent"}, {ID: "child", ParentID: "parent"}, {ID: "keep"}} {
		if err = s.CreateSession(t.Context(), x); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Append(t.Context(), Event{SessionID: x.ID, Type: TypeUserMessage}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.EndSession(t.Context(), "parent", StatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteConversation(t.Context(), "parent"); err == nil {
		t.Fatal("deleted running child")
	}
	if err = s.EndSession(t.Context(), "child", StatusCompleted); err != nil {
		t.Fatal(err)
	}
	release, err := other.AcquireExecution(t.Context(), "child")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteConversation(t.Context(), "parent"); err == nil {
		t.Fatal("deleted leased child")
	}
	release()
	if err = s.DeleteConversation(t.Context(), "parent"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parent", "child"} {
		if _, err = s.Session(t.Context(), id); err == nil {
			t.Fatal("session retained")
		}
		if _, err = other.AcquireExecution(t.Context(), id); err == nil {
			t.Fatal("deleted session execution allowed")
		}
	}
	if _, err = s.Session(t.Context(), "keep"); err != nil {
		t.Fatal("unrelated session deleted")
	}
	if err = s.AddWorkspace(t.Context(), "/test/project"); err != nil {
		t.Fatal(err)
	}
	paths, err := other.Workspaces(t.Context())
	if err != nil || len(paths) == 0 {
		t.Fatal(paths, err)
	}
}
