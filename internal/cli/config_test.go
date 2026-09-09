package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedConfigurationPrecedenceAndSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	env := func(k string) string {
		if k == "MULCH_CONFIG" {
			return path
		}
		if k == "MULCH_MODEL" {
			return "override"
		}
		return ""
	}
	var out bytes.Buffer
	opts := Options{Getenv: env, Stdin: strings.NewReader("test-secret\n"), Stdout: &out, Stderr: &out}
	for _, args := range [][]string{{"set", "api-key", "--stdin"}, {"set", "model", "saved"}, {"set", "base-url", "https://example.com/v1"}} {
		if err := configure(args, opts); err != nil {
			t.Fatal(err)
		}
	}
	get, err := configuredEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}
	if get("MULCH_MODEL") != "override" || get("MULCH_PROVIDER_API_KEY") != "test-secret" {
		t.Fatal("configuration precedence")
	}
	if err = configure([]string{"show"}, opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "test-secret") {
		t.Fatal("key exposed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("configuration permissions")
	}
	if err = configure([]string{"set", "api-key", "visible-in-history"}, opts); err == nil {
		t.Fatal("accepted a secret argument")
	}
	if err = configure([]string{"set", "base-url", "https://user:secret@example.com"}, opts); err == nil {
		t.Fatal("accepted embedded credentials")
	}
	if err = configure([]string{"unset", "api-key"}, opts); err != nil {
		t.Fatal(err)
	}
	get, err = configuredEnvironment(env)
	if err != nil || get("MULCH_PROVIDER_API_KEY") != "" {
		t.Fatal("unset failed", err)
	}
}
