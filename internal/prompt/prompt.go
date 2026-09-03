package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultText = `You are Mulch, a concise coding agent.
Work only inside the provided working directory.
Use read, write, edit, and bash to inspect and change files.
Prefer small, verifiable changes and be terse.`

type Source struct {
	Path       string    `json:"path"`
	ModifiedAt time.Time `json:"modified_at"`
}

type Assembled struct {
	Text    string
	Sources []Source
}

func Assemble(workdir, home string) (Assembled, error) {
	cwd, err := filepath.Abs(workdir)
	if err != nil {
		return Assembled{}, fmt.Errorf("resolve workdir: %w", err)
	}
	text := defaultText
	var sources []Source
	systemPath := filepath.Join(cwd, "SYSTEM.md")
	if b, info, readErr := readSource(systemPath); readErr == nil {
		text = string(b)
		sources = append(sources, Source{Path: systemPath, ModifiedAt: info.ModTime()})
	} else if !os.IsNotExist(readErr) {
		return Assembled{}, readErr
	}

	paths := []string{filepath.Join(home, ".mulch", "agent", "AGENTS.md")}
	var ancestors []string
	for dir := cwd; ; dir = filepath.Dir(dir) {
		ancestors = append(ancestors, filepath.Join(dir, "AGENTS.md"))
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		paths = append(paths, ancestors[i])
	}
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		b, info, readErr := readSource(path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return Assembled{}, readErr
		}
		text = strings.TrimRight(text, "\n") + "\n\n" + string(b)
		sources = append(sources, Source{Path: path, ModifiedAt: info.ModTime()})
	}
	return Assembled{Text: text, Sources: sources}, nil
}

func readSource(path string) ([]byte, os.FileInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	return b, info, nil
}
