//go:build !windows

package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcessCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// An inherited pipe must not keep Wait blocked after the command exits.
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		group := -cmd.Process.Pid
		// Stop new forks before killing the group. A single SIGKILL can race
		// with a shell creating a child that inherits its output descriptors.
		err := syscall.Kill(group, syscall.SIGSTOP)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
		err = syscall.Kill(group, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// Wait has reaped the group leader by this point. Bound the remaining wait for
// descendants to be reaped, and surface incomplete cleanup instead of silently
// treating a closed output pipe as proof that all processes have exited.
func finishProcessCancellation(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		err := syscall.Kill(-cmd.Process.Pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("verify command process-group shutdown: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("command process group still exists after shutdown")
		}
		time.Sleep(time.Millisecond)
	}
}
