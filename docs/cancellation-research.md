# Cancellation timeout research

## Executive conclusion

**Implementation update, 2026-09-13:** the recommended staged shutdown is now implemented. The final code uses a 10ms group-stop settle interval, group SIGKILL, one-second WaitDelay, ESRCH normalization, and up to 250ms to verify group disappearance after Wait reaps the leader. An expired output-pipe wait also triggers group shutdown when a shell exits leaving a background descendant. Cleanup verification failures are surfaced in the tool result. These bounds are watchdogs, not guarantees that every OS scheduler will meet a latency target.

The tool cancellation stress passed 100/100, the agent process cancellation stress passed 100/100, and the separate synchronized agent bookkeeping test passed 100/100 with the race detector. The direct tool test now waits for a nonempty PID file and resolves the actual PGID (important for Linux's wrapper process). It has a bounded wait and failure cleanup. CI now repeats the OS tests on macOS and Linux; Linux execution is pending. The official Go and POSIX references below were rechecked in Ego Browser while implementing the fix. See [release verification](release-verification.md).

The two-second assertion is too broad to diagnose the cause by itself: it measures the whole `agent.Run` shutdown path after `cancel()`. However, follow-up stress testing established that this is **not merely a strict timeout**. A focused `Bash.Run` test hung beyond 30 seconds, and its goroutine dump showed `os/exec.(*Cmd).awaitGoroutines` blocked copying stdout into the `bytes.Buffer`. A concurrent process snapshot showed an orphaned `sleep 30` under PID 1, still in the killed group's PGID and still holding the output pipe open.

The original defect was therefore a **release-blocking cancellation leak**, not merely an overly strict assertion. Merely changing `2s` to a larger constant lets the orphan run until its natural 30-second exit. The investigation identified three parts to the fix:

1. Harden production command execution against the observed fork-versus-group-kill race.
2. Add a non-zero `exec.Cmd.WaitDelay` and cancellation-race-safe error handling as a final bound on inherited pipes.
3. Split the current test into deterministic agent bookkeeping coverage and a focused OS-process integration test with a test-deadline-derived watchdog, phase diagnostics, and eventual process-group cleanup checks.

## Local reproduction evidence

The investigation used the unmodified production path on Darwin arm64 with Go 1.27.1:

- `go test ./internal/agent -run '^TestRunCancelsAllToolProcessGroupsAndRecordsSession$' -count=100` failed 8/100 times at two seconds.
- Raising the diagnostic watchdog to ten seconds did not resolve the issue; multiple iterations still failed at ten seconds.
- `go test ./internal/tool -run '^TestBashParentCancellationKillsProcessGroup$' -count=100 -timeout=30s -v` timed out. The goroutine dump placed `Bash.Run` in `exec.Cmd.Wait` → `awaitGoroutines`, with the output-copy goroutine blocked reading a pipe.
- During a separate live hang, `ps` showed `sleep 30` orphaned to PID 1. Its process-group leader was gone, but the sleep remained in that PGID and kept the inherited output descriptor open. Killing that exact group immediately unblocked the test.
- Adding `WaitDelay = 250ms` alone made commands return quickly, but 15/100 iterations returned while the process group still existed. This is an availability bound, not a complete cleanup fix.
- A temporary `SIGSTOP`-then-`SIGKILL` group sequence with a 10ms settle interval plus a one-second `WaitDelay` passed 100/100 focused tool cancellations and 100/100 agent-level cancellations. A separate 200-run attempt had one pre-cancellation fixture failure because the shell never wrote its readiness file; cancellation itself did not fail in that run.

These measurements describe the initial investigation, not cross-platform guarantees. The temporary changes were reverted then; the permanent implementation and later validation are recorded in the update above.

## What Go guarantees—and what it does not

### `CommandContext` only provides a cancellation trigger

[`exec.CommandContext`](https://pkg.go.dev/os/exec#CommandContext) associates a context with a command. When the context becomes done, Go calls `Cmd.Cancel`; its default implementation calls `Process.Kill`. Callers may replace `Cancel`, as Mulch does.

The default is not sufficient for shell trees: [`os.Process.Kill`](https://pkg.go.dev/os#Process.Kill) explicitly kills only that process, does not kill processes it started, and does not wait for termination. This is why Mulch's process-group override is necessary.

This is not a promise that `Run` or `Wait` returns within a fixed wall-clock interval. In particular, [`Cmd.WaitDelay`](https://pkg.go.dev/os/exec#Cmd.WaitDelay) exists because `Wait` can otherwise be delayed by either:

- a child that does not exit after cancellation; or
- a child that exits while another process still holds an inherited I/O pipe open.

`WaitDelay` starts when the context is done or `Wait` observes process exit, whichever happens first. When it expires, Go kills the direct child if necessary and forcibly closes still-open I/O pipes. Its default is zero, which means pipe reads can wait until orphaned subprocesses close their descriptors.

This applies directly to Mulch. `internal/tool/bash.go` assigns a `bytes.Buffer` to `Stdout` and `Stderr`. The [`exec.Cmd` documentation](https://pkg.go.dev/os/exec#Cmd) says that when these are not `*os.File`, `Wait` also waits for the goroutines copying output into those writers. The [Go implementation](https://go.dev/src/os/exec/exec.go#L974) shows that `awaitGoroutines` waits without a bound when `WaitDelay == 0`, and closes the parent pipe descriptors when the delay expires.

The Go proposal that introduced this behavior explicitly calls out inherited pipes and process-tree shutdown as distinct problems; it notes that platform-specific process-group shutdown remains the caller's responsibility ([golang/go#50436](https://github.com/golang/go/issues/50436)).

### The existing process-group strategy is sound on Unix

Mulch sets `SysProcAttr.Setpgid = true`. Go's Darwin `SysProcAttr` contract specifies that with `Pgid == 0`, the new child's process-group ID is its PID ([Go syscall source](https://go.dev/src/syscall/exec_libc2.go#L20)). Mulch can therefore address that group as `-cmd.Process.Pid`.

POSIX specifies that `kill` with a negative PID other than `-1` sends the signal to processes whose process-group ID is the absolute value ([POSIX `kill`](https://pubs.opengroup.org/onlinepubs/009604499/functions/kill.html)). Apple's Darwin manual documents the same negative-PID behavior ([Apple `kill(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/kill.2.html)) and documents `setpgid` as the operation that assigns a process to a process group ([Apple `setpgid(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/setpgid.2.html)).

Using `SIGKILL` means a target cannot catch or ignore the signal ([POSIX `<signal.h>`](https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/signal.h.html)). Therefore, once the group signal succeeds, a 30-second `sleep` in that group cannot elect to keep running. Scheduling, process reaping, I/O-copy completion, and post-cancel persistence can still add latency.

A process group is not a security boundary: a descendant can deliberately enter another process group or session. The present design correctly covers ordinary shell descendants, but adversarial containment would require stronger platform-specific facilities.

### `WaitDelay` complements, rather than replaces, group killing

Go's `WaitDelay` fallback calls `Process.Kill` on the direct process; it does not promise to kill an entire Unix process group. Mulch should retain its group-targeted cancellation and add `WaitDelay` as a final bound on `Cmd.Run`, especially on pipe-copy goroutines.

If Mulch later changes cancellation to send `SIGTERM` first, `WaitDelay` alone is not a complete group escalation mechanism: after the grace period Go only kills the direct child. A graceful design would need an explicit group-level `SIGKILL` escalation. The current immediate group `SIGKILL` is simpler and appropriate when the product contract is "cancel now."

## What the current test actually proves

`internal/agent/loop_test.go` currently performs all of these in one test:

1. Starts two real shell commands through the macOS sandbox wrapper.
2. Waits for each shell to write a PID file.
3. Cancels the top-level agent context.
4. Requires the complete `agent.Run` call to return in under two seconds.
5. Reads SQLite to verify the cancelled session and two cancelled tool-result events.
6. Uses signal `0` to verify that both process groups no longer exist.

This is useful end-to-end coverage, but the two-second failure message cannot identify which phase was late. `tool.Executor.RunAll` waits for every tool goroutine. After that, `agent.Run` synchronously appends tool-result and session-end events and ends the session using `context.WithoutCancel`. The store serializes those writes through its writer goroutine. The test therefore conflates OS process cleanup with output copying and durable bookkeeping.

The Go team's guidance is that wall-clock tests of asynchronous behavior are inherently slow or flaky under scheduler or CI load, and recommends explicit synchronization or a way to wait for quiescence rather than sleeping to an exact time ([Go blog: Testing Time](https://go.dev/blog/testing-time)). The external-process portion cannot use fake time, but the agent bookkeeping portion can use a context-aware fake tool and explicit channels.

Go's own subprocess test helper uses the test's overall deadline, reserves cleanup time, adds a command context deadline, sets `WaitDelay`, and verifies that the subprocess was waited for ([Go `internal/testenv.CommandContext` source](https://go.dev/src/internal/testenv/exec.go#L163)). Mulch cannot import an `internal` Go package, but it can follow that design.

## Recommended production fix

### 1. Close the fork-versus-kill race

The observed orphan was in the intended PGID, so the basic group selection was correct. The likely race is cancellation while the shell or sandbox wrapper is launching its final descendant: a single group-directed `SIGKILL` can kill the current members while a concurrently created child survives in that group.

The smallest promising Unix strategy is a bounded two-stage group shutdown:

1. Send `SIGSTOP` to the process group, preventing current members from continuing to fork.
2. After a short bounded settle interval, send `SIGKILL` to the same group.
3. Keep a `WaitDelay` fallback to close inherited pipes.
4. Before reporting cleanup success, poll the known PGID until `kill(-pgid, 0)` returns `ESRCH`, within a bounded deadline.

The temporary version of this strategy passed the local 100-run tool and agent stress loops. It still needs permanent tests on both macOS and Linux before adoption. Do not use an unbounded polling loop, and ensure failure cleanup sends a final group `SIGKILL`.

An alternative is a dedicated supervisor or containment primitive that owns the whole tree. That is architecturally stronger, especially against descendants that deliberately change sessions or process groups, but it is a larger platform-specific change.

### 2. Add a non-zero `WaitDelay`

Set `cmd.WaitDelay` in `configureProcessCancellation`, using a named package constant. A value around one to two seconds is a reasonable starting safety bound for immediate-`SIGKILL` cancellation; benchmark and tune it on supported CI platforms. It is not a normal grace period—the group `SIGKILL` should normally make `Run` return much earlier—but it prevents an inherited output descriptor from hanging the command indefinitely.

Conceptually:

```go
const processWaitDelay = 2 * time.Second

func configureProcessCancellation(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    cmd.WaitDelay = processWaitDelay
    cmd.Cancel = func() error {
        if cmd.Process == nil {
            return nil
        }
        err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
        if errors.Is(err, syscall.ESRCH) {
            return os.ErrProcessDone
        }
        return err
    }
}
```

The `ESRCH` mapping handles the normal race in which the command or group has already exited. The [`Cmd.Cancel` contract](https://pkg.go.dev/os/exec#Cmd.Cancel) gives special meaning to errors equivalent to `os.ErrProcessDone`; other errors may be wrapped and returned by `Wait`. This change should have a targeted test.

Do not rely on `WaitDelay` as the only descendant cleanup mechanism, and do not replace the group signal with `cmd.Process.Kill()` on Unix.

### 3. Keep platform semantics explicit

The existing Unix-specific file is the correct seam. If Windows support is added, it needs its own process-tree mechanism; Unix process groups and negative PIDs are not portable Windows semantics.

### 4. Keep phase observability

On watchdog failure, record at least:

- elapsed time from `cancel()` until each `Cmd.Cancel` starts and returns;
- elapsed time until each `cmd.Run()` returns;
- elapsed time until `RunAll` returns;
- elapsed time spent appending cancellation events and ending the session;
- command PID and PGID; and
- a goroutine dump plus a process snapshot scoped to those PGIDs.

The local goroutine dump and process snapshot identified this occurrence, but keeping these diagnostics on watchdog failure will distinguish future regressions from store or scheduler delays.

## Recommended test redesign

### A. Deterministic agent cancellation/bookkeeping test

Replace real bash commands in the agent-layer test with two controlled fake tools that:

- close a `started` channel when running;
- block on `<-ctx.Done()`;
- close a `stopped` channel and return a cancelled result.

Wait for both `started` signals, cancel, then wait for both `stopped` signals. Assert the session status and two persisted cancellation results. This tests `agent.Run`, `Executor.RunAll`, and event persistence without making a two-second process-scheduling claim.

### B. Focused Unix process-group integration test

Keep one separate test around `Bash.Run`/`configureProcessCancellation` that launches a shell plus a descendant, confirms their PGID, cancels, waits for `Run` under a watchdog derived from `t.Deadline()`, and eventually asserts that `kill(-pgid, 0)` reports no group.

Use a generous outer watchdog (for example, five to ten seconds locally, scaled from remaining test time in CI), while asserting separately that the configured production `WaitDelay` is small. This preserves a strict product bound without equating it to a scheduler-sensitive end-to-end wall-clock threshold.

The timeout branch should dump diagnostic state before failing. Always register cleanup that sends a final group `SIGKILL`, so a failed test cannot leave a 30-second sleeper behind.

### C. Stress test outside the basic release gate

Run the process integration test repeatedly (`-count=100`) as a stress or nightly job and report latency percentiles. Keep the deterministic test in the basic gate. If a production SLO is desired, measure cancellation latency in an environment-controlled benchmark rather than using a universal two-second assertion in a correctness test.

## Release decision

Recommended classification:

- **Current production behavior:** release blocker; a real orphaned descendant and unbounded output-pipe wait were observed.
- **Current two-second assertion:** too broad and scheduler-sensitive as an end-to-end test, even though it exposed a real defect.
- **Zero `WaitDelay` in production:** real hardening gap because Go documents an unbounded inherited-pipe case.
- **Any observed descendant still alive after a generous cleanup deadline:** release blocker.
- **Any `Bash.Run` still blocked beyond the configured `WaitDelay` plus small scheduling tolerance:** release blocker.
- **Cancellation bookkeeping occasionally taking over two seconds while processes are already gone:** performance/observability issue; investigate, but do not label it a process-safety failure.

## Proposed acceptance criteria

1. A deterministic agent cancellation test passes repeatedly without real-time sleeps.
2. The Unix integration test proves the shell and descendant process group disappears after cancellation.
3. `Cmd.WaitDelay` bounds inherited-pipe waits, with coverage for a descendant that retains stdout/stderr.
4. Cancellation races where the group has already exited do not surface a spurious cancellation error.
5. A 100-run stress job has no leaked process groups and emits latency distribution data.
6. A watchdog failure prints enough phase and process data to locate the delayed component.
