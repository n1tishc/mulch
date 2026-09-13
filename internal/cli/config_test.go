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

func TestNamedProviderConfigurationSelectsActiveProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	env := func(k string) string {
		if k == "MULCH_CONFIG" {
			return path
		}
		return ""
	}
	opts := Options{Getenv: env, Stdin: strings.NewReader("zen-secret\n"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	for _, args := range [][]string{{"set", "provider", "zen"}, {"set", "provider.zen.base-url", "https://example.com/v1"}, {"set", "provider.zen.models", "fast, smart"}, {"set", "provider.zen.api-key", "--stdin"}, {"set", "provider.local.base-url", "http://localhost:11434/v1"}, {"set", "provider.local.models", "qwen, llama"}} {
		if err := configure(args, opts); err != nil {
			t.Fatal(err)
		}
	}
	get, err := configuredEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}
	if get("MULCH_PROVIDER") != "zen" || get("MULCH_PROVIDER_API_KEY") != "zen-secret" || get("MULCH_PROVIDER_BASE_URL") != "https://example.com/v1" || get("MULCH_MODEL") != "fast" {
		t.Fatal("active provider was not projected into runtime environment")
	}
	profiles, active, err := configuredProviderProfiles(get)
	if err != nil || active != "zen" || len(profiles) != 2 || len(profiles[1].Models) != 2 {
		t.Fatalf("profiles=%#v active=%q err=%v", profiles, active, err)
	}
}

func TestNamedProviderOverridesLegacyModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfig(path, map[string]string{
		"provider": "named", "model": "legacy",
		"provider.named.models": "profile-model,second",
	}); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", "environment-model"} {
		t.Run("override="+override, func(t *testing.T) {
			get, err := configuredEnvironment(func(key string) string {
				switch key {
				case "MULCH_CONFIG":
					return path
				case "MULCH_MODEL":
					return override
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "profile-model"
			if override != "" {
				want = override
			}
			if got := get("MULCH_MODEL"); got != want {
				t.Fatalf("model = %q, want %q", got, want)
			}
		})
	}
}
