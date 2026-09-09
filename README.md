# mulch

Mulch is a small Go coding harness that records durable sessions, scores working context asynchronously, and can intervene when its configured policy detects degraded context. Run `mulch` for the interactive terminal or `mulch web` for the local browser workspace. Both use the same agent runtime; the browser interface is embedded in the native binary.

## Current status and recent changes

**Release status (2026-09-08): this work has not published a public GitHub release or npm package, or created a release tag.** Native binaries, the curl installer, npm/pnpm packaging, and a manual draft-release workflow are prepared locally. Preparing or testing packages is not publication. See [release preparation and verification](docs/releases.md).

Recent implementation work includes:

- **Browser coding workspace:** `mulch web` opens a conversation interface in the launch directory, with streamed Markdown, tool output, saved drafts, same-session follow-ups, stop/steer controls, session rename, commands, and mobile drawers. Reconnects backfill committed events; repeated submissions reuse a durable request identity.
- **Inspectable evidence:** recorded run configuration, correctly scaled score dimensions, freshness and failure categories, exact repair-to-score/context links, historical request visibility, and candidate comparisons. Ordinary tasks remain explicitly **not independently graded**. Terminal `/inspect` opens the same saved conversation as a read-only browser view.
- **Shared execution:** standalone runs, dashboard sessions, and evaluations use the same runtime for scoring and repair; dashboard session start/resume now attaches those hooks.
- **Repair correctness:** confirmation counts distinct scored turns; critical escalation bypasses races; branch pruning preserves exact event occurrences; equal race scores choose Prune deterministically. SQLite/replay and workspace-isolation tests cover these contracts.
- **Independent evaluation:** four modes (`plain`, scoring-only `control`, `intervention`, `race`), clean/degraded coding fixtures, external hidden graders, protected specifications, recorded exposure, retained traces, and accounting across agent/judge/summary/candidate calls.
- **Distribution preparation:** six native platform archives, checksum-verified curl installation, an npm wrapper without lifecycle scripts, npm/pnpm installation checks, and CI-gated draft release staging.

The [first correctness pilot](docs/correctness-pilot-results.md) did **not** establish a correctness advantage. Scorer failures, unreachable repair routing in the tested configuration, and conservative budget stops remain open problems. These changes make the behavior testable and inspectable; they do not make every repair reliable or beneficial.

## Install

**The following registry/release commands are intended for after publication.** Build from source below to test the current work.

```sh
# macOS / Linux: checksummed native binary, no sudo or Node required
curl -fsSL https://github.com/n1tishc/mulch/releases/latest/download/install.sh | sh

# npm registry: platform-specific native binary, no install scripts
pnpm add -g @n1tishc/mulch
mulch version
```

For a project-local, pinned dependency, use `pnpm add -D @n1tishc/mulch@0.1.0` and `pnpm exec mulch version`. npm users can use `npm install -g @n1tishc/mulch`. The wrapper requires Node 22+ and optional dependencies enabled. The curl installer defaults to `$HOME/.local/bin`; override it with `MULCH_INSTALL_DIR`.

The prepared release configuration builds CGO-free archives for Linux, macOS, and Windows on amd64 and arm64 plus `checksums.txt`. The installer verifies SHA-256 before replacing an existing executable. Linux shell execution also requires Bubblewrap (`bubblewrap`) and host support for its sandbox.

The Windows binary supports session, replay, and viewer operations, but its agent Bash tool returns an unavailable error because Mulch does not yet have a Windows containment backend. Use Linux or macOS for tasks that require shell execution.

From source, use a released tag rather than an unpinned moving target:

```sh
go install github.com/n1tishc/mulch/cmd/mulch@v0.1.0
```

Until that tag exists, build a fresh clone with the pinned toolchains declared in `go.mod` and `ui/package-lock.json`:

```sh
git clone https://github.com/n1tishc/mulch.git
cd mulch
cd ui && npm ci && npm test && npm run build && cd ..
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath -o mulch ./cmd/mulch
./mulch version
```

