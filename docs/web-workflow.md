# Browser coding workspace

The web interface is embedded in Mulch's native binary. It operates the same Go runtime as the terminal; no Node server or cloud account is needed at runtime. This implementation is local work, not a public release or npm publication.

## Launch

Build the current checkout once:

```sh
make ui
go build -o dist/mulch ./cmd/mulch
```

Then, with the binary on PATH:

```sh
cd /path/to/project
mulch config set base-url https://opencode.ai/zen/go/v1
mulch config set model glm-5.3-flash
mulch config set api-key
mulch web
```

The browser opens the actual bound loopback URL. Confirm the displayed working directory, enter a task, and send. `--no-open` prints the URL without opening a tab; SSH launches also print the URL. `--session ID` selects a saved conversation, `--db PATH` selects its database, and `--workdir PATH` changes the initial workspace. `mulch serve --addr 127.0.0.1:4141` starts the same interface at an explicit address.

Provider credentials remain in the Go process. Missing credentials produce a read-only viewer with setup instructions. `.env` loading follows the existing CLI behavior. Model, judge, scoring, and repair settings come from the launch environment/flags. Use `--race`, `--no-intervene`, or `--policy PATH` as needed; these choices are recorded per execution. Browser model switching is not part of this version.

## Everyday use

| Control | Behavior |
|---|---|
| New conversation | Preserve the current session and open a fresh composer |
| Send message / Enter | Start a task, or continue an eligible saved session |
| Shift+Enter | Insert a newline |
| Stop task | Cancel owned work; completed file edits remain |
| Steer next turn | Supply guidance to the current run; this is not a durable follow-up queue |
| Rename | Label an inactive session without changing its messages |
| Commands / Ctrl+K / Cmd+K | New, switch/resume, rename, status, context, repairs, raw events, stop |
| Inspect | Toggle the evidence panel; smaller screens use a modal drawer |
| Tool card | Expand inputs/results; large output previews link to the complete raw event |
| Copy / Copy code | Copy an assistant response or fenced code block |

Drafts are saved locally per conversation/workspace. Failed sends keep the draft and request identity; retrying the unchanged message reuses the original acceptance. After an ambiguous failure, inspect the session list before changing the prompt or starting new work. Changing the prompt intentionally creates a new submission identity.

Streams backfill by sequence, deduplicate repeats, and reconnect across later tasks in the same session. Reading older messages pauses auto-scroll. Long conversations initially show the latest 100 message/tool rows, with earlier rows available on demand. Tool previews and assistant display size are bounded; full recorded payloads remain in Raw evidence.

Closing/reloading the browser does not cancel the daemon's task. Stop it in the UI or stop the server normally with Ctrl+C. Abruptly killing the owning process can leave a persisted execution lease and running status. This version deliberately does not steal or expire such ownership automatically. Inspect the trace, verify the previous process has stopped, and start a new conversation for further work; automated recovery of crashed sessions is deferred. A request interrupted between reservation and acceptance remains indeterminate and is never silently re-executed.

## Terminal handoff

Inside an existing terminal session:

```text
/web
```

This opens the same saved session in a read-only inspector. The terminal keeps ownership; the inspection server shuts down when the terminal exits. `/inspect --no-open` prints the URL. It is available during a running terminal task once its session has been recorded. A separately launched web server sharing the database can also observe terminal events; external active sessions have disabled stop/steer/resume controls.

## Reading repair evidence

- **Health** shows the recorded score, scored turn, 0–100 dimension presentation, and per-dimension freshness. `fresh`, `reused`, `unverified`, `not_applicable`, `unavailable`, and legacy `unknown` have different meanings. Failure categories include timeout, cancellation, invalid response, and other scorer errors.
- **Recorded policy** comes from that execution's `run.config`, not today's launch settings. Old traces without it say unknown. This UI work did not retune policy thresholds or fix the live pilot's scoring/routing problems.
- **Repairs** lists fired and skipped decisions. New repair events include exact supporting score and context-mutation sequence IDs, including when a newer asynchronous score arrives during a mutation. Old events without those IDs disclose the missing link.
- **Context** uses each `llm.request`'s recorded visible sequence IDs and explicit prune/compact/inject mutations. Duplicate text at different sequences remains separate evidence. Context pruning does not mean deletion of repository files.
- **Candidates** shows recorded scores, selection, child mutations and primary usage where available. Missing scores/costs stay unavailable. Candidate workspace edits are not merged into the parent. Exact parent-to-child occurrence mapping is not recorded and is explicitly identified as unavailable.
- **Outcome** separates task completion/cancellation/failure from correctness. Ordinary sessions are not independently graded. This version does not import untrusted benchmark files or attach grades by task-name similarity.

