package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultBashTimeout = 60 * time.Second
	DefaultMaxOutput   = 16 * 1024
)

type Bash struct {
	workdir   string
	Timeout   time.Duration
	MaxOutput int
}

func NewBash(workdir string) *Bash {
	return &Bash{workdir: workdir, Timeout: DefaultBashTimeout, MaxOutput: DefaultMaxOutput}
}
func (*Bash) Name() string { return "bash" }
func (*Bash) Description() string {
	return "Run a shell command in the working directory with a timeout and bounded output."
}
func (*Bash) InputSchema() map[string]any {
	return schema(map[string]any{"command": map[string]any{"type": "string"}}, "command")
}

func (b *Bash) Run(ctx context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return invalidInput(err)
	}
	if blockedCommand(in.Command) {
		return Result{Output: "blocked destructive command", IsError: true}, nil
	}
	if commandEscapesWorkdir(b.workdir, in.Command) {
		return Result{Output: "command path is outside workdir", IsError: true}, nil
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = DefaultBashTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	root, err := filepath.Abs(b.workdir)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	cacheDir := filepath.Join(root, ".mulch-cache")
	if err := os.MkdirAll(filepath.Join(cacheDir, "tmp"), 0700); err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	executable, args, err := sandboxedShell(root, in.Command)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	cmd := exec.CommandContext(runCtx, executable, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TMPDIR="+filepath.Join(cacheDir, "tmp"), "GOCACHE="+filepath.Join(cacheDir, "go-build"), "XDG_CACHE_HOME="+cacheDir)
	configureProcessCancellation(cmd)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err = cmd.Run()
	// A shell can exit while a background descendant keeps stdout open.
	// WaitDelay bounds the pipe wait; explicitly shut down that group too.
	if errors.Is(err, exec.ErrWaitDelay) && cmd.Cancel != nil {
		_ = cmd.Cancel()
	}
	var cleanupErr error
	if runCtx.Err() != nil || errors.Is(err, exec.ErrWaitDelay) {
		cleanupErr = finishProcessCancellation(cmd)
	}
	text := output.String()
	limit := b.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	if len(text) > limit {
		text = text[:limit] + "\n[output truncated]"
	}
	if cleanupErr != nil {
		text += "\n" + cleanupErr.Error()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		return Result{Output: text + "\ncommand timed out", IsError: true}, nil
	}
	if runCtx.Err() == context.Canceled {
		return Result{Output: text + "\ncommand cancelled", IsError: true}, nil
	}
	if err != nil {
		return Result{Output: strings.TrimSpace(text + "\n" + err.Error()), IsError: true}, nil
	}
	return Result{Output: text}, nil
}

func sandboxedShell(workdir, command string) (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		profilePath := strings.ReplaceAll(strings.ReplaceAll(workdir, `\`, `\\`), `"`, `\"`)
		profile := fmt.Sprintf(`(version 1) (allow default) (deny file-write*) (allow file-write* (subpath "%s")) (allow file-write* (literal "/dev/null"))`, profilePath)
		return "/usr/bin/sandbox-exec", []string{"-p", profile, "/bin/sh", "-c", command}, nil
	case "linux":
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			return "", nil, errors.New("bash containment requires bubblewrap (bwrap)")
		}
		return bwrap, []string{"--die-with-parent", "--ro-bind", "/", "/", "--bind", workdir, workdir, "--dev", "/dev", "--proc", "/proc", "--chdir", workdir, "/bin/sh", "-c", command}, nil
	default:
		return "", nil, fmt.Errorf("bash containment is unavailable on %s", runtime.GOOS)
	}
}

func commandEscapesWorkdir(workdir, command string) bool {
	if strings.Contains(command, "$HOME") || strings.Contains(command, "${HOME}") {
		return true
	}
	for _, field := range strings.Fields(command) {
		candidate := strings.Trim(field, "'\"`;|&()<>")
		candidate = strings.TrimLeft(candidate, ">")
		if candidate == ".." || strings.HasPrefix(candidate, "../") || strings.Contains(candidate, "/../") || strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "~") {
			return true
		}
		if candidate != "" && !strings.HasPrefix(candidate, "-") {
			if _, err := safePath(workdir, candidate, true); err != nil {
				return true
			}
		}
	}
	return false
}

func blockedCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	patterns := []string{"rm -rf /", "rm -fr /", "git reset --hard", "git clean -f", "mkfs", ":(){:|:&};:"}
	for _, pattern := range patterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}