`make verify` additionally runs the architecture checks and golangci-lint v2. `make release-dry` creates all six release targets with GoReleaser v2.

## Quickstart

Build the frontend and binary, then launch the terminal coding interface:

```sh
make ui
go build -o dist/mulch ./cmd/mulch
./dist/mulch --workdir /path/to/your/project
```

Configure provider credentials below first (or use `.env` in the directory where you launch Mulch). Running `mulch` with no subcommand opens the interactive terminal interface; `mulch chat` is an explicit alias. It includes an editable multiline prompt, input history, a slash-command menu, a scrollable transcript, streamed responses, tool results, and task status. Enter sends, Alt+Enter or Ctrl+J adds a newline, Tab completes commands, and PgUp/PgDn scrolls. Esc or Ctrl+C interrupts the current task and clears queued follow-ups; `/exit` or Ctrl+D exits. Pasted text stays in the editor until you send it.

Use `/help`, `/new`, `/sessions`, `/resume ID`, `/status`, `/model NAME`, `/mode`, `/rename NAME`, `/history`, and `/diff` inside the app. New sessions preserve previous conversations; changing the model starts a fresh session. Messages submitted while working queue for the next task. See the [terminal workflow and command reference](docs/terminal-workflow.html).

Reopen a conversation with `./dist/mulch --resume SESSION_ID`. It uses the saved model and working directory. Chat supports `--db`, `--policy`, `--no-intervene`, and `--race`; choose the same repair flags when reopening. It runs locally through the shared runtime, independently of daemon submission. Each task retains the runtime's turn limit; idle chat makes no model calls. The existing `run` command remains available for single-task execution. Redirected input/output or `TERM=dumb` uses the simpler line interface.

Mulch uses provider-neutral configuration. OpenCode Go is the default endpoint/model pair, but any OpenAI-compatible chat endpoint can be selected:

```sh
export MULCH_PROVIDER_API_KEY=your-key
export MULCH_PROVIDER_BASE_URL=https://opencode.ai/zen/go/v1
export MULCH_MODEL=glm-5.3-flash
./mulch run "inspect this repository and explain its architecture"
```

Environment variables can also live in `.env`. Relevance scoring is enabled separately with `MULCH_EMBEDDING_API_KEY`; its base URL and model are configurable through `MULCH_EMBEDDING_BASE_URL` and `MULCH_EMBEDDING_MODEL`. This split allows the chat and embedding providers to differ. See [the provider spike](docs/provider-spike.md) for the adapter decision and live verification command.

Open the browser workspace from the project you want Mulch to work on:

```sh
cd /path/to/your/project
mulch web
# Or print the URL without opening a browser:
mulch web --no-open
# Inspect or continue a saved conversation:
mulch web --session SESSION_ID
```

Use the absolute path to your built binary if `mulch` is not on PATH. The web launcher binds one loopback listener on an available port, prints its URL, and opens the browser. Confirm the working-directory field before sending. With credentials present, Send starts or continues a task; while running, Stop and Steer next turn control daemon-owned work. Closing a tab leaves the task running. Without credentials, saved traces remain readable. `mulch serve --addr 127.0.0.1:4141` remains the explicit server command.

Use `--race` for candidate comparisons, `--no-intervene` for scoring only, or `--policy path.json` for a configured ladder. Model and mode are selected at launch; historical views use each run's recorded configuration. `/inspect` in the terminal opens a read-only view whose server ends when the terminal exits. See the [web workflow, commands, and offline testing guide](docs/web-workflow.html).

## Test the CLI and Web locally

Use a disposable project copy: Mulch can edit files and execute commands. Live provider calls consume credits. After building above, put the binary on PATH (or use its absolute path), then launch from that project:

```sh
cd /path/to/test-project
export MULCH_PROVIDER_API_KEY=your-key
export MULCH_PROVIDER_BASE_URL=https://opencode.ai/zen/go/v1
export MULCH_MODEL=glm-5.3-flash

mulch       # Interactive CLI
# Or, in the same configured shell:
mulch web   # Browser workspace; keep the terminal running
```

