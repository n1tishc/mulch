//go:build windows

package tool

import "os/exec"

// Bash refuses to launch on Windows because no containment backend is available;
// keep the command buildable so the rest of Mulch can ship for trace operations.
func configureProcessCancellation(_ *exec.Cmd) {}

func finishProcessCancellation(_ *exec.Cmd) error { return nil }
