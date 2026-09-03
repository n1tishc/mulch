# Build Plan: mulch — a minimal Go agent harness that prunes and composts stale context

> Name: **mulch** — it prunes stale context, composts it into summaries, and keeps the agent's working soil healthy. Module path: `github.com/<you>/mulch`. A single-binary Go agent harness in the spirit of [Pi](https://pi.dev) — minimal core, primitives over features, sessions as trees — with two twists: it **scores the quality of its own context window in real time, off the critical path, and intervenes before the agent drifts**, and it runs as **infrastructure** (daemon, concurrent sessions, parallel tools, parallel evals) rather than as a script.

This is the final draft for the implementing agent. Read the whole file before starting. Sections are ordered by build sequence. Every phase has a definition of done; do not start the next phase until the current one's checks pass. Wherever a design choice exists *because* of Go, it is marked **[Go]**. Wherever it is borrowed from Pi, it is marked **[Pi]**. Wherever it is our own twist, it is marked **[Twist]**.

---

## 0. Goals, non-goals, principles

### What we borrow from Pi, and what we twist

Pi's thesis: *"There are many agent harnesses, but this one is yours."* A minimal core with a tiny system prompt and four tools; everything else is a primitive you compose. Sessions are trees you can rewind and branch. Four run modes (interactive, print/JSON, RPC, SDK). A deliberate list of things it refuses to build in (MCP, sub-agents, permission popups, plan mode, to-dos, background bash).

| Pi idea | mulch version |
|---|---|
| Four tools: read, write, edit, bash **[Pi]** | Same four. No more this sprint. |
| Minimal system prompt + `AGENTS.md` discovery + `SYSTEM.md` override **[Pi]** | Same. `AGENTS.md` walked from `~/.mulch/`, parent dirs, cwd. Each loaded file is a *source* with a mtime, so staleness scoring sees it **[Twist]**. |
| Sessions as trees, one file, rewind/branch from any point **[Pi]** | One SQLite file; `sessions.parent_id/fork_seq`; `mulch tree` and `mulch branch <session> --at <seq>`. |
| Package split: `pi-ai` / `pi-agent-core` / `pi-coding-agent` **[Pi]** | `internal/provider` / `internal/agent` (+`hook`, `event`, `bus`) / `cmd/mulch`, plus a tiny public `mulch` SDK package. |
| Four modes: interactive, print/JSON, RPC, SDK **[Pi]** | `run` (stream), `run --json` (JSONL event stream), `serve` (HTTP/WS API = our RPC), Go SDK. **No interactive TUI this sprint.** |
| Extensions hook the loop (before-turn injection, message filtering, tool events, custom compaction) **[Pi]** | A three-method Go `Hook` interface. **The health scorer and the intervention ladder are themselves hooks** — the core loop has no knowledge of scoring **[Twist]**. |
| Auto-compaction near the context limit **[Pi]** | Compaction only ever fires as a *ladder action* chosen by sub-score, never on a fixed threshold **[Twist]**. |
| Steering: send a message mid-run, delivered after the current tool **[Pi]** | `POST /api/sessions/{id}/steer` and `mulch steer`, delivered after the current tool batch via the event bus. |
| "What we didn't build" list **[Pi]** | Adopted verbatim below. |

### Goals
1. A working agent loop that completes small coding/file tasks with four tools.
2. An **append-only event log** (SQLite) as the single source of truth. Invariant: *anything the model has ever seen must be reconstructable from the log.*
3. Deterministic **replay**, **resume**, and **branch** on the log.
4. A **Context Health Score** (0–100) from four pluggable sub-scores: saturation, staleness, relevance, coherence.
5. **[Go]** Scoring **pipelined and fanned out**: turn N is scored while the model generates turn N+1; the four scorers run in parallel under independent deadlines; scoring adds ~zero wall-clock latency.
6. An **intervention ladder** with hysteresis and cooldown whose decision lands *before* tool execution of the next turn.
7. **[Go]** **Parallel tool execution** with per-call timeouts and clean cancellation of in-flight tools and the streaming LLM call.
8. **[Go]** A channel-based **event bus**; scorers, ladder, eval injector, steering, and the viewer are all subscribers.
9. **[Go]** **Daemon mode** (`mulch serve`) managing many concurrent sessions with an HTTP/WS API and embedded viewer.
10. **[Go]** **Parallel eval runner** with a with/without-intervention comparison.
11. **[Go]** **Single static binary**, cross-compiled via goreleaser.
12. An embedded **trace viewer** (React) showing a per-turn timeline with the health score overlaid and interventions marked, live for running sessions.
13. **[Pi]** A **Go SDK** (`mulch` package) so the harness can be embedded, and a `--json` event-stream mode for process integration.

### Stretch (days 13–14 only)
14. **[Go] Race mode**: fork on a ladder fire, run two candidate interventions concurrently for one turn, keep the healthier branch. See §8.5. **Do not start before day 13.**