Run these checks in both interfaces:

| Check | Expected result |
|---|---|
| Fix a known small bug and run tests | Inspect the diff and independently rerun tests; verify the actual fix, not just the agent's claim. |
| Follow up: “Add a regression test for that fix” | The same conversation retains the previous task's context. |
| Cancel a long task | CLI `/cancel` or Web **Stop** ends execution. Existing edits remain. |
| Exit and reopen a saved session | CLI `/sessions` then `/resume ID`, or Web session selection, restores history and supports follow-ups. |
| Launch separately with invalid credentials | A clear failure appears, no false success is reported, and the interface stays usable. |

**CLI:** check `/help`, `/status`, `/diff`, and `/new`. `/inspect` opens the current session read-only in the browser.

**Web:** refresh during a running task and check for duplicate messages or execution. Test **Steer next turn**, rename, saved drafts, and Cmd/Ctrl+K commands. Closing the tab leaves execution running.

**Repair evidence:** use a longer task and inspect Health, Repairs, and Context. Recorded repairs should link to the score and context change that triggered them; missing or reused scores must be labeled. Short tasks may trigger no repairs.

**Correctness comparison:** repeat identical tasks from clean project copies with CLI `/mode plain`, `/mode control`, and `/mode intervention`, keeping model and budget equal. Compare independently passing tests, regressions, total tokens, and time. Health scores alone do not establish correctness. Use the [benchmark protocol](docs/correctness-benchmark.html) for controlled trials and the offline checks below for mechanics.

## The event-log invariant

The append-only SQLite event log is the source of truth. Prompts, model responses, tool starts/results, visibility changes, health scores, interventions, steering, and session lifecycle changes are recorded before downstream consumers observe them. Existing history is not rewritten: pruning changes visibility, compaction appends a replacement event, and branching creates a child session at a parent sequence. Replay, resume, the terminal UI, and the web viewer are projections of the same log.

Writes pass through one store-owned writer goroutine, and committed events are published to a channel-based bus. This preserves per-session sequence ordering while subscribers such as scoring, the intervention ladder, and live viewers operate away from the core loop.

## Why this harness

The central feature is **auditable context repair**: observe context degradation, apply a bounded intervention, and preserve the evidence needed to reconstruct the decision. Experimental race mode compares Prune and Reanchor in isolated workspace copies and adopts the healthier context mutation.

`make contract` exercises the repair mechanics without provider credentials, including escalation in race mode, distinct score confirmation, exact-event pruning after branching, and replay. This demonstrates the mechanism; the current live evaluation does not yet show a quality improvement. See [the differentiator analysis and evidence plan](docs/harness-differentiator.md) and the [correctness benchmark protocol](docs/correctness-benchmark.md).

## Pi influences

Mulch follows Pi's preference for a minimal loop, explicit tool execution, JSONL-friendly events, extensions around the loop, and sessions modeled as trees. Mulch's additions are infrastructure-oriented concurrency and context-health feedback: hooks score and mutate context without teaching the core agent loop about either policy.

## Context health and the intervention ladder

After a turn, independent scorers fan out for saturation, staleness, relevance, and coherence. Available scores are combined on a 0–100 scale using configurable weights; a failed scorer reuses its previous value where possible. Scoring is pipelined and records both latency and whether completion reached the critical path.

The default ladder requires two confirming low-health observations, uses five points of hysteresis and a two-turn cooldown, and routes according to the weakest health dimension (critical composite health always escalates):

| Composite below | Action | Behavior |
|---:|---|---|
| 80 | warn | inject a compact warning |
| 65 | prune | when relevance is weakest, hide low-value context, capped at 30% |
| 50 | compact | when saturation is weakest and a summarizer is available, append a replacement summary |
| 40 | reanchor | when coherence or staleness is weakest, inject the task anchor again |
| 25 | escalate | cancel rather than continue unsafely |

