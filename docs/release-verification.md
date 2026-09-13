# Release blocker fixes and verification

Date: 2026-09-13. Local platform: Darwin arm64, Go 1.27.1.

[Main readiness document](release-readiness.md) · [Cancellation research](cancellation-research.md) · [Packaging slice](release-slices/15-release-packaging.md)

## Result

The five numbered defect fixes are implemented and pass local verification. The owner subsequently narrowed the target to a small portfolio release; the eight-provider plan is now an optional [extended roadmap](provider-roadmap.md). The current [release contract](release-readiness.md) retains core workflow, safety and verification requirements without requiring native adapters or a settings rewrite.

The working repository has not been committed or published. Generated web assets and obsolete-hash removals are staged so the packaged index has tracked dependencies. All new regression files, the asset-check script, CI changes, and documentation must be included in the eventual release commit. Existing local evaluation artifacts and .DS_Store were left untouched.

## Fixes and regression evidence

| Defect | Change | Evidence |
|---|---|---|
| Web starts permanently without execution control | Always create the session manager. Its synchronous Prepare callback validates credentials and captures provider/model/mode before launching a task. | Real daemon integration test starts unready, selects ready, executes, switches during a tool turn, resumes with retained history and a new provider, then returns to unready. |
| Failed/multiple settings updates can disagree with displayed configuration | Validate before changing selection; serialize the callback and publishing of its HTTP snapshot. | Invalid-mode/provider update leaves previous selection intact. Race suite passes. |
| Named profile loses to legacy model | Select the profile's first configured model before considering legacy fallback; retain environment and flag priority. | Regression first failed with model=legacy, then passed for both profile selection and environment override. |
| Orphaned descendant keeps output pipe open | Group SIGSTOP, 10ms settle, group SIGKILL, one-second WaitDelay, ESRCH normalization, bounded post-Wait group check. Also clean a background group after an inherited-pipe timeout. | 100-run direct-tool and full-agent process cancellation stress; separate synchronized bookkeeping test; race checks. |
| Live test bypasses production identity/translation | Use production OpenAI adapter, synthetic session ID, canonical JSON replay, and redacted errors. | Live gate passes text, tool arguments/ID, usage, same-session follow-up, and cancellation. Offline fixture verifies exact token counts and fragmented arguments. |
| Embedded index refers to omitted build outputs | Stage matching Vite assets; verify local references and tracking in CI/GoReleaser; fetch each embedded asset in HTTP regression. | Asset checker, embedded HTTP tests, packaged browser smoke, and six native archives pass. |

The cancellation watchdog is not an assertion that a full agent shutdown must meet an exact two-second latency. Agent bookkeeping has synchronized fake-tool coverage; the OS integration test has a five-second diagnostic watchdog and checks actual group disappearance. Direct-tool readiness parsing no longer treats an empty, newly created PID file as a started process, and resolves the shell's real PGID to account for Linux wrappers.

## Commands and local outcomes

| Command/check | Outcome |
|---|---|
| `make verify` | PASS: architecture, lint, clean frontend install/tests/build, embedded asset validation, vet, full Go race suite. Also passed in a separate clean validation checkout. |
| `make contract` | PASS |
| `make correctness-test` | PASS |
| `make web-test` | PASS: 10 frontend unit tests and 9 browser E2E tests. |
| `make distribution-test` | PASS: shell checksum handling and npm/pnpm installation coverage. |
| `make release-dry` | PASS: six archives (Linux/macOS/Windows × amd64/arm64) and checksums. Snapshot only; nothing published. |
| `go test -race ./internal/tool -run '^TestBashParentCancellationKillsProcessGroup$' -count=100 -timeout=60s` | PASS: 100/100 |
| `go test -race ./internal/agent -run 'TestRunRecordsCancellationForEveryInFlightTool\|TestRunCancelsAllToolProcessGroupsAndRecordsSession' -count=100 -timeout=90s` | PASS: 100/100 per test |
| `go test -tags=spike -run '^TestLiveProviderGate$' -v ./internal/provider -count=1 -timeout=180s` with existing credentials loaded privately | PASS against default OpenCode endpoint; 6.98 seconds for both subtests. |
| `git diff --check` and `git diff --cached --check` | PASS |

The verification checkout is an isolated local clone populated with tracked changes plus explicitly selected new source, tests, scripts, and release docs. It excludes local credentials, databases, .DS_Store, and evaluation scratch output. All six required Make targets passed there as well. The final packaging rehearsal used temporary validation commit `86e3e39`; it is not a commit in the user's working repository. Rebuilding the frontend left its tracked asset tree unchanged.

## Packaged-browser smoke

Ego Browser drove the Darwin arm64 snapshot binary against a local deterministic OpenAI-compatible fixture, with isolated configuration, database, and workspace:

1. Open with active profile lacking an API key: read-only explanation is visible.
2. Run `/provider ready` and `/mode plain`: task sending becomes available without restarting the process.
3. Submit a task: user and assistant messages appear, task completes, and the configured provider/model are used.
4. Send a follow-up: the mock sees the same session identity with increased message history.
5. Run `/provider blank`: missing-key guidance returns and execution becomes unavailable.
6. Run `/new`: visible transcript and active selection clear; the previous conversation remains in saved history.

