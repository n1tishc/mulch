# Mulch: a five-minute engineering demo

This project demonstrates a working coding harness and its observability architecture. It does not claim to outperform other agents. Start with the deterministic walkthrough; use a live provider only for the actual coding segment.

## Prerequisites

Build with Go 1.25 or newer (see `go.mod`) and Node 24.8.0, matching CI, for the frontend. The local verification used Go 1.27.1 and Node 22.23.2. Do not assume every older Node 22 release can run the TypeScript tests. A recent Go installation can download the required toolchain. Linux live shell tasks additionally need Bubblewrap. The offline fixture uses file tools, not Bash.

```sh
make ui
go build -trimpath -o dist/mulch ./cmd/mulch
./dist/mulch version
```

## No-key walkthrough

The existing browser acceptance fixture runs the real CLI/server, runtime, event store and embedded UI with an injected deterministic model. It does not contact a provider. Responses and seeded repair evidence are synthetic, not a live AI result or a benchmark.

```sh
go run ./ui/testserver
```

Open `http://127.0.0.1:4144`. Use a separate browser profile if you have old fixture drafts. Keep the process running; Ctrl+C stops it and removes its temporary database and workspace. Do not point its workspace picker at a real project: the fixture's write tool is real.

1. Send **Write the fixture result.** Observe the user/assistant distinction, streaming Markdown, expandable write-tool result, and completed task.
2. Send **Continue with a follow-up.** The session ID stays the same. Inspect the trace to see the two tasks and recorded requests.
3. Send **slow task to cancel**, then press **Stop**. The task becomes cancelled and the composer remains usable.
4. Send **provider failure**. This intentionally produces an error. Send a normal follow-up to demonstrate recovery.
5. Enter `/model`, then `/model fixture-alt`, then `/mode plain`. These are fixture model names, not real services. Start another task and inspect its recorded configuration.
6. Rename the conversation, refresh to show persistence, then use `/new`. The transcript clears; the saved conversation remains in the sidebar.
7. Open **Fixture · evidence**, then **Inspect → Repairs**. Show how a prune decision links to the exact score and affected event occurrence, and how candidate branches remain inspectable. Say explicitly that these are seeded synthetic records.

## Live coding segment

Configure a real compatible provider with a tool-capable model using `mulch config`. Enter keys with the hidden prompt, never in the prompt transcript or a recording. See [setup](../README.md#quickstart). Named profiles use the current flat string-map config format, not the typed format in the deferred roadmap.

Use a disposable copy of a small project with a known failing test. Start the terminal or browser, select `/mode plain` before sending a task, and ask:

> Fix the failing test with the smallest justified change. Run the relevant tests, explain the cause, and summarize the diff.

Then ask for an additional regression case in the same conversation. Independently inspect the actual diff and rerun tests in a separate terminal. Show `/provider`, `/model`, `/status` and `/new` in the terminal, or the corresponding commands and trace in the browser. Record the actual outcome, including failures; model narration is not a test result.

## What to explain in an interview

- **Durability:** append-only events are recorded before observers consume them. Resume and UI projections share one source of truth.
- **Separation of concerns:** the loop uses provider/tool/hook interfaces; scoring and intervention remain outside it. Import-boundary tests protect that architecture.
- **Concurrency:** tool execution, cancellation, task snapshots and replay have regression coverage, including race tests and process-group stress.
- **Evidence over claims:** a health score is not correctness. The pilot exposed limits; hidden graders and retained traces make those limits diagnosable.

For evidence run `make contract`, `make web-test` and the full [release gates](release-readiness.md#verification-gates). The seeded demo is for explaining mechanics; the offline tests and independently checked live task serve different purposes.

## Troubleshooting

| Symptom | Next action |
|---|---|
| Port 4144 is in use | Stop the previous fixture process; do not kill an unknown process. |
| Missing-key banner in normal web mode | Configure a key in the terminal, then select a credentialed profile; restart if you edited its saved credentials externally. |
| Wrong model after configuration | Check environment and `.env` overrides, which take priority over saved settings. |
| Unsupported tool/model error | Select a model and endpoint that implement Chat Completions streaming and tool calls. |
| Linux shell containment failure | Check Bubblewrap and host user-namespace policy; do not disable containment as a demo workaround. |
| Stop cancelled the task but files changed | Cancellation does not roll back tool edits; inspect the disposable workspace diff. |
