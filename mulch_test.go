package mulch_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch"
)

func TestPublicHarnessCanOpenAndCloseWithoutInternalPackages(t *testing.T) {
	h, err := mulch.Open(mulch.Options{DB: filepath.Join(t.TempDir(), "mulch.db"), APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicHarnessValidatesRequiredConfiguration(t *testing.T) {
	if _, err := mulch.Open(mulch.Options{APIKey: "test"}); err == nil {
		t.Fatal("Open accepted an empty DB")
	}
}

func TestPublicHarnessExposesServe(t *testing.T) {
	h, err := mulch.Open(mulch.Options{DB: filepath.Join(t.TempDir(), "mulch.db"), APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Serve(ctx, "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}