The browser task and fixture server were stopped after testing.

## Online basis for the cancellation change

The [Go Cmd documentation](https://pkg.go.dev/os/exec#Cmd) explains the inherited-pipe wait and how WaitDelay bounds it. [CommandContext](https://pkg.go.dev/os/exec#CommandContext) exposes a custom cancellation callback but defaults to killing only the immediate process. [POSIX kill](https://pubs.opengroup.org/onlinepubs/009604499/functions/kill.html) defines negative-PID process-group signaling and ESRCH. These primary sources were read in Ego Browser. The 10ms staged-stop mitigation is based on the local fork-race experiments; the sources do not guarantee that interval is universally sufficient.

## Still required before publication

- Run the new cancellation CI matrix on Linux and macOS from the actual release commit. Local Linux execution was unavailable because Docker had no running daemon; cross-compilation is not execution evidence.
- Include all intended files and run the full gates on the eventual tagged commit. The working repository still contains uncommitted work.
- Do not claim native support for the deferred eight-provider roadmap. This release exposes one OpenAI-compatible adapter, configured model lists, runtime-only slash settings, and terminal credential entry.
- Select a real release version/tag matching the npm package; the successful rehearsal used a snapshot version.

## Portfolio release finishing pass — 2026-09-13

The owner explicitly reduced the scope to a working project for recruiter demonstrations. The eight-provider plan was preserved as `provider-roadmap.md`; each slice now points to that roadmap and the smaller release contract. README now leads with a no-key walkthrough. The new demo guide, support boundaries, draft 0.1.0 release notes and rollback guidance have Markdown and generated HTML versions.

No new demo engine or launch-mode interface was needed: the existing deterministic browser fixture and `/mode plain` already provide those behaviors. The fixture uses the real runtime and file tool with a fake model; its seeded repair records are explicitly synthetic. Existing acceptance tests cover the walkthrough. No additional tests at new interfaces were introduced in this finishing pass.

Fresh verification of the current working tree:

| Gate | Outcome |
|---|---|
| `make verify` | PASS; lint reports zero issues, frontend has 10 passing unit tests, vet and full Go race suite pass. |
| `make contract` | PASS |
| `make correctness-test` | PASS |
| `make web-test` | PASS; all 9 browser acceptance tests pass. |
| `make distribution-test` | PASS; npm/pnpm and checksum rejection preserve existing installation. |
| `make release-dry` | PASS; six native snapshot archives plus checksums, version `0.0.0-SNAPSHOT-0242996`. This version names the base commit, not a clean release revision. |
| `make docs-html` | PASS; generated documentation refreshed. |
| Embedded asset tracking and whitespace checks | PASS |

The first restricted test attempt could not bind local sockets or load the lint cache. The same gates passed with the required local test permissions; no code change or weakened assertion was used to bypass those failures.

GitHub's [latest committed CI run](https://github.com/n1tishc/mulch/actions/runs/34379906028) is successful for `02429967fcd7a0a3b8a8d27db166cb56353b5f49`. It does **not** cover these uncommitted release fixes or the new cancellation matrix. Linux execution remains pending for the final source revision; the local Docker daemon is unavailable. The earlier clean-checkout and live-provider results above remain historical evidence, not a newly run remote release gate.

Verdict: the narrowed core is locally verified and suitable for a source-based portfolio demonstration. No known additional feature work is required by this release contract. Public artifact approval remains conditional on an intended commit, green final-commit CI, matching version/tag and artifact inspection. No original-repository commit, push, tag or publication was performed.

## Public release candidate

The owner subsequently authorized a release-candidate commit and a push to a new release branch, but no tag, GitHub publication or npm publication. The branch is `release/v0.1.0-rc1`. Earlier statements that no commit/push occurred describe the earlier local-only passes, not the candidate workflow. The owner chose to keep the project source-visible without granting a reuse license; no license file was added.

A final safety inspection found that an upstream authentication error could echo the configured API key into CLI output and persisted errors. `TestChatRedactsProviderCredentialErrors` reproduced the leak through the public CLI with a synthetic HTTP provider. The OpenAI adapter now redacts its configured key before returning an error while preserving error unwrapping. The regression checks both terminal output and JSON replay through CLI commands. This is targeted credential redaction, not a guarantee that arbitrary model/tool content is free of secrets.

`SECURITY.md` documents loopback-only use, local plaintext keys, trace privacy, cancellation limits and reporting guidance. The intended candidate file set was checked for copies of locally configured secret values without printing those values; the scan passed. Local `.DS_Store` and evaluation scratch output are excluded from the candidate and left untouched.

For final-commit status, consult [CI runs on the release branch](https://github.com/n1tishc/mulch/actions/workflows/ci.yml?query=branch%3Arelease%2Fv0.1.0-rc1). A successful older or local run does not approve a different commit. Version metadata is prepared for `0.1.0`; no matching tag is created by this candidate preparation.
