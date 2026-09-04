package cli

import (
	"context"
	"strings"
	"testing"
)

func TestServeValidatesAddressBeforeOpeningDatabase(t *testing.T) {
	err := Execute(context.Background(), []string{"serve", "--addr", "bad address", "--db", t.TempDir() + "/mulch.db"}, Options{Getenv: func(string) string { return "" }})
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("error = %v", err)
	}
}
