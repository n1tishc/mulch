package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n1tishc/mulch/internal/prompt"
)

func TestAssembleDiscoversInstructionsNearestLast(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "project")
	workdir := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(home, ".mulch", "agent"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workdir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(home, ".mulch", "agent", "AGENTS.md"): "global instructions",
		filepath.Join(root, "AGENTS.md"):                    "parent instructions",
		filepath.Join(workdir, "AGENTS.md"):                 "nearest instructions",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	assembled, err := prompt.Assemble(workdir, home)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(assembled.Text, "global instructions") >= strings.Index(assembled.Text, "parent instructions") || strings.Index(assembled.Text, "parent instructions") >= strings.Index(assembled.Text, "nearest instructions") {
		t.Fatalf("instruction order:\n%s", assembled.Text)
	}
	if len(assembled.Sources) != 3 {
		t.Fatalf("sources = %#v", assembled.Sources)
	}
	for _, source := range assembled.Sources {
		if source.Path == "" || source.ModifiedAt.IsZero() {
			t.Fatalf("source = %#v", source)
		}
	}
}

func TestSystemOverrideReplacesDefault(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "SYSTEM.md"), []byte("custom system"), 0644); err != nil {
		t.Fatal(err)
	}
	assembled, err := prompt.Assemble(workdir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(assembled.Text, "You are Mulch") || !strings.HasPrefix(assembled.Text, "custom system") {
		t.Fatalf("prompt = %q", assembled.Text)
	}
	if len(assembled.Sources) != 1 || filepath.Base(assembled.Sources[0].Path) != "SYSTEM.md" {
		t.Fatalf("sources = %#v", assembled.Sources)
	}
}

func TestLocalPromptExcludesAncestorInstructions(t *testing.T) {
	root := t.TempDir()
	wd := filepath.Join(root, "fixture")
	if err := os.Mkdir(wd, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("PRIVATE_PARENT_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "AGENTS.md"), []byte("LOCAL_FIXTURE_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	assembled, err := prompt.AssembleLocal(wd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(assembled.Text, "PRIVATE_PARENT_SENTINEL") || !strings.Contains(assembled.Text, "LOCAL_FIXTURE_SENTINEL") {
		t.Fatalf("prompt=%s", assembled.Text)
	}
}
