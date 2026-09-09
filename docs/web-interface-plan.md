# Mulch web interface — implementation handoff

Prepared 2026-09-08; implemented locally after approval. The four implementation phases below now have code and deterministic regression coverage. See [the shipped workflow and test commands](web-workflow.html). This document retains the original design rationale and acceptance targets. No paid evaluation, public release, or deployment was performed; prior pilot artifacts are preserved.

## Product decision and reference

Build a local coding workspace with a conversation at its center and an inspectable context-repair system alongside it. Developers should be able to complete real repository tasks in the browser; a reviewer should be able to inspect the evidence behind a repair without learning the event schema first. This is an Operate surface. The differentiator remains auditable context repair, with independently graded correctness as the eventual proof.

The user's reference is specifically **deepseek-ai/deepseek-harness**, not the consumer DeepSeek chat application. Its documented `dsh web` command starts a local web server and opens the browser, with `--no-open` available. Its user guide covers configuring a model, selecting a workspace, and continuing agent tasks. Borrow the launch and workspace workflow, while keeping Mulch's Go/SQLite runtime. Sources: [DeepSeek Harness README](https://github.com/deepseek-ai/deepseek-harness), [Web UI guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md).

DeepSeek's architecture also uses durable session events as the basis for context and UI projections. Therefore neither an event log nor a browser interface is uniquely Mulch's. Mulch's specific repair decisions, isolated candidate comparisons, and inspectable correctness evidence are the focus. Its plugin architecture is a different scope from this interface project. [DeepSeek architecture](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md)

Recommended direction, pending any correction: conversation first, repair inspector second. Keep Mulch's existing warm neutral/olive identity initially. The reference establishes a workflow, not a request to copy branding. An optional question about coding-workspace versus analysis-dashboard priority was sent during planning; absence of a reply is not confirmation of a new visual identity. Do not restart questions about the already established correctness focus, local repository use, or CLI support.

## Proposed launch experience

```sh
cd /path/to/project
mulch web
# Proposed flags:
mulch web --no-open
mulch web --session SESSION_ID
mulch web --db /path/to/events.db
```

`mulch` keeps opening the terminal. `mulch web` starts the same embedded Go server, defaults to loopback, and selects the launch directory as the proposed workspace. Show that resolved path before the first task. Bind one listener, determine the actual bound port, then open the browser; avoid the current probe-close-rebind pattern. If automatic opening fails or the launch is remote, print the usable URL and keep serving. Preserve `mulch serve` as the explicit server command and `--no-open` for tests.

The native binary already embeds the frontend: no separate Node server or cloud account should be required at runtime. Model credentials stay on the backend. First version: show configuration status and clear environment/.env setup instructions, without sending secrets to the browser. In-browser credential storage and OAuth are later work.

## Existing implementation and verified gaps

| Area | Current evidence | Planned change |
|---|---|---|
| Frontend | React/Vite/TypeScript, CSS modules, embedded assets; most UI is in `ui/src/main.tsx` | Extend the existing stack; split state, transcript, composer, and inspector |
| Sessions | Start, steer, cancel, branch, list/tree, event history and WebSocket routes | Add completed-session follow-up/resume, session metadata/capabilities, and explicit ownership |
| Conversation | Main view is a grid of raw event types grouped by turn | Derive readable messages and tools; retain raw events behind inspection |
| Workspace | Start API accepts `opts.workdir`, but current UI submits empty opts | Show and validate selected workspace; never silently act in an unexpected directory |
| Follow-up | Manager/runtime can resume; HTTP Control exposes no ordinary resume method | Add resume request and persistent composer behavior |
| Streaming | Server closes at session end; frontend stops reconnecting after end | Restart subscription for later runs of the same session; recover gaps without duplicates |
| CLI observation | Standalone terminal uses its own runtime/router; daemon manager does not own it | Tail committed SQLite events for externally running sessions; advertise read-only control for those runs |
| Health thresholds | UI uses 75/60/45/35/20, runtime defaults are 80/65/50/40/25 | Obtain policy from recorded configuration; do not hardcode chart labels |
| Score units | Dimension values are 0–1 but current UI meters use 0–100 | Normalize once in a tested presentation adapter; composite stays 0–100 |
| Scorer trust | Pilot repeatedly carried forward coherence=1; partial events lack precise failure causes | Show freshness/availability separately; improve failure recording before claiming a reliable detector |
| Repair evidence | Existing health strip links some intervention/context events | Add exact before/after context and candidate views with lineage |
| Tests | Go server/runtime tests and Node tests for `model.ts` exist | Add DOM and real browser integration coverage against a fake provider |

Code anchors: `internal/cli/cli.go`, `internal/server/server.go`, `internal/server/control.go`, `internal/session/manager.go`, `internal/runtime/runtime.go`, `internal/event/event.go`, `ui/src/main.tsx`, `ui/src/model.ts`, `ui/src/viewer.module.css`, `ui/src/diagnostics.module.css`.

## Workspace composition

```text
Workspace / model / repair mode                        Connection state
────────────────────────────────────────────────────────────────────────
Sessions              Conversation                    Inspector (toggle)
New conversation      User request                    Health + freshness
Recent conversations  Assistant response              Repair decisions
Branch children       Collapsible tool calls          Context before/after
                      Inline repair notice             Candidate comparison
                      Task result                     Raw evidence
                      ──────────────────────────────
                      Persistent composer / Send / Stop
```

The center must be useful with the inspector closed. Use readable Markdown, code fences with copy controls, and expandable tool input/output; raw JSON is an advanced view. Collapse large tool results and group stream deltas into their parent response without rendering the final message twice. Keep the draft when sending fails. Announce stop/failure and retain partial output.

The memorable interaction: selecting a repair notice opens the exact supporting scores/evidence, the recorded policy decision, and the affected context occurrences. Context changes and workspace file changes are separate concepts. A pruned tool result does not mean a deleted file.

At desktop widths, show sessions and conversation with an optional inspector. On a laptop the inspector overlays or replaces part of the workspace without squeezing the composer. On a narrow screen use session and inspector drawers with predictable back/focus behavior. Preserve existing identity; choose final spacing and component styling during implementation after reviewing the running incumbent. No image generation or visual rebrand is required to execute this plan.

## State and command behavior

The composer has explicit states: no workspace, missing provider/read-only, ready, submitting, running, stopping, disconnected, and failed submission. Browser disconnect must not cancel backend work. Stopping does not roll back completed file edits.

- Ready/new session: send creates a task in the selected workspace.
- Completed/cancelled/failed session: send resumes the same saved conversation, subject to backend eligibility.
- Running session: main action is Stop; a separately labelled “Steer next turn” action uses existing steering semantics. Do not call steering a durable follow-up queue.
- Running in another process: view events, but disable mutation with a clear ownership explanation. Do not start a competing runner.
- Read-only or missing credentials: browse/replay saved evidence; explain how to enable execution.

First-version command palette: new session, switch/resume session, rename, show status, inspect context, show repairs, show raw events, and stop owned work. Buttons remain discoverable without slash-command knowledge. Expose only backend-supported actions. Model/mode changes can initially apply to new sessions; persist effective configuration per run so historical views do not inherit today's settings.

## Backend and event contracts

Extend existing HTTP/WebSocket transport rather than replacing it. Proposed additions, to finalize with integration tests:

| Contract | Purpose |
|---|---|
| `GET /api/config` | Effective non-secret model/mode/policy, launch workspace, readiness, supported actions |
| `GET /api/sessions/{id}` | Saved metadata, current status and available controls, including runner ownership |
| `POST /api/sessions/{id}/resume` with `{text, request_id}` | Append follow-up and resume through the session manager |
| `PATCH /api/sessions/{id}` with `{label}` | Rename without altering transcript |
| Existing create request plus `request_id` | Deduplicate repeated submit/retry; return original acceptance for the same ID |
| Existing event stream with sequence cursor | Backfill then follow, detect gaps, deduplicate by session+sequence |
| Versioned run-configuration event | Preserve effective mode, policy, model/judge, weights, and supported scoring dimensions |

Use conflict responses for incompatible concurrent actions; UI disabled states alone cannot enforce ownership. Persist request acceptance/idempotency where needed to survive a lost HTTP response. Never automatically retry an ambiguous model-task submission without its idempotency identity.

Add scorer availability/freshness and actual failure categories at the backend rather than inventing them in TypeScript. Old traces without that metadata display “unknown”; carried-forward values display as reused. Record a specific run's effective configuration and causal event IDs. Do not try to match duplicate events by payload text.

The browser is a projection, not a second agent loop. All execution continues through `internal/runtime.Config`; session lifecycle remains in the manager/store. CLI-to-browser inspection can initially use the shared SQLite log without changing CLI execution ownership. A shared daemon for all CLI execution is a separate migration, not a prerequisite to viewing traces.

## Repair inspector and correctness evidence

1. **Health:** show score, scored turn, freshness and partial failures. Never equate green health with a correct answer. Policy labels come from the trace; legacy policy is unknown unless independently recorded.
2. **Decision:** show confirmation, cooldown, skip reasons, action and source evidence. “No repair triggered” is a legitimate result with a useful explanation.
3. **Context:** show retained/hidden/inserted/replaced occurrences by sequence, using recorded request visibility and context mutations. Reconstruct as-of state from the log; present-day visibility flags are insufficient for historical views.
4. **Race:** compare Prune and Reanchor candidate sessions, their mutations, recorded scores, selection and costs when available. Missing score is unavailable, not zero. Candidate workspace edits are not merged into the parent; display this plainly. If exact parent/child occurrence mapping is missing from old events, show the limitation instead of guessing.
5. **Outcome:** show completed/failed/cancelled separately from independently graded correctness. Normal sessions default to “not independently graded.” Link pilot/evaluation outcomes only through explicit run/session IDs and trusted local artifacts. Do not label the winning health score a proven correctness win.

The existing pilot is evidence of failures, not a successful repair demonstration. Use clearly labelled deterministic fixtures to exercise inspector states. A live efficacy demonstration depends on the separate scorer/routing work and a frozen, held-out evaluation.

## Implementation order after reset

### 1. Launch and lifecycle foundation

Implement `mulch web`, listener ownership, safe browser opening, launch workspace, readiness/capability metadata, resume and rename routes, idempotent submit, and backend ownership checks. Fix same-session reconnect behavior. Acceptance: start → complete → follow-up → cancel → follow-up → reload retains one coherent conversation. Browser closure leaves work running.

### 2. Conversation workspace

Extract `AppShell`, `SessionSidebar`, `Conversation`, `Message`, `ToolCall`, `Composer`, and `CommandPalette` components. Add typed API client, incremental session-event reducer, and a lifecycle-aware stream hook. Preserve drafts per session and keep auto-scroll off when the user is reading older content. Initial pass keeps model/repair settings fixed by launch where changing them would lack recorded configuration.

### 3. Trustworthy diagnostics

Add backend scoring freshness/failure metadata and run configuration, then correct score scaling and threshold sourcing. Implement `RepairInspector`, `ContextDiff`, and `RaceComparison`. Preserve legacy/unknown cases. Do not tune the repair policy as a hidden part of the UI change; track policy improvements separately.

### 4. CLI handoff and validation

Add a terminal command such as `/inspect` that opens the current session URL once launcher/ownership behavior is reliable. Validate read-only inspection of a standalone CLI session and mutating controls for daemon-owned sessions. Complete browser and regression checks, update documentation/HTML, rebuild the embedded assets and local binary. Local symlink `~/.local/bin/mulch` already points to this checkout's `dist/mulch`.

Do not create another frontend project or copy DeepSeek's framework. Targeted new files can live under `ui/src/components/`, `ui/src/hooks/`, `ui/src/api/`, and `ui/src/state/`. Introduce browser-test dependencies only when their tests are written. Keep generated `internal/server/ui_dist` in sync through the existing build.

## Validation and acceptance

All initial testing uses disposable repositories and deterministic providers. No extra paid campaign is implicitly approved by resuming UI implementation.

| Test level | Required cases |
|---|---|
| Go integration | Resume retains history; repeated request ID starts once; conflicting ownership rejected; cancel finishes; config contains no keys; read-only controls correct |
| Frontend reducer | Delta/final deduplication; out-of-order/gapped delivery; session switching; resumed stream; 0–1 dimensions normalized; reused/unknown scores preserved |
| Browser end-to-end | Launch/select workspace; send; streamed tools; follow-up; stop; reload; reconnect; new session; rename; narrow viewport; keyboard-only command/inspector navigation |
| Evidence correctness | Duplicate payload occurrences; prune/compact/reanchor before/after; critical escalation; race tie/failure; ungraded versus graded outcome |
| Failure handling | Missing credentials, bad workspace, failed send, lost response, provider timeout, invalid judge response, server restart, externally running CLI session |
| Load | 10,000-event replay and a sustained fixture stream; batch deltas, window long lists, cap displayed tool output; no unbounded duplicate accumulation |

Use safe Markdown rendering without raw HTML, same-origin mutation/WS checks, loopback defaults, and no API keys in browser payloads or stored UI preferences. These are concrete requirements for a local browser that can start file-changing tasks. Do not add cloud authentication or remote sharing to this scope.

Performance target to verify on a recorded local machine: a 10,000-event fixture stays interactive, with command/composer input responding within about 100ms during replay. Treat this as an acceptance target, not a measured claim. Accessibility checks include focus return after drawers, labelled controls, non-color status cues, usable contrast, reduced motion, and stream announcements that do not read every token aloud.

Run the existing Go race suite, vet/lint, frontend tests/build, plus new browser tests. Do one batched desktop/narrow visual inspection, fix findings together, and do one confirmation pass. Save screenshots and exact fixture/test commands. A later live smoke test should have a separately agreed model, payload and spending limit; it still cannot substitute for correctness evaluation.

## Deferred scope and resume instruction

Defer cloud hosting, public release, provider OAuth, plugin marketplace, embedded shell/IDE, arbitrary workspace file browsing APIs, full permission-policy management, automatic rollback, and a general benchmark dashboard. A bounded tracked-diff panel can follow the core inspector; show existing CLI `/diff` limitations if reused.

After reset, resume with: **“Build the plan in docs/web-interface-plan.md, starting with launch and session lifecycle, then the conversation workspace. Use fake-provider tests first.”** Recheck the dirty worktree and existing skills before edits; preserve all earlier packaging, CLI, benchmark and documentation work. Read this handoff instead of restarting reference research or expanding the plan into a clone of every DeepSeek feature.