### What we didn't build (this sprint) **[Pi]**
- **No MCP.** Tools are Go; the `Tool` interface is the extension point. (Roadmap: GoAI's MCP support makes this cheap later.)
- **No sub-agents.** Branching + the daemon give you the primitive; compose it yourself.
- **No permission popups.** Run in a container. A `BeforeTools` hook can implement gates.
- **No plan mode, no to-dos.** Write files.
- **No background bash.** Use tmux.
- **No interactive TUI.** `run` streams; `serve` + viewer is the interactive surface.
- No local NLI/ONNX; coherence uses an LLM judge. No vision features (only an `image_ref` column). No auth/TLS/multi-user.

### Principles
- Boring, obvious Go. Standard library first. Few dependencies; the core must stay skimmable in an hour.
- Every side effect that touches the model's context is an event. No hidden state.
- The harness orchestrates models; it does not run them. ML-flavored work sits behind provider interfaces.
- **[Pi]** Primitives, not features. If something can be a hook or a tool, it is not core.
- **[Go]** Everything long-running takes a `context.Context` and honors cancellation. No goroutine without an owner, a stop, and a test that stops it.
- **[Go]** Concurrency only where it removes latency or enables scale. Never for decoration.
- **[Pi]** Token-efficient: the default system prompt fits on one screen.

---

## 1. Tech stack and dependencies

Verified against current releases on 2026-09-03. **On day 1, run `go get -u ./... && go mod tidy`, record the resolved versions in `go.mod`, and pin them. Do not treat the versions below as final.**

| Concern | Choice | Notes |
|---|---|---|
| Language | **Go 1.25+ (`go 1.25.0` in go.mod), develop on 1.26** | Go 1.24 is EOL (Feb 2026). `testing/synctest` is stable in 1.25 — use it for timeout/cancellation tests. |
| LLM + embeddings | **`github.com/zendev-sh/goai` (conditional default, pinned)** behind our own `provider.LLM` / `provider.Embedder` interfaces | Vercel-AI-SDK-style, 21+ providers, streaming, tool calls, `Embed`/`EmbedMany`, MCP, ~2 deps. Pre-1.0 and fast-moving: **pin the exact version, never chase releases mid-sprint.** Gate on the day-1 spike (§4.1). **Never use its auto tool loop (`MaxSteps`); the harness owns the loop.** |
| LLM fallback | `github.com/anthropics/anthropic-sdk-go` v1.62+ | Wire-faithful, v1-stable. Used if the spike fails. Same `provider.LLM` interface, zero design change. |
| Embedding model | Voyage `voyage-4-lite` by default; OpenAI `text-embedding-3-small` fallback; Ollama for fully local | Via GoAI's embed API if the spike confirms it; else a ~60-line HTTP client. |
| Storage | `modernc.org/sqlite` v1.48+ | Pure Go, no cgo, SQLite 3.51.x, WAL. Single writer goroutine (§3.5). Ships a CGO-free `sqlite-vec`; optional, not needed this sprint. |
| Concurrency | `golang.org/x/sync` (`errgroup`, `semaphore`) | |
| CLI | `github.com/spf13/cobra` | Thin commands. |
| Config | `.env` + flags | `github.com/joho/godotenv`. |
| Logging | `log/slog` | JSON in non-TTY, text in TTY. |
| HTTP/WS | `net/http` + `github.com/coder/websocket` | Maintained successor of nhooyr/websocket. Not gorilla (archived), not x/net/websocket (deprecated). |
| Frontend | Vite 8 + `@vitejs/plugin-react` 6 + React 19 + TS, Node 22+ | Rolldown-based; no Babel config. Embedded via `//go:embed`. |
| Release | goreleaser v2 | |
| Lint | golangci-lint v2 (`version: "2"` config) | v1 is unmaintained. |
| Tests | `testing`, table-driven, **always `-race`**, `testing/synctest` for time | |
| CI | GitHub Actions | lint, `go test -race ./...`, ui build, `go build`, goreleaser dry-run. |

Token counting: **no tokenizer dependency.** Use the provider's per-call `usage.input_tokens`; that is the exact context size the model saw and is what saturation scoring uses.

Deprecation watch: Go ≤1.24 as a floor; `nhooyr.io/websocket`; `gorilla/websocket`; golangci-lint v1 config; Babel options in plugin-react; `voyage-3`/`voyage-2`; Node 20; GoAI's `MaxSteps`/auto-loop in *our* code.

---

## 2. Repository layout

```
mulch/
├── cmd/mulch/main.go            # CLI entry; wires subcommands, nothing else
├── mulch.go                     # [Pi] public SDK: Open(), Run(), Serve(); thin over internal/session
├── internal/
│   ├── event/                    # append-only log (source of truth)
│   │   ├── event.go              # Event, Type, payload structs
│   │   ├── store.go              # Store interface + SQLite impl, single writer goroutine
│   │   ├── schema.sql
│   │   └── replay.go             # BuildMessages: the ONLY place model-visible context is assembled
│   ├── bus/bus.go                # [Go] channel pub/sub, bounded, drop-not-block
│   ├── provider/
│   │   ├── llm.go                # LLM interface (streaming, single-step)
│   │   ├── embed.go              # Embedder interface + sha256 cache
│   │   ├── goai.go               # GoAI-backed LLM + Embedder (default)
│   │   ├── anthropic.go          # fallback LLM (only if spike fails; otherwise omit)
│   │   └── recording.go          # RecordingLLM for deterministic replay
│   ├── tool/
│   │   ├── tool.go               # Tool interface + registry
│   │   ├── exec.go               # [Go] parallel executor, per-call timeouts, cancellation
│   │   ├── read.go  write.go  edit.go  bash.go   # [Pi] the four tools
│   ├── hook/hook.go              # [Pi][Twist] Hook interfaces: BeforeTurn, BeforeTools, OnEvent
│   ├── agent/
│   │   ├── loop.go               # [Go] pipelined loop; calls hooks; knows nothing about scoring
│   │   ├── prompt.go             # [Pi] minimal system prompt, AGENTS.md discovery, SYSTEM.md override
│   │   ├── context.go            # wraps event.BuildMessages
│   │   ├── compaction.go         # summarize-and-replace (an action, not a policy)
│   │   └── steer.go              # [Pi] mid-run user messages via the bus
│   ├── score/                    # [Twist] Context Health Score — implemented as an OnEvent hook
│   │   ├── scorer.go             # Scorer interface, Composite, fan-out runner, per-session runner
│   │   ├── saturation.go  staleness.go  relevance.go  coherence.go  window.go
│   ├── intervene/                # [Twist] ladder — implemented as a BeforeTools hook
│   │   ├── ladder.go  actions.go  policy.go
│   │   └── race.go               # STRETCH — do not create before day 13
│   ├── session/manager.go        # [Go] concurrent sessions, errgroup per session, limits
│   ├── eval/
│   │   ├── scenario.go  poison.go
│   │   └── runner.go             # [Go] parallel runner
│   └── server/
│       ├── server.go  api.go  ws.go
│       └── ui_dist/              # built viewer, embedded (gitignored, produced by `make ui`)
├── ui/                           # Vite 8 + React 19 + TS
├── testdata/{scenarios,fixtures}/
├── .github/workflows/ci.yml
├── .goreleaser.yaml
├── .golangci.yml                 # version: "2"
├── Makefile                      # build, test, lint, ui, run, release-dry
└── README.md
```

Dependency direction: `cmd` → `mulch.go` → `session` → `agent` → {`event`, `bus`, `provider`, `tool`, `hook`}. `score` and `intervene` depend on `hook`/`event`/`provider`, never on `agent`. `agent` never imports `score` or `intervene`. Enforce with an `internal/arch_test.go` that fails on forbidden imports.

---

## 3. Core data model: the event log

### 3.1 Schema (`internal/event/schema.sql`)

```sql
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS sessions (
  id             TEXT PRIMARY KEY,           -- ulid
  parent_id      TEXT,                       -- [Pi] branch parent; NULL for root
  fork_seq       INTEGER,                    -- seq in parent this session branched from
  label          TEXT,                       -- [Pi] optional bookmark/label
  task           TEXT NOT NULL,
  model          TEXT NOT NULL,
  context_window INTEGER NOT NULL,
  workdir        TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  ended_at       TEXT,
  status         TEXT NOT NULL DEFAULT 'running'
    -- running | completed | failed | escalated | abandoned | cancelled
);

CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id  TEXT NOT NULL REFERENCES sessions(id),
  seq         INTEGER NOT NULL,              -- monotonic per session from 1
  turn        INTEGER NOT NULL,              -- 0 = setup
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL,                 -- JSON
  tokens      INTEGER,
  visible     INTEGER NOT NULL DEFAULT 1,    -- the only mutable column
  image_ref   TEXT,                          -- reserved for vision roadmap
  created_at  TEXT NOT NULL,
  UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_events_session_seq  ON events(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_events_session_type ON events(session_id, type);

CREATE TABLE IF NOT EXISTS embeddings (
  hash   TEXT PRIMARY KEY,                   -- sha256(model + text)
  model  TEXT NOT NULL,
  vector BLOB NOT NULL
);
```

Nothing is ever deleted. Pruning/compaction flip `visible` and append an event saying what was hidden and why.

### 3.2 Event types

| Type | When | Payload |
|---|---|---|
| `session.start` | Run begins | `{task, model, workdir, parent_id?, fork_seq?}` |
| `system.prompt` | Prompt assembled | `{text, sources:[{path, mtime}]}` — AGENTS.md/SYSTEM.md files listed as sources **[Twist]** |
| `user.message` | Initial task, steer, or injected user turn | `{text, origin: task\|steer\|eval}` |
| `assistant.message` | Model text | `{text, stop_reason}` |
| `assistant.tool_call` | Model requested a tool | `{call_id, name, input}` |
| `tool.start` | Tool launched | `{call_id, name}` |
| `tool.result` | Tool finished | `{call_id, name, output, is_error, duration_ms, source_ts, cancelled}` |
| `llm.request` | Before provider call | `{message_count, visible_event_seqs[]}` |
| `llm.response` | Stream ended | `{input_tokens, output_tokens, latency_ms, model, cancelled}` |
| `context.inject` | Harness added model-visible text | `{reason, text, by: hook name}` |
| `context.prune` | Events hidden | `{removed_seqs[], reason, tokens_freed, by}` |
| `context.compact` | Span replaced by summary | `{replaced_seqs[], summary, tokens_before, tokens_after, by}` |
| `score.health` | Composite ready | `{turn_scored, composite, saturation, staleness, relevance, coherence, details, latency_ms, on_critical_path}` |
| `score.partial` | Scorer missed deadline | `{name, timeout_ms, used_previous}` |
| `intervene.fire` | Ladder acted | `{level, action, score_before, reason, applied_before_turn}` |
| `intervene.skip` | Ladder blocked | `{level, reason}` |
| `race.start` / `race.end` | Stretch | `{branches[], winner?}` |
| `session.end` | Run ends | `{status, turns, total_input_tokens, total_output_tokens, wall_ms}` |
| `eval.inject` | Scenario poisoned context | `{kind, text, target_seq}` |

Because scoring is pipelined, a `score.health` for turn N normally lands during turn N+1; `turn_scored` says which turn it describes. `on_critical_path` is true only if the loop had to wait (should be rare; the eval reports the rate).

### 3.3 Store interface

```go
type Store interface {
    CreateSession(ctx context.Context, s Session) error
    EndSession(ctx context.Context, id, status string) error
    Append(ctx context.Context, e Event) (Event, error)  // assigns Seq; publishes on bus after commit
    List(ctx context.Context, sessionID string, from int64) ([]Event, error)
    Visible(ctx context.Context, sessionID string) ([]Event, error)
    SetVisible(ctx context.Context, sessionID string, seqs []int64, visible bool) error
    Sessions(ctx context.Context) ([]Session, error)
    Tree(ctx context.Context, rootID string) (Node, error)             // [Pi] session tree
    Branch(ctx context.Context, parentID string, atSeq int64) (Session, error) // copies visible ≤ atSeq
}
```

### 3.4 Reconstruction (`replay.go`)
`BuildMessages(events []Event) []provider.Message` is the **only** producer of model-visible context. Rules: `system.prompt`→system; `user.message`/`context.inject`/`context.compact`→user; `assistant.message`/`assistant.tool_call`→assistant (tool calls grouped by turn); `tool.result`→user tool_result blocks **in the order of the turn's tool calls, never completion order** (parallel execution must not change message shape). Everything else ignored.

Fixture test: for every `llm.request`, `BuildMessages(Visible)` at that seq equals what was sent.

### 3.5 Writer discipline **[Go]**
One writer goroutine fed by a channel of requests with reply channels. `Append` from any goroutine is safe and ordered per session. Readers use separate connections in WAL mode. Don't route writes through `database/sql` pooling.

---

## 4. Providers

### 4.1 Day-1 spike: is GoAI usable as the backend? (time-box: 30 minutes)

Write `internal/provider/spike_test.go` (build tag `spike`, needs a key) that asserts, using `zendev-sh/goai`:
1. **Single-step call, no auto loop.** One request → the raw assistant content blocks, including `tool_use` blocks with their ids, with *no* tools executed by the library. `MaxSteps`/auto-execution must be off or bypassable.
2. **Usage.** Per-call `input_tokens` and `output_tokens` are returned exactly as the provider reports them.
3. **Cancellation.** Cancelling `ctx` mid-stream returns within 500 ms with `ctx.Err()`.
4. **Fidelity.** Content blocks round-trip: what we log from the response can be sent back as the next request's assistant message and the provider accepts it (tool_use ids intact).

All four pass → GoAI is the backend for LLM *and* embeddings (`goai.go`); delete `anthropic.go`. Any fail → implement `anthropic.go` with the official SDK and a 60-line Voyage/OpenAI embed client; leave GoAI out. Either way, record the decision under `## Deviations` with the failing check, if any.

### 4.2 Interfaces (what the rest of the code depends on)

```go
type LLM interface {
    // One model turn. Sends deltas on out until done or ctx cancelled; returns the final Response.
    // Never executes tools. Cancelling ctx aborts the stream promptly.
    Stream(ctx context.Context, req Request, out chan<- Delta) (Response, error)
}
type Request struct { System string; Messages []Message; Tools []ToolSpec; MaxTokens int; Model string }
type Response struct { Blocks []Block; StopReason string; InputTokens, OutputTokens int; Model string }

type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)   // batches ≤64, cached by sha256
    Dim() int
}
```

`RecordingLLM` (replay) replays recorded `llm.response` + assistant events as deltas with no network.

---

## 5. Tools **[Pi]**

Four tools, same names as Pi so AGENTS.md conventions transfer:

- `read` — path relative to workdir, refuses `..` escapes, optional `offset`/`limit` lines. `SourceTS` = mtime.
- `write` — creates parent dirs. Refuses escapes.
- `edit` — exact-match string replacement (`old` must occur exactly once), returns a unified diff snippet. This is Pi's most-used tool; don't skip it.
- `bash` — `exec.CommandContext` in its own process group so cancel kills children; 60 s default timeout; output truncated to 16 KiB with a marker; blocklist of obvious destructive patterns returns `IsError`.

```go
type Tool interface {
    Name() string; Description() string; InputSchema() map[string]any
    Run(ctx context.Context, input json.RawMessage) (Result, error)  // must return promptly on cancel
}
type Result struct { Output string; IsError bool; SourceTS time.Time }
```

**Parallel executor (`exec.go`) [Go]:** `RunAll(ctx, calls, emit) []Outcome` runs calls concurrently under a semaphore (`MaxPar` default 8), each with its own timeout; returns outcomes **in input order**; emits `tool.start`/`tool.result` per call; on parent cancel, in-flight calls are cancelled and their results carry `cancelled: true`.

---

## 6. Hooks **[Pi][Twist]** (`internal/hook`)

This is the extension surface, and it is the *only* way scoring and intervention touch the loop.

```go
type Turn struct {
    SessionID string; Turn int
    Visible   []event.Event          // what BuildMessages will see; hooks may mark seqs to hide
    Inject    []string               // texts to append as context.inject before the LLM call
    ToolCalls []provider.ToolUse     // set after the LLM call, before tools run
    Cancel    func(reason string)    // end the session (escalate)
}

type Hook interface{ Name() string }
type BeforeTurn interface{ Hook; BeforeTurn(ctx context.Context, t *Turn) error }   // context engineering
type BeforeTools interface{ Hook; BeforeTools(ctx context.Context, t *Turn) error }  // the ladder lives here
type OnEvent   interface{ Hook; OnEvent(ctx context.Context, e event.Event) }        // bus subscriber
```

- Hooks are registered in `cmd/mulch` (compile-time wiring, like Caddy plugins). Order is registration order.
- `BeforeTurn` runs after `BuildMessages` input is gathered and before `llm.request`. Used by: prompt/AGENTS.md loader, steering delivery, eval injector.
- `BeforeTools` runs after the assistant's tool calls are logged and before the executor starts. Used by: the intervention ladder (and, in a future extension, permission gates).
- `OnEvent` receives every committed event via the bus. Used by: the scoring runner, the WS fan-out, `--json` output.
- Built-in hooks: `prompt`, `steer`, `score`, `ladder`, `eval` (only when `mulch eval`). A user could delete `score` and `ladder` from the wiring and have a plain Pi-like harness. **That is the point.**

---

## 7. The agent loop **[Go]** (`internal/agent/loop.go`)

Pipelined: scoring of turn N overlaps generation of N+1; `BeforeTools` (ladder) runs before tool execution of N+1.

```
Run(ctx, deps, session, task):
  append session.start; prompt hook assembles system.prompt (with sources); append user.message{origin:task}
  for turn := 1..maxTurns:
    t := &Turn{Visible: store.Visible(session), ...}
    for h in beforeTurnHooks: h.BeforeTurn(ctx, t)      // may Inject / hide seqs (each becomes an event)
    msgs := BuildMessages(t.Visible)
    append llm.request
    turnCtx, cancelTurn := context.WithCancel(ctx)
    resp := llm.Stream(turnCtx, ..., deltas)              // deltas → terminal/--json; scoring of turn-1 runs meanwhile
    append llm.response; append assistant.* blocks
    t.ToolCalls = resp.ToolUses
    for h in beforeToolsHooks: h.BeforeTools(ctx, t)     // ladder consults freshest score.health; may prune/compact/inject/Cancel
    if session cancelled by a hook: cancelTurn(); append session.end escalated; return
    if len(t.ToolCalls) == 0: append session.end completed; return   // score hook gets a final Request(); wait ≤5s
    executor.RunAll(turnCtx, t.ToolCalls, append)        // parallel, results in call order
    cancelTurn()
    // nothing here waits on scoring: the score hook reacts to tool.result events on the bus
  append session.end failed
```

Rules:
- The loop never blocks on scoring except ≤5 s at session end. If it ever would, that `score.health` gets `on_critical_path: true`.
- Prune/compact applied in `BeforeTools` affect the *next* `BuildMessages`; the current turn's tool calls proceed.
- SIGINT → cancel → stream and tools abort → `session.end cancelled` within 2 s.
- Terminal output: streamed text; one compact line per tool call; one health line per score with the turn it describes: `[t3] health 72 | sat .41 stale .12 rel .88 coh .95 (async 640ms)`. `--json`: every event as one JSON line on stdout, nothing else **[Pi]**.

`prompt.go` **[Pi]**: default system prompt ≤ 40 lines (identity, the four tools, workdir, "be terse"); append `AGENTS.md` found in `~/.mulch/agent/`, each parent of cwd, and cwd (nearest last); `SYSTEM.md` in cwd replaces the default. Every file included is recorded in `system.prompt.sources` with its mtime.

`steer.go` **[Pi]**: `Manager.Steer(id, text)` publishes on the bus; the `steer` BeforeTurn hook drains pending steers into `user.message{origin:steer}` at the start of the next turn. Steering never interrupts a running tool batch (simpler than Pi's; note it).

`compaction.go`: `Compact(ctx, span)` summarizes with the Haiku-class model preserving paths, decisions, open questions; hides the span; appends `context.compact`. Called only by ladder actions.

---

## 8. Context Health Score and the ladder **[Twist]**

### 8.1 Scorer interface and runner (`internal/score`)

```go
type Scorer interface {
    Name() string
    Score(ctx context.Context, in Input) (Result, error)
    Deadline() time.Duration        // saturation 5ms, staleness 5ms, relevance 3s, coherence 8s
}
type Input struct { Session Session; Events, Visible []event.Event; LastResponse provider.Response; TaskEmbed []float32 }
type Result struct { Score float64; Details map[string]any }   // 0..1
```

**Fan-out [Go]:** `Composite.Score` runs all scorers in an `errgroup`, each under `context.WithTimeout(s.Deadline())`. Timeout/error → previous value + `score.partial`. Latency = max, not sum.

**Runner:** one goroutine per session; it is an `OnEvent` hook that triggers on the last `tool.result` of a turn (or `assistant.message` with no tools). Requests coalesce (queue depth 1). Publishes `score.health`.

### 8.2 Sub-scores
- **Saturation** — `ratio = input_tokens / context_window`; 1.0 up to 0.5, linear to 0.0 at 0.95.
- **Staleness** — over visible `tool.result`, `context.inject`, and `system.prompt.sources` with a timestamp: `f = exp(-age/halfLife)` (30 m default), token-weighted mean; 1.0 if no sources.
- **Relevance** — chunks = visible `tool.result`/`assistant.message`/`context.inject`/`context.compact` (≤2k chars); `sim = cos(embed(chunk), taskEmbed)`, decay `exp(-turnsAgo/8)`, weighted mean rescaled from [0.2,0.9]→[0,1]. Details: lowest 5 `{seq, sim}`. Only new chunks are embedded.
- **Coherence** — every 3 turns or on ladder request; ≤6 chunk pairs biased to shared paths/entities; one batched Haiku prompt → strict JSON `consistent|contradictory|unrelated`; `1 - contradictory/(contradictory+consistent)`. Details: contradictory pairs.
- **Composite** — `100·Σ w·s`, weights `.30 .20 .30 .20` (config). Keep last three for hysteresis.

### 8.3 Policy (`intervene/policy.go`)
```go
type Policy struct {
    WarnBelow, PruneBelow, CompactBelow, ReanchorBelow, EscalateBelow float64 // 80 65 50 40 25
    Hysteresis float64 // 5
    CooldownTurns, ConfirmTurns int // 2, 2
    MaxPruneShare float64 // 0.3
    Race RacePolicy // Enabled=false
}
```

### 8.4 Ladder (a `BeforeTools` hook)
- Levels Warn < Prune < Compact < Reanchor < Escalate; deepest level below threshold for `ConfirmTurns` consecutive scores; cooldown → `intervene.skip`, try shallower.
- Routing by dominant low sub-score: saturation→Compact; relevance→Prune (lowest-sim chunks); coherence→Reanchor citing contradictory pairs; staleness→Reanchor with "re-read these sources" (it can name `AGENTS.md` if that's what went stale).
- Actions: Warn (one-line inject), Prune (≤`MaxPruneShare`; never system prompt, original task, or last two turns), Compact (oldest half excl. last two turns), Reanchor (task verbatim + note), Escalate (`t.Cancel`).
- Timing invariant: `applied_before_turn == turn_scored + 1 or + 2`, never later. The eval asserts it.

### 8.5 Race mode — STRETCH **[Go]** (`race.go`)
On a Prune-or-deeper fire with `Race.Enabled`: `Branch` twice from the current seq; apply candidate A (Prune) to one and B (Reanchor) to the other; run **one turn** of each concurrently with recording; score both; winner's visible set becomes the parent's (mirrored as `context.*` events in the parent so the parent log stays the source of truth); loser → `abandoned`; append `race.start/end`. One race per `2×CooldownTurns`. If unclear or slipping, leave a documented stub. Nothing else may depend on it.

---

## 9. Session manager **[Go]** (`internal/session/manager.go`)

```go
type Manager struct{ ... }
func (m *Manager) Start(ctx, task string, opts RunOpts) (id string, err error)   // errgroup: loop + hooks
func (m *Manager) Steer(id, text string) error
func (m *Manager) Cancel(id string) error
func (m *Manager) List() []Status
func (m *Manager) Shutdown(ctx) error     // cancel all, bounded wait
```
`MaxSessions` default 32 via semaphore. `mulch run` and `mulch serve` share this path.

## 10. Public SDK **[Pi]** (`mulch.go`)
```go
h, _ := mulch.Open(mulch.Options{DB: "...", Model: "...", Hooks: mulch.DefaultHooks()})
id, _ := h.Run(ctx, "task", mulch.RunOpts{Workdir: "."})
events, _ := h.Subscribe(id)           // <-chan event.Event
_ = h.Serve(ctx, ":4141")
```
Kept deliberately tiny; it exists so the harness can be embedded and so `cmd/mulch` is a thin client of it.

---

## 11. CLI (`cmd/mulch`)

```
mulch run "<task>" [--workdir .] [--model ...] [--max-turns 40] [--db ~/.mulch/mulch.db] [--json] [--no-intervene] [--policy policy.json] [--tool-par 8]
mulch replay <id>                      # offline, deterministic
mulch resume <id>
mulch branch <id> --at <seq> ["<task override>"]   # [Pi] /tree: continue from any point
mulch tree <id>                        # [Pi] print the session tree with labels
mulch label <id> "<label>"
mulch sessions
mulch steer <id> "<text>"              # [Pi] against a running serve
mulch serve [--addr :4141] [--open] [--max-sessions 32]
mulch cancel <id>
mulch eval <scenario.yaml> [--runs 10] [--concurrency 5] [--no-intervene] [--race]
mulch version
```
`mulch run` submits to a running `serve` if one is detected (pidfile + health endpoint), else runs standalone.

---

## 12. Eval **[Go]** (`internal/eval`)

Scenario YAML with `task`, `workdir_fixture`, `injections` (`stale_tool_result` with `source_ts_offset`, `contradiction`, `distractor`), `success_check` commands, `max_turns`. The injector is a `BeforeTurn` hook active only under `mulch eval`.

Runner: fresh temp workdir per run; `--runs` with and without intervention under a `--concurrency` semaphore via the Manager. Report: success rate, mean turns, mean input tokens, mean min-health, interventions by level, **scoring on-critical-path rate**, **mean scoring latency**, **tool wall vs sum (parallelism gain)**, eval wall-clock. Writes `eval_results.json` + a markdown table. Any measurable difference is a result; if intervention hurts, record it and adjust `policy.go` with a comment.

---

## 13. Server and viewer

**API:** `GET /api/sessions`, `GET /api/sessions/{id}/events?from=`, `GET /api/sessions/{id}/tree`, `POST /api/sessions {task, opts}`, `POST /api/sessions/{id}/steer {text}`, `POST /api/sessions/{id}/branch {at}`, `DELETE /api/sessions/{id}`, `GET /api/metrics`. WS: `/ws/sessions/{id}` (one session), `/ws/sessions` (`session.*` + `score.health` for all).

**Viewer** (Vite 8 + React 19 + TS, no UI framework, plain CSS modules, ≤12 components):
1. Session list (live first with pulsing dot, turn, health; branches nested under parents).
2. Health strip: hand-drawn SVG sparkline of composite per `turn_scored`; hover shows sub-scores + scoring latency; intervention markers by level; faint threshold lines.
3. Timeline: one column per turn; cards per event; parallel tool calls side by side with duration bars; pruned/compacted events dimmed with a tooltip naming the hiding event; steer messages visibly tagged.
4. Detail pane: pretty JSON; for `score.health` bars per sub-score + lowest-relevance chunks + contradictory pairs; for `intervene.fire` the reason and a link to the resulting `context.*` event; race inset (stretch).
5. Live mode via WS; a "branch from here" button on any turn (calls `/branch`).

Design: one accent color, neutral grays, monospace payloads, generous whitespace, no charts library.

`make ui` builds and copies `ui/dist` → `internal/server/ui_dist`; `//go:embed all:ui_dist`.

## 14. Release **[Go]**
goreleaser v2: linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`, `-trimpath -ldflags "-s -w -X main.version=..."`, checksums. README install: one `curl | sh` or `go install`. Budgets: `mulch version` < 20 ms; `serve` ready < 200 ms. Record actuals in the README.

---

## 15. Sprint schedule (10 days + 4 buffer)

| Day | Deliverable | Definition of done |
|---|---|---|
| 1 | Module, layout, `arch_test`, `event` store (single writer), `bus`, **GoAI spike (§4.1)** | `go test -race` green on `event`/`bus`; 1k concurrent appends across 4 sessions keep seq monotonic; slow subscriber drops without blocking; spike decision recorded in Deviations |
| 2 | `provider` (per spike), **four tools** incl. `edit`, parallel executor, `hook` package, minimal loop, `prompt.go` (system prompt + AGENTS.md/SYSTEM.md), `mulch run` | Completes "create hello.go and run it"; 3 tool calls run concurrently (wall < sum with sleeping fakes); Ctrl-C mid-stream ends cleanly < 2 s; AGENTS.md content appears in `system.prompt.sources` |
| 3 | `RecordingLLM`, `replay`, `resume`, `branch`, `tree`, `label`, `sessions`, `--json` | Replay: zero network, identical assistant events; branch copies visible ≤ seq and `tree` shows it; `--json` emits one line per event and nothing else |
| 4 | `score`: interface, fan-out, saturation, staleness (incl. prompt sources), runner as `OnEvent` hook; terminal readout | Composite latency == slowest scorer (fakes 10/50/200 ms); timeout → `score.partial` + previous value |
| 5 | `Embedder` + cache, relevance; **pipelining verified** | `on_critical_path` false for all turns of a 10-turn run; relevance drops on an injected distractor |
| 6 | Coherence judge; ladder as `BeforeTools` hook with hysteresis/cooldown; Warn/Prune/Reanchor | Ladder unit tests; `applied_before_turn == turn_scored+1` in a scripted run |
| 7 | Compact, Escalate, `--no-intervene`, policy file, `session.Manager`, `steer`, `mulch.go` SDK | Ladder fully exercised on a synthetic session; Manager runs 3 sessions concurrently, `Shutdown` bounded; a steer lands as the next `user.message{origin:steer}` |
| 8 | Eval scenario, fixture, parallel runner | `mulch eval --runs 5 --concurrency 5` prints the table incl. scoring latency and parallelism gain |
| 9 | `serve`: API incl. steer/branch, WS, Manager wiring; viewer skeleton | Browse a recorded session; start one via `POST`, steer it, see it live |
| 10 | Health strip, intervention markers, dimmed pruned events, parallel-tool bars, branch button, live all-sessions list | A running eval shows multiple sessions updating live |
| 11–12 | README, CI (`-race`, golangci-lint v2), goreleaser, Makefile | CI green on fresh clone; 6 release targets build; startup numbers recorded |
| 13–14 | Race mode (stretch) **or** threshold tuning from eval, whichever is more interesting | Documented either way |

Cut order if slipping: race mode → live all-sessions WS → `steer` → coherence (stub 1.0) → Compact → `tree`/`label` (keep `branch`). **Never cut:** event-log invariant, replay, the four tools, hooks, parallel tools, fan-out + pipelined scoring, the eval, the README.

---

## 16. Testing requirements
- All tests under `go test -race ./...`. Time-dependent code (deadlines, cooldowns, half-life, per-call timeouts) via `testing/synctest`.
- `arch_test.go`: `agent` must not import `score`/`intervene`; `score`/`intervene` must not import `agent`.
- `event`: CRUD; concurrent append ordering; `BuildMessages` fixture equality incl. parallel results in call order; `Branch`/`Tree`.
- `bus`: fan-out, filters, bounded buffers, drop counter, cancel closes, publisher never blocks.
- `tool`: executor ordering, per-call timeout, parent cancel kills all (`sleep 30` must die), path escapes, `edit` exact-once, truncation, blocklist.
- `hook`/`agent`: loop against fake LLM + fake tools asserting the exact event sequence; hooks called in order; `BeforeTools` can prune/inject/cancel; pipelining (score for N appended after `llm.request` of N+1); cancellation; steer delivery.
- `score`: each scorer table-driven; fan-out latency == max; deadline → partial; weights.
- `intervene`: confirm turns, cooldown, hysteresis, routing, prune cap, protected events, timing invariant.
- `session`: limits, shutdown bound.
- No network in tests. Providers are interfaces; fakes with configurable latency.

## 17. README outline
1. GIF: the viewer during a parallel eval — several live sessions, health dipping, interventions firing, parallel tool bars.
2. Pitch: "a minimal harness in the spirit of Pi, that measures its own context and runs as infrastructure." The one-line invariant.
3. Quickstart: install, key, `mulch run`, `mulch serve`.
4. What's borrowed from Pi and what's different (the table from §0).
5. How the score works (sub-score table, ladder table, why decisions happen before tools).
6. **Why Go** — three measured claims with eval numbers: scoring off the critical path, parallel tools, N concurrent evals; binary size and startup time.
7. Eval results with sample-size caveat.
8. Architecture: loop → event log → bus → {hooks: score, ladder, steer, eval, viewer}.
9. Design decisions: append-only log; single-writer SQLite; hooks over features; GoAI vs official SDK (the spike outcome); LLM judge over NLI.
10. Roadmap: MCP via GoAI, race mode (if not done), interactive TUI, local NLI via ONNX, image-bearing trace nodes for vision agents.
11. References: Pi (pi.dev), context rot (Chroma 2025), lost-in-the-middle, UDCG (EACL 2026), AgentTether, DeepSeek Harness event-log design.

## 18. Conventions for the implementing agent
- Commit after each row in §15 with a message that says what now works.
- `go vet ./... && go test -race ./...` before every commit.
- No new dependency without a one-line justification in the commit message. Pin versions; no `@latest` anywhere. Node deps via `npm ci` with a committed lockfile.
- Every goroutine has an owner, a `ctx` it selects on, and a test that cancels it.
- The core loop must never learn about scoring or intervention. If you find yourself importing `score` from `agent`, you are building the wrong thing.
- Never use GoAI's auto tool loop or `MaxSteps`.
- Do not start §8.5 (race mode) before day 13.
- If a design choice here is wrong in practice, change it and add a note under `## Deviations` (what, why, when).
- Environment: `ANTHROPIC_API_KEY` (or the provider key GoAI needs), `MULCH_MODEL`, `MULCH_JUDGE_MODEL`, `VOYAGE_API_KEY` / `OPENAI_API_KEY`, `MULCH_DB`, `MULCH_ADDR`.
- Terminal output stays calm.

## Deviations
- **2026-09-03 — GoAI rejected; official OpenAI SDK selected.** GoAI `v0.10.0` passed the live content, tool-call identifier, usage, and replay checks against OpenCode Go, but cancelling a stream returned `nil` rather than `context.Canceled`. Per §4.1's fail-fast rule, Mulch uses `github.com/openai/openai-go/v3` pinned at `v3.47.0` through OpenCode Go's OpenAI-compatible endpoint. Configuration uses Mulch-owned `MULCH_PROVIDER_API_KEY`, `MULCH_PROVIDER_BASE_URL`, and `MULCH_MODEL` variables. OpenCode Go does not advertise embeddings, so Voyage remains the default through a standard-library HTTP adapter with no additional dependency.
