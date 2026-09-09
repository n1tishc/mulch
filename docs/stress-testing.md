# Stress testing Mulch before publication

This is a staged maintainer protocol, dated 2026-09-08. The commands below exercise existing tools; the fault matrix and held-out evaluation are additional work to perform, not a claim of completed coverage. See the [actual pilot results](correctness-pilot-results.md) and [cross-harness comparison research](harness-comparison.md).

## 1. Establish a reproducible offline baseline

From the repository root, with the pinned development tools and native sandbox prerequisites installed:

```sh
make verify
make contract
make correctness-test
make distribution-test
make release-dry
```

`make verify` includes frontend tests/build, architecture checks, lint, vet, and the Go race suite. Distribution tests exercise a real host binary through npm, pnpm, and the curl installer, including checksum rejection. See [tool and platform requirements](releases.md). Save the command output, OS/tool versions, source revision and dirty diff, binary hash, and configuration. Do not put API keys in saved logs.

Repeat the concurrency-sensitive packages to reveal intermittent failures:

```sh
go test -race -count=20 -timeout=20m ./internal/event ./internal/score ./internal/intervene ./internal/runtime ./internal/cli
```

This repeats existing tests; it is not a sustained production-load benchmark. Any sequence corruption, cross-session mutation, leaked candidate edit, race detector failure, or failed replay is a release blocker. Fix and retain a regression test before moving on.

## 2. Exercise failures and recovery deliberately

Use deterministic fake providers and disposable repositories first. Add missing integration coverage for the following cases; do not spend model credits to simulate faults that can be reproduced locally.

| Stress | Required observation |
|---|---|
| Duplicate, delayed, and out-of-order scores | Only distinct eligible observations confirm a repair; stale observations cannot trigger it |
| Judge timeout, malformed answer, cancellation, missing usage | Failure reason and freshness are visible; a default/carried-forward score is not presented as a successful check |
| Severe contradiction with otherwise healthy saturation | The intended repair is reachable under the chosen policy; preserve confirmation, cooldown, and critical escalation |
| Duplicate tool payloads across branches | Only the intended parent occurrences change visibility; reopening SQLite reproduces the same context |
| Candidate failures and ties | Deterministic selection, retained evidence, no candidate filesystem edits in the parent |
| Cancellation during tools/scoring, then resume | Terminal status and committed events agree; no orphan activity or duplicated tool execution |
| Kill the daemon during a disposable session, then reopen | Committed history replays; document how interrupted sessions recover instead of assuming automatic resume |
| Many simultaneous sessions, slow viewer, reconnecting browser | No cross-session events; bounded resource use; dashboard and CLI agree on status and recorded decisions |
| Failed install, unsupported platform, missing Linux sandbox | Actionable errors; failed checksum validation preserves the previous executable |

For daemon load, use separate disposable workspaces and a fake provider; increase simultaneous sessions from 1 to 2, 4, 8, then 16. Record error counts, event ordering, memory/goroutines, and p50/p95 completion latency over a fixed 10-minute window per level. This load driver and instrumentation are not implemented by `mulch eval`; its concurrency flag only limits simultaneous evaluation runs. Establish your own acceptable memory/latency envelope before running the sweep.

Use the dashboard to verify failed/partial scoring and actual repair events, then reopen its database and compare the replay. A green health number alone is not evidence of correctness. The current pilot exposed scorer observability and routing gaps; those need fixes before this matrix can pass in full.

## 3. Reproduce a small live diagnostic

Live commands send task and tool context to the configured model provider and consume its allowance. Set a fresh campaign budget and verify current rates before running; the previous pilot's $10 authorization is not a recurring budget. The per-run token guard is conservative, not a provider billing cap.

After configuring `MULCH_PROVIDER_API_KEY`, endpoint, and model as in the README, run one condition with all four modes:

```sh
mkdir -p dist
go build -o dist/mulch ./cmd/mulch
run_dir="eval-output/diagnostic-$(date +%Y%m%d-%H%M%S)"
./dist/mulch eval --runs 1 --concurrency 1 --seed 17 \
  --modes plain,control,intervention,race --token-budget 40000 \
  --output "$run_dir" testdata/scenarios/invoice-degraded.yaml
```

That is four runs, each with its own 40,000 charged-token allowance. Budget all roles, including failed requests and race candidates. Serial execution reduces concurrent-provider effects for diagnosis. Read `eval_results.json` and the report's `trace_db`; inspect it with:

```sh
./dist/mulch serve --addr 127.0.0.1:4141 --db /absolute/path/from/report/trace_db
```

To reproduce the original full 16-run configuration, the existing script fixes its model, endpoint, scenarios, and budget:

```sh
python3 scripts/correctness-pilot.py dist/mulch
# Replace the path with the timestamped directory printed by the script:
python3 scripts/summarize-correctness-pilot.py eval-output/correctness-pilot-YYYYMMDD-HHMMSS
```

The script has a historical $6.40 planning envelope at $10 per million charged tokens; revalidate that assumption for another run. It is not a generic campaign budget manager. Do not blindly increase repetitions or the allowance to hide exhaustion. First diagnose judge failures, confirm meaningful fresh scores, and calibrate enough room for normal task completion. Current eval uses the default policy; it does not expose the standalone `--policy` flag. A revised policy needs explicit evaluator support and a recorded configuration before comparison.

## 4. Freeze, then test held-out correctness

Invoice and pagination are now development fixtures. Build new tasks with independent graders: multi-file changes, long investigations, repeated tool results, conflicting historical advice, near-limit context, and resumptions. For each task, preserve a clean version and a degraded version with the same authoritative specification. Validate graders against a broken implementation and a known correct fix. Keep graders outside agent-readable storage for adversarial evaluations; the current fixture separation is not a confidentiality sandbox.

Start with a feasibility set of 10 new tasks × 2 conditions × 4 modes × 3 repetitions = 240 runs. This is a proposed next-stage design, not a statistically sufficient universal minimum or an authorized paid run. Price it first: at the old 40,000 charged-token allowance, its maximum is 9.6 million charged tokens, far beyond the original pilot envelope. Use a smaller predeclared subset if needed; don't choose the subset after seeing results.

Freeze source, policy, model/version, prompts, tools, limits, graders, and task list before evaluation. Pair runs by task/condition/repetition and randomize mode order; a scheduling seed does not make model responses deterministic. Report every planned run, including infrastructure and budget failures. Do not silently retry poor outcomes.

Record:

- Primary: normal completion AND hidden grader pass.
- Secondary: patch-only pass, clean-task regression, degraded-task gain, repair exposure and counts, harmful/helpful repairs, and scorer availability/freshness.
- Efficiency: all-role reported tokens, conservative reservations separately, known costs or unknown billing, wall time, and p50/p95 latency.
- Reliability: failures by cause, cancellations, replay/recovery results, and missing telemetry.

Compare paired correctness differences with uncertainty clustered by task; repetitions of the same task are not independent new tasks. Per-mode Wilson intervals alone do not establish a paired improvement. If no repairs fire, the experiment did not measure repair efficacy. If budget stops dominate, the budget is confounding the comparison. Both outcomes are findings to diagnose, not evidence to discard.

## 5. Decide what can be released and claimed

A clearly labelled experimental release can expose verified mechanics without proving a quality advantage. Before publishing, require passing offline contracts, native installation smoke checks on every advertised supported execution platform, honest dashboard failure states, and documented recovery/limitations. Windows agent shell tasks remain unsupported; six archive builds do not change that.

For a correctness-improvement claim, additionally require meaningful repair exposure, held-out independently graded gains, uncertainty estimates, acceptable clean-task regressions under a predeclared tolerance, and all-role cost reporting. Set tolerances before the experiment. Preserve counterexamples: a useful personal comparison should show where each harness fails as well as where it wins.

Publishing remains a separate action described in [the release guide](releases.md). This documentation update does not publish packages or run another paid campaign.
