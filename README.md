# mulch

Mulch is a small Go agent harness that treats an agent session as a durable event log, scores the health of its working context asynchronously, and intervenes before degraded context becomes a bad answer. It runs standalone or as a daemon with concurrent sessions and an embedded trace viewer.

## Install

Releases contain CGO-free archives for Linux, macOS, and Windows on amd64 and arm64 plus `checksums.txt`. Download an archive from the GitHub release, verify it against that checksum file, and place `mulch` (or `mulch.exe`) on your `PATH`.

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

Mulch uses provider-neutral configuration. OpenCode Go is the default endpoint/model pair, but any OpenAI-compatible chat endpoint can be selected:

```sh
export MULCH_PROVIDER_API_KEY=your-key
export MULCH_PROVIDER_BASE_URL=https://opencode.ai/zen/go/v1
export MULCH_MODEL=glm-5.3-flash
./mulch run "inspect this repository and explain its architecture"
```

Environment variables can also live in `.env`. Relevance scoring is enabled separately with `MULCH_EMBEDDING_API_KEY`; its base URL and model are configurable through `MULCH_EMBEDDING_BASE_URL` and `MULCH_EMBEDDING_MODEL`. This split allows the chat and embedding providers to differ. See [the provider spike](docs/provider-spike.md) for the adapter decision and live verification command.

Start the daemon and viewer with:

```sh
./mulch serve --addr 127.0.0.1:4141
```

Then open `http://127.0.0.1:4141`. With provider credentials present, the viewer can start, steer, branch, and cancel sessions; without them it remains a read-only trace viewer.

## The event-log invariant

The append-only SQLite event log is the source of truth. Prompts, model responses, tool starts/results, visibility changes, health scores, interventions, steering, and session lifecycle changes are recorded before downstream consumers observe them. Existing history is not rewritten: pruning changes visibility, compaction appends a replacement event, and branching creates a child session at a parent sequence. Replay, resume, the terminal UI, and the web viewer are projections of the same log.

Writes pass through one store-owned writer goroutine, and committed events are published to a channel-based bus. This preserves per-session sequence ordering while subscribers such as scoring, the intervention ladder, and live viewers operate away from the core loop.

## Pi influences

Mulch follows Pi's preference for a minimal loop, explicit tool execution, JSONL-friendly events, extensions around the loop, and sessions modeled as trees. Mulch's additions are infrastructure-oriented concurrency and context-health feedback: hooks score and mutate context without teaching the core agent loop about either policy.

## Context health and the intervention ladder

After a turn, independent scorers fan out for saturation, staleness, relevance, and coherence. Available scores are combined on a 0–100 scale using configurable weights; a failed scorer reuses its previous value where possible. Scoring is pipelined and records both latency and whether completion reached the critical path.

The default ladder requires two confirming low-health observations, uses five points of hysteresis and a two-turn cooldown, and chooses progressively stronger actions:

| Composite below | Action | Behavior |
|---:|---|---|
| 80 | warn | inject a compact warning |
| 65 | prune | hide low-value context, capped at 30% |
| 50 | compact | summarize selected history into a replacement event |
| 40 | reanchor | inject the task anchor again |
| 25 | escalate | cancel rather than continue unsafely |

Pass `--no-intervene` to retain health scoring without ladder actions, or `--policy path.json` to supply a validated policy. Health weights use the `MULCH_HEALTH_WEIGHT_*` variables.

Pass `--race` to make a confirmed Prune-or-deeper decision fork one Prune candidate and one Reanchor candidate. Each branch runs one recorded turn concurrently; the healthier context mutation is appended to the parent, the losing branch is retained as `abandoned`, and another race is suppressed for twice the normal intervention cooldown. Race mode is opt-in and cannot be combined with `--no-intervene`.

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

The current provider-backed evaluation is also deliberately small: five runs per mode, ten total. Both modes succeeded, no interventions fired, scoring was reported on the critical path in 50.0% of control samples and 66.7% of intervention samples, and measured tool parallelism gain was only 1.01x. Those numbers do not establish a general performance advantage. The complete table and reproduction command are in [measured evaluation results](docs/evaluation-results.md).

## Roadmap

- Run larger, repeated evaluations that actually cross ladder thresholds and report confidence intervals.
- Tune scoring deadlines and policy thresholds from those measured traces.
- Add signed release artifacts and a stable installer after the first tagged release.
- Expand provider conformance coverage without coupling the harness to provider-owned environment variables.
- Explore race mode: fork on an intervention and retain the healthier branch.
