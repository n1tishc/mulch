package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/tool"
)

func TestFileToolsStayInsideWorkdir(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		run  func() (tool.Result, error)
	}{
		{"read", func() (tool.Result, error) { return tool.NewRead(root).Run(t.Context(), raw(`{"path":"../secret"}`)) }},
		{"write", func() (tool.Result, error) {
			return tool.NewWrite(root).Run(t.Context(), raw(`{"path":"../secret","content":"bad"}`))
		}},
		{"edit", func() (tool.Result, error) {
			return tool.NewEdit(root).Run(t.Context(), raw(`{"path":"../secret","old":"a","new":"b"}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(result.Output, "outside workdir") {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestReadWriteAndExactEdit(t *testing.T) {
	root := t.TempDir()
	writeResult, err := tool.NewWrite(root).Run(t.Context(), raw(`{"path":"nested/hello.txt","content":"hello world\n"}`))
	if err != nil || writeResult.IsError {
		t.Fatalf("write = %#v, %v", writeResult, err)
	}
	readResult, err := tool.NewRead(root).Run(t.Context(), raw(`{"path":"nested/hello.txt"}`))
	if err != nil || readResult.Output != "hello world\n" || readResult.SourceTS.IsZero() {
		t.Fatalf("read = %#v, %v", readResult, err)
	}
	editResult, err := tool.NewEdit(root).Run(t.Context(), raw(`{"path":"nested/hello.txt","old":"world","new":"mulch"}`))
	if err != nil || editResult.IsError || !strings.Contains(editResult.Output, "-hello world") || !strings.Contains(editResult.Output, "+hello mulch") {
		t.Fatalf("edit = %#v, %v", editResult, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "nested", "hello.txt"))
	if string(got) != "hello mulch\n" {
		t.Fatalf("content = %q", got)
	}
	for _, content := range []string{"no match", "world world"} {
		_ = os.WriteFile(filepath.Join(root, "nested", "hello.txt"), []byte(content), 0600)
		result, err := tool.NewEdit(root).Run(t.Context(), raw(`{"path":"nested/hello.txt","old":"world","new":"x"}`))
		if err != nil || !result.IsError || !strings.Contains(result.Output, "exactly once") {
			t.Fatalf("edit %q = %#v, %v", content, result, err)
		}
	}
}

func TestBashSafetyTimeoutAndTruncation(t *testing.T) {
	root := t.TempDir()
	b := tool.NewBash(root)
	if b.Timeout != 60*time.Second || b.MaxOutput != 16*1024 {
		t.Fatalf("defaults: timeout=%s output=%d", b.Timeout, b.MaxOutput)
	}
	for _, command := range []string{"rm -rf /", "git reset --hard", "git clean -fd"} {
		result, err := b.Run(t.Context(), rawJSON(t, map[string]string{"command": command}))
		if err != nil || !result.IsError || !strings.Contains(result.Output, "blocked") {
			t.Fatalf("command %q = %#v, %v", command, result, err)
		}
	}
	for _, command := range []string{"touch ../escaped", "touch /tmp/escaped", "touch ~/escaped"} {
		result, err := b.Run(t.Context(), rawJSON(t, map[string]string{"command": command}))
		if err != nil || !result.IsError || !strings.Contains(result.Output, "outside workdir") {
			t.Fatalf("escape %q = %#v, %v", command, result, err)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape-link")); err != nil {
		t.Fatal(err)
	}
	result, err := b.Run(t.Context(), raw(`{"command":"touch escape-link/file"}`))
	if err != nil || !result.IsError || !strings.Contains(result.Output, "outside workdir") {
		t.Fatalf("symlink escape = %#v, %v", result, err)
	}
	b.Timeout = time.Second
	b.MaxOutput = 32
	result, err = b.Run(t.Context(), raw(`{"command":"printf 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'"}`))
	if err != nil || result.IsError || !strings.Contains(result.Output, "[output truncated]") || len(result.Output) > 80 {
		t.Fatalf("truncation = %#v, %v", result, err)
	}
}

func TestBashSandboxRejectsExpandedOutsideWrite(t *testing.T) {
	root, outside := t.TempDir(), filepath.Join(t.TempDir(), "escaped")
	command := fmt.Sprintf("target=%q; touch \"$target\"", outside)
	result, err := tool.NewBash(root).Run(t.Context(), rawJSON(t, map[string]string{"command": command}))
	if err != nil || !result.IsError {
		t.Fatalf("result = %#v, %v", result, err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestFileToolsHonorCancelledContext(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	for _, candidate := range []tool.Tool{tool.NewRead(root), tool.NewWrite(root), tool.NewEdit(root)} {
		result, err := candidate.Run(ctx, raw(`{"path":"file","content":"x","old":"x","new":"y"}`))
		if err != nil || !result.IsError || !strings.Contains(result.Output, "cancelled") {
			t.Fatalf("%s = %#v, %v", candidate.Name(), result, err)
		}
	}
}

func TestEditReportsMultilineChange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("before\nold one\nold two\nafter\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewEdit(root).Run(t.Context(), raw(`{"path":"file.txt","old":"old one\nold two","new":"new one\nnew two"}`))
	if err != nil || result.IsError || !strings.Contains(result.Output, "-old one\n-old two") || !strings.Contains(result.Output, "+new one\n+new two") {
		t.Fatalf("edit = %#v, %v", result, err)
	}
}

func TestBashParentCancellationKillsProcessGroup(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan tool.Result, 1)
	go func() {
		result, _ := tool.NewBash(root).Run(ctx, raw(`{"command":"echo $$ > shell.pid; sleep 30"}`))
		done <- result
	}()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(root, "shell.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("bash process did not start")
	}
	group, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })
	cancel()
	var result tool.Result
	select {
	case result = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled bash did not return within watchdog deadline")
	}
	if !result.IsError || !strings.Contains(result.Output, "cancelled") {
		t.Fatalf("result = %#v", result)
	}
	for i := 0; i < 100 && syscall.Kill(-group, 0) == nil; i++ {
		time.Sleep(time.Millisecond)
	}
	if err := syscall.Kill(-group, 0); err == nil {
		t.Fatalf("process group %d still exists", group)
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