Pass `--no-intervene` to retain health scoring without ladder actions, or `--policy path.json` to supply a validated policy. Health weights use the `MULCH_HEALTH_WEIGHT_*` variables.

Pass `--race` to make a confirmed Prune, Compact, or Reanchor decision fork one Prune candidate and one Reanchor candidate. Each branch runs one recorded turn concurrently; the healthier context mutation is appended to the parent, the losing branch is retained as `abandoned`, and another race is suppressed for twice the normal intervention cooldown. Equal scores favor Prune deterministically. Critical escalation still cancels the parent without starting a race. Candidate tool edits are not merged into the parent workspace. Race mode is opt-in and cannot be combined with `--no-intervene`.

## Architecture

```text
CLI / daemon / Go API
        |
     agent loop ---- parallel tools
        |
  append-only event store (SQLite)
        |
      event bus
   /      |       |          \
score   ladder  steering   live viewer
   \      /
context events
```

The core loop depends only on hook and provider interfaces. Architecture tests prevent the loop, scoring, and intervention packages from importing each other in the forbidden direction. The viewer assets are built by Vite into `internal/server/ui_dist` and embedded into the Go binary with `go:embed`.

## Measured behavior

On a MacBook Pro `Mac14,9` with Apple M2 Pro, macOS Darwin 25.5.0, Go 1.25.1, and an arm64 CGO-free stripped snapshot binary (16 MiB), 50 process launches of `mulch version` had a 9.628 ms median and 14.653 ms p95. One cold/outlier launch was 399.891 ms, making the mean 17.881 ms. Twenty daemon launches reached a successful `/api/sessions` response in 12.545 ms median and 15.063 ms p95; the maximum was 65.429 ms. Measurements include process creation and, for the daemon, polling every 2 ms. They are local observations, not cross-platform guarantees.

The earlier provider-backed smoke evaluation was deliberately small: five runs per mode, ten total. Both modes succeeded, no interventions fired, scoring was reported on the critical path in 50.0% of control samples and 66.7% of intervention samples, and measured tool parallelism gain was only 1.01x. Those numbers do not establish a general performance advantage. The complete table and reproduction command are in [measured evaluation results](docs/evaluation-results.md).

The subsequent [16-run correctness pilot](docs/correctness-pilot-results.md) exposed gaps: plain execution completed correctly in 3/4 conditions; scoring-only, ladder, and race modes each completed correctly in 0/4. Fourteen of sixteen patches passed hidden tests, but twelve runs hit the conservative budget guard. No interventions or races fired, and coherence repeatedly reused its previous value. These diagnostic results do not support a correctness advantage; scoring reliability, routing reachability, and budget calibration come before that claim.

## Stress testing before publication

Start with the offline checks (no model calls):

```sh
make verify
make contract
make correctness-test
make web-test
make distribution-test
make release-dry
```

These require the development and packaging tools listed in [release verification](docs/releases.md). Passing them validates covered mechanics and packaging, not comparative coding quality. Cross-compilation is not a native runtime test, and Windows shell execution remains unsupported.

Next follow the [stress-test guide](docs/stress-testing.md): repeated concurrency checks, fault and recovery cases, a bounded live diagnostic, and finally repeated trials on held-out tasks. Define success as **normal completion plus an independent passing grader**; retain budget stops, failed scoring, clean-task regressions, and repairs that make an answer worse.

For comparisons with other harnesses, use the [research and comparison protocol](docs/harness-comparison.md). First compare Mulch's four modes to isolate the effect of repair, then compare against other harnesses under a shared model and environment. Track correctness, total cost, latency, and operational reliability separately. No single score establishes a universally better harness.

## Roadmap

- Run larger, repeated evaluations that actually cross ladder thresholds and report confidence intervals.
- Tune scoring deadlines and policy thresholds from those measured traces.
- Publish the first tagged release and npm packages; add independently signed release artifacts.
- Expand provider conformance coverage without coupling the harness to provider-owned environment variables.
- Evaluate race mode against independently graded task outcomes, including cases where health and correctness disagree.