## Offline verification

Install development dependencies and the browser once:

```sh
cd ui
npm ci
npx playwright install chromium
cd ..
make web-test
go test -race ./...
go vet ./...
```

On Linux, Playwright may require `npx playwright install --with-deps chromium`. Shell-tool regression tests require the existing native containment dependencies. CI now runs browser acceptance after building the embedded frontend.

`make web-test` runs reducer tests, rebuilds assets, and starts `ui/testserver` at `127.0.0.1:4144`. Its provider is deterministic; its file writes and database live in a disposable temporary directory. No real provider key, model API call, or user repository is used. The fixture includes clearly labelled synthetic repair/candidate evidence and a 10,000-event replay. Do not treat these fixture scores as benchmark results.

Covered checks include:

- Create, stream a real tool write, follow up under the same ID, cancel, continue again, reload, retain drafts, rename, and keyboard commands.
- Lost accepted HTTP response, browser reload, retry with the same identity, and exactly one created task.
- Provider failure followed by a successful task, inert HTML/unsafe Markdown links, and browser closure while backend work continues.
- Exact duplicate context occurrences, recorded policy, reused scores, tied candidate selection, and ungraded outcomes.
- External ownership, missing provider configuration, invalid workspace, responsive drawers, focus return, and horizontal overflow.
- Forced WebSocket reconnect, event gap/backfill/deduplication, and 10,000-event replay with a local under-100ms reducer/input-response acceptance target.
- Go tests for cross-connection execution leases, durable request receipts across database reopen, external event tailing after session end, terminal handoff, freshness/failure categories, and asynchronous causal linkage.

Browser screenshots and failure traces are generated in `ui/test-results/`; they are local, ignored artifacts. The verified local environment is macOS arm64, Node 26.7, Go 1.25.1, and Playwright Chromium. The under-100ms check passed locally; it is not a cross-device performance guarantee or a sustained-throughput benchmark.

The initial full Go run hit an existing two-second process-cancellation timing limit while suites ran concurrently. That test passed in isolation, and the complete race suite passed with package concurrency limited to two. A slower host may still expose that timing sensitivity.

## Before wider use

Try the UI in a disposable clone with your configured model and inspect both edits and trace evidence. Live provider compatibility, long-running streams, network failures across daemon restarts, assistive-technology testing, and comparative correctness still deserve broader testing. Follow the [stress-testing protocol](stress-testing.html) before publication. This interface makes repair behavior inspectable; the [pilot results](correctness-pilot-results.html) still do not establish a correctness advantage.

## Workspace folders, deletion, and execution trace

Use **+** beside Workspaces to add an existing absolute directory. The server validates and remembers it in SQLite; chats are grouped by their actual working directory. **New chat here** selects that folder without moving files or changing an existing session's directory.

**Delete chat** opens a confirmation. Deletion removes the selected conversation, its descendant candidate sessions, and their events. Any running task or execution lease in the subtree blocks deletion. Project files are untouched. Request receipts and deleted-session IDs remain to prevent retries from silently re-executing deleted work. This is history deletion, not secure erasure of database backups. Terminal-attached read-only views cannot delete history or add workspaces.

**Inspect → Trace** is the default evidence view. Expand recorded turns to follow model requests, tool calls and results, scoring, context mutations, repairs, and candidate branches. Exact repair-to-score and repair-to-context links remain clickable. Asynchronous score arrival is grouped by the event's recorded turn, with the scored turn labeled separately. Stream deltas are summarized as counts; long traces reveal earlier turns and steps incrementally. No private model reasoning or unrecorded causal links are fabricated.

From a terminal conversation, `/web` (or `/web --no-open`) opens this trace mid-task. It aliases `/inspect`: execution stays in the terminal and the attached viewer closes when the terminal exits. For an independently running browser workspace, use `mulch web`.

## Saved provider settings

Run `mulch config` for setup commands. Use `mulch config set api-key` for a hidden prompt, `mulch config set base-url URL`, and `mulch config set model NAME`. `mulch config show` redacts keys. Settings are saved in `~/.config/mulch/config.json`, with `XDG_CONFIG_HOME` and `MULCH_CONFIG` overrides. Environment and `.env` values override the saved file. Restart the process to apply changes.
