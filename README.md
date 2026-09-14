# Mulch

Mulch is a small Go coding harness with durable conversations and inspectable context-repair experiments. Run `mulch` for the terminal interface or `mulch web` for the local browser workspace; both use the same event-sourced agent runtime.

> Source-visible, with no reuse license granted. Public availability is not permission to redistribute or relicense this project's code. Third-party dependencies retain their own licenses.

## What it does

- Runs a coding loop with file and sandboxed shell tools.
- Streams readable Markdown and tool output in terminal and browser interfaces.
- Saves sessions in an append-only SQLite event log for resume, replay, and inspection.
- Switches configured providers, models, and repair modes inside a conversation.
- Scores context health asynchronously and records any bounded repair decision.
- Exposes an execution trace linking turns, tools, scores, repairs, and race candidates.

Mulch supports configured OpenAI Chat Completions-compatible endpoints. Context scoring and repair are experimental: the current evaluation does **not** demonstrate a correctness advantage.

## Install

Install the latest checksummed native binary on macOS or Linux:

```sh
curl -fsSL https://github.com/n1tishc/mulch/releases/latest/download/install.sh | sh
mulch version
```

The installer defaults to `$HOME/.local/bin`; set `MULCH_INSTALL_DIR` to override it. Linux tool execution also requires Bubblewrap and host sandbox support. Windows builds can manage sessions and open the viewer, but do not provide a shell-containment backend.

Or install the tagged source with Go 1.25+:

```sh
go install github.com/n1tishc/mulch/cmd/mulch@v0.1.0
```

Prepared npm packages are attached to the GitHub release but are not published to the npm registry.

## Quick start

Save an OpenAI-compatible endpoint and credential:

```sh
mulch config set base-url https://api.example.com/v1
mulch config set model your-model
mulch config set api-key       # hidden prompt
mulch config show              # secrets are redacted
```

Then launch Mulch from the project it may edit:

```sh
cd /path/to/project
mulch             # terminal workspace
mulch web         # browser workspace
mulch web --no-open
```

Use a disposable or version-controlled project and review all edits: Mulch can write files and execute commands. Provider calls may consume credits.

Inside either interface, `/provider`, `/model`, and `/mode` list or change the next turn's runtime without discarding conversation history. `/new` creates a clean session. The terminal also provides `/help`, `/sessions`, `/resume`, `/status`, `/api-key`, `/rename`, `/history`, `/diff`, and `/inspect`.

See the [terminal command reference](docs/terminal-workflow.md) and [browser workflow](docs/web-workflow.md) for complete interface behavior.

### Multiple providers

Named profiles let one conversation switch among several configured OpenAI-compatible endpoints:

```sh
mulch config set provider zen
mulch config set provider.zen.base-url https://opencode.ai/zen/go/v1
mulch config set provider.zen.models glm-5.3-flash,glm-5
mulch config set provider.zen.api-key
```

Settings live in `~/.config/mulch/config.json`; `XDG_CONFIG_HOME` or `MULCH_CONFIG` can override the path. API keys are local plaintext protected with owner-only Unix permissions, not encrypted. Use `mulch config path` to locate the file and `mulch config unset KEY` to remove a value.

## No-key demo

The synthetic fixture exercises the real runtime and browser UI without making provider calls:

```sh
make ui
go run ./ui/testserver
# Open http://127.0.0.1:4144
```

Its write tool uses a disposable workspace; do not select a real project directory. The [five-minute demo](docs/portfolio-demo.md) explains what to show.

## How it works

The append-only SQLite event log is the source of truth. Prompts, responses, tool activity, scores, repairs, steering, and lifecycle changes are recorded before downstream consumers observe them. Replay, resume, terminal rendering, and the browser viewer are projections of that log.

```text
CLI / web / Go API
        |
     agent loop ---- parallel tools
        |
  append-only SQLite event log
        |
      event bus
   /      |       |          \
score   repair  steering   live viewer
```

The core loop depends only on provider and hook interfaces. Scoring and repair remain outside it, and architecture tests enforce the import boundaries. Browser assets are compiled with Vite and embedded into the Go binary.

Mulch's differentiator is auditable context repair, not an unverified claim that repair improves every task. The default intervention ladder requires confirming low-health observations and records the supporting score and resulting context mutation. Optional race mode evaluates bounded repair candidates in isolated workspace copies; candidate edits are never merged into the parent workspace.

## Build and test

From a clone:

```sh
make ui
go build -o dist/mulch ./cmd/mulch
make verify
```

Additional offline gates:

```sh
make contract
make correctness-test
make web-test
make distribution-test
make release-dry
```

`make web-test` needs Playwright Chromium (`cd ui && npx playwright install chromium`). `make release-dry` needs GoReleaser v2. Cross-compilation checks that targets build; it is not native runtime coverage for every operating system.

Relevant public documentation:

- [Live session API](docs/live-api.md)
- [Correctness benchmark protocol](docs/correctness-benchmark.md)
- [Correctness pilot results](docs/correctness-pilot-results.md)
- [Earlier measured evaluation](docs/evaluation-results.md)
- [Distribution and release process](docs/releases.md)
- [Security policy](SECURITY.md)

## Evaluation status

The 16-run diagnostic correctness pilot found 3/16 fully correct outcomes. Plain mode completed 3/4 conditions, while scoring-only, intervention, and race modes completed 0/4. Twelve runs exhausted the conservative token budget, no repair fired, and scorer reliability was poor. These results are retained because they define the current limit honestly; see the [full pilot report](docs/correctness-pilot-results.md).

Future work is deliberately narrow: improve score reliability and repair reachability, repeat independently graded trials, broaden provider conformance coverage, and publish npm packages only after a separate registry-release decision.
