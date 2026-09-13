# Portfolio release readiness

Scope decision: 2026-09-13. Target: a small, demonstrable coding harness, not a full multi-provider platform.

Current stage: public release candidate preparation on `release/v0.1.0-rc1`, authorized by the owner. All six local gates passed in the preceding finishing pass; the candidate adds production credential-error redaction and safe-use documentation. Publication requires green CI on the actual candidate commit and matching tagged artifacts. See the [verification evidence](release-verification.md) and [release-branch CI](https://github.com/n1tishc/mulch/actions/workflows/ci.yml?query=branch%3Arelease%2Fv0.1.0-rc1).

## Release contract

Mulch's portfolio release demonstrates a Go agent loop, durable SQLite sessions, terminal and browser interfaces, and inspectable context-repair experiments. It must be easy to run, honest about its limits, and backed by reproducible tests. Native enterprise adapters, universal model discovery, and a new settings architecture are not prerequisites.

| Area | Release promise | Evidence |
|---|---|---|
| Coding workflow | Streaming text, tools, follow-ups, cancellation, saved history, and a fresh conversation boundary | CLI integration, web E2E, runtime and event tests |
| Providers | One OpenAI Chat Completions-compatible adapter; named endpoint profiles and explicitly configured model lists | Offline adapter contract and opt-in live smoke |
| Switching | Provider, model, and mode changes affect subsequent tasks without erasing history | Real-daemon switching regression and CLI/web tests |
| Configuration | Hidden terminal API-key entry, redacted display, local persistence through `mulch config` | Config and terminal tests |
| Browser | Embedded assets, safe Markdown, visibly distinct messages, reconnect, stop and saved sessions | Asset guard, HTTP tests and desktop/mobile E2E |
| Engineering differentiator | Replayable context changes, linked scoring evidence and isolated candidate comparisons | `make contract`; explicitly experimental, not a proven quality gain |
| Distribution | Source build and checksummed native archives | Verification, installer tests and snapshot packaging |

## Deliberate limits

- Support means protocol compatibility, not “any model from any provider.” Users supply a compatible endpoint, key and tool-capable model. `/model` lists configured choices, not a remotely discovered catalog. The default endpoint has live verification; other services need their own smoke test before being advertised as verified.
- `/provider`, `/model`, and `/mode` are runtime-only. `mulch config` persists launch defaults. Browser API-key entry is intentionally unsupported; configure secrets in the terminal.
- `/new` clears the active conversation, draft and pending request identity. It does not delete saved sessions or undo file edits.
- macOS/Linux are the coding targets. Linux needs Bubblewrap and a host permitting its sandbox. Windows archives support history/viewer operations; Bash execution is unsupported.
- Context scoring, intervention and race modes are experimental. The existing pilot did not establish a correctness advantage. Use plain mode for a straightforward coding demo.
- Run on a disposable workspace with non-sensitive data. Local execution can modify files and run commands; this is not a hosted or multi-user service. API calls consume credits, and saved traces can contain source code and tool output.
- Abrupt termination can leave a saved session marked as running. Automatic crash-lease recovery is deferred; verify its owner has stopped, inspect the trace, and start a new conversation. Normal Stop/Ctrl+C is the supported shutdown path.

## Verification gates

Run from the exact source intended for release:

```sh
make verify
make contract
make correctness-test
make web-test
make distribution-test
make release-dry
```

`make web-test` needs Playwright Chromium installed (`cd ui && npx playwright install chromium`). Distribution tests need npm and pnpm; snapshot packaging needs GoReleaser v2. Generated embedded assets must be tracked. A passing local cross-build is not evidence of execution on another OS.

The [verification report](release-verification.md) records local outcomes and the five fixed blockers. CI must pass on the actual release commit, including Linux/macOS cancellation and installer jobs. Local tests cannot substitute for those remote results.

## Publication checklist

1. Include intended source, regression tests, scripts, documentation and matching embedded assets; exclude credentials and local evaluation scratch files.
2. Run all six gates and the [demo walkthrough](portfolio-demo.md). Verify a small live coding task with independent tests if advertising live coding results.
3. Review the final diff and obtain green CI on that commit. Do not claim a green CI run until it exists.
4. Use the existing `0.1.0` npm metadata for a matching `v0.1.0` tag, or update every package version together. Follow [release staging](releases.md) to build a draft, inspect artifacts and verify checksums.
5. Publish only with the maintainer's authorization. A source/demo-ready project is useful for a portfolio before npm publication; npm is an optional distribution channel, not an engineering milestone.

No additional provider adapter is required for this contract. No public release, tag or registry publication is implied by local verification.

## Implementation slice index

The [extended roadmap](provider-roadmap.md#implementation-slice-index) retains all sixteen slice files. They are optional follow-up work, not an unfinished checklist for this release. Slice 15 remains useful for packaging mechanics, but its expanded provider dependencies no longer apply to the portfolio scope.
