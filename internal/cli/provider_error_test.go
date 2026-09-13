package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n1tishc/mulch/internal/cli"
)

func TestChatRedactsProviderCredentialErrors(t *testing.T) {
	const key = "synthetic-provider-key-not-a-real-secret"
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
			"message": "Invalid API key: " + key, "type": "authentication_error",
		}})
	}))
	defer endpoint.Close()
	var output bytes.Buffer
	db := filepath.Join(t.TempDir(), "sessions.db")
	err := cli.Execute(t.Context(), []string{"chat", "--db", db, "--workdir", t.TempDir()}, cli.Options{
		Stdin: strings.NewReader("/mode plain\nhello\n/exit\n"), Stdout: &output, Stderr: &output,
		Getenv: func(name string) string {
			switch name {
			case "MULCH_PROVIDER_API_KEY":
				return key
			case "MULCH_PROVIDER_BASE_URL":
				return endpoint.URL
			case "MULCH_MODEL":
				return "fixture-model"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal("chat did not recover from provider failure")
	}
	if strings.Contains(output.String(), key) {
		t.Fatal("provider credential leaked into CLI output")
	}
	if !strings.Contains(output.String(), "401") || !strings.Contains(output.String(), "[redacted]") {
		t.Fatal("safe authentication failure details are missing")
	}
	// Replay through the CLI as well: a safe terminal message is insufficient
	// if the original upstream error was already persisted in the trace.
	output.Reset()
	opts := cli.Options{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }}
	if err := cli.Execute(t.Context(), []string{"sessions", "--db", db}, opts); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(output.String())
	if len(fields) == 0 {
		t.Fatal("failed task was not saved")
	}
	id := fields[0]
	output.Reset()
	if err := cli.Execute(t.Context(), []string{"replay", "--json", "--db", db, id}, opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), key) {
		t.Fatal("provider credential leaked into saved trace")
	}
	if !strings.Contains(output.String(), `"status":"failed"`) {
		t.Fatal("saved trace lost the failed task status")
	}
}
