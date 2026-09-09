package event

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCancelledExecutionAcquisitionDoesNotStrandLease(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "cancel.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.AcquireExecution(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire: %v", err)
	}
	runCtx, stop := context.WithCancel(t.Context())
	release, err := s.AcquireExecution(runCtx, "s")
	if err != nil {
		t.Fatal(err)
	}
	stop()
	release()
	again, err := s.AcquireExecution(t.Context(), "s")
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestExecutionLeaseAcrossConnectionsAndRequestRestart(t *testing.T) {
	db := filepath.Join(t.TempDir(), "web.db")
	one, err := Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	release, err := one.AcquireExecution(t.Context(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = two.AcquireExecution(t.Context(), "shared"); err == nil {
		t.Fatal("second process acquired running session")
	}
	release()
	release2, err := two.AcquireExecution(t.Context(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	release2()
	r, err := one.ReserveRequest(t.Context(), "request", "fingerprint")
	if err != nil || !r.Fresh {
		t.Fatalf("reserve: %#v %v", r, err)
	}
	if err = one.FinishRequest(t.Context(), "request", `{"id":"shared"}`, ""); err != nil {
		t.Fatal(err)
	}
	if err = one.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	r, err = reopened.ReserveRequest(t.Context(), "request", "fingerprint")
	if err != nil || r.Fresh || r.Response != `{"id":"shared"}` {
		t.Fatalf("restart receipt: %#v %v", r, err)
	}
	if _, err = reopened.ReserveRequest(t.Context(), "request", "changed"); err == nil {
		t.Fatal("id reused for different request")
	}
}
