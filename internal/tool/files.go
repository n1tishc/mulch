package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Read struct{ workdir string }
type Write struct{ workdir string }
type Edit struct{ workdir string }

func NewRead(workdir string) *Read   { return &Read{workdir: workdir} }
func NewWrite(workdir string) *Write { return &Write{workdir: workdir} }
func NewEdit(workdir string) *Edit   { return &Edit{workdir: workdir} }

func (*Read) Name() string  { return "read" }
func (*Write) Name() string { return "write" }
func (*Edit) Name() string  { return "edit" }
func (*Read) Description() string {
	return "Read a UTF-8 text file relative to the working directory, optionally selecting lines."
}
func (*Write) Description() string {
	return "Write a UTF-8 text file relative to the working directory, creating parent directories."
}
func (*Edit) Description() string {
	return "Replace one exact occurrence in a text file and return a diff."
}
func (*Read) InputSchema() map[string]any {
	return schema(map[string]any{"path": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 1}, "limit": map[string]any{"type": "integer", "minimum": 1}}, "path")
}
func (*Write) InputSchema() map[string]any {
	return schema(map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "path", "content")
}
func (*Edit) InputSchema() map[string]any {
	return schema(map[string]any{"path": map[string]any{"type": "string"}, "old": map[string]any{"type": "string"}, "new": map[string]any{"type": "string"}}, "path", "old", "new")
}

func (r *Read) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	if cancelled := cancellation(ctx); cancelled != nil {
		return *cancelled, nil
	}
	var in struct {
		Path          string `json:"path"`
		Offset, Limit int
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return invalidInput(err)
	}
	path, err := safePath(r.workdir, in.Path, false)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	text := string(b)
	if in.Offset > 0 || in.Limit > 0 {
		lines := strings.SplitAfter(text, "\n")
		start := max(in.Offset-1, 0)
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if in.Limit > 0 && start+in.Limit < end {
			end = start + in.Limit
		}
		text = strings.Join(lines[start:end], "")
	}
	return Result{Output: text, SourceTS: info.ModTime()}, nil
}

func (w *Write) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	if cancelled := cancellation(ctx); cancelled != nil {
		return *cancelled, nil
	}
	var in struct{ Path, Content string }
	if err := json.Unmarshal(input, &in); err != nil {
		return invalidInput(err)
	}
	path, err := safePath(w.workdir, in.Path, true)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	if cancelled := cancellation(ctx); cancelled != nil {
		return *cancelled, nil
	}
	if err := os.WriteFile(path, []byte(in.Content), 0644); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), SourceTS: info.ModTime()}, nil
}

func (e *Edit) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	if cancelled := cancellation(ctx); cancelled != nil {
		return *cancelled, nil
	}
	var in struct{ Path, Old, New string }
	if err := json.Unmarshal(input, &in); err != nil {
		return invalidInput(err)
	}
	path, err := safePath(e.workdir, in.Path, false)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	if in.Old == "" || strings.Count(string(b), in.Old) != 1 {
		return Result{Output: "old text must occur exactly once", IsError: true}, nil
	}
	if cancelled := cancellation(ctx); cancelled != nil {
		return *cancelled, nil
	}
	after := strings.Replace(string(b), in.Old, in.New, 1)
	if err := os.WriteFile(path, []byte(after), 0644); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	oldShown, newShown := in.Old, in.New
	if !strings.Contains(in.Old, "\n") {
		oldShown = containingLine(string(b), strings.Index(string(b), in.Old))
		newShown = strings.Replace(oldShown, in.Old, in.New, 1)
	}
	diff := fmt.Sprintf("--- %s\n+++ %s\n@@ exact replacement @@\n%s\n%s", in.Path, in.Path, prefixLines("-", oldShown), prefixLines("+", newShown))
	return Result{Output: diff, SourceTS: info.ModTime()}, nil
}

func containingLine(text string, at int) string {
	start := strings.LastIndex(text[:at], "\n") + 1
	end := strings.Index(text[at:], "\n")
	if end < 0 {
		return text[start:]
	}
	return text[start : at+end]
}

func prefixLines(prefix, text string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

func cancellation(ctx context.Context) *Result {
	select {
	case <-ctx.Done():
		result := Result{Output: "operation cancelled", IsError: true}
		return &result
	default:
		return nil
	}
}

func safePath(workdir, name string, allowMissing bool) (string, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("path is outside workdir")
	}
	target := filepath.Join(root, filepath.Clean(name))
	lexicalRel, err := filepath.Rel(root, target)
	if err != nil || lexicalRel == ".." || strings.HasPrefix(lexicalRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside workdir")
	}
	check := target
	if allowMissing {
		for {
			resolved, resolveErr := filepath.EvalSymlinks(check)
			if resolveErr == nil {
				check = resolved
				break
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", resolveErr
			}
			check = parent
		}
	} else {
		check, err = filepath.EvalSymlinks(target)
		if err != nil {
			return target, nil
		}
		target = check
	}
	rel, err := filepath.Rel(root, check)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside workdir")
	}
	return target, nil
}
