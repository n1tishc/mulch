# Correctness benchmark protocol

Status: the approved live pilot completed on 2026-09-08. See [results and diagnosis](correctness-pilot-results.md). It did not demonstrate a correctness advantage; no interventions or races fired.

## Claim and primary outcome

With the same model, tools, task specification, and total model-token allowance, does context repair improve independently verified completion on tasks with degraded context?

A run is correct only when its session completed normally AND its external grader passes. Escalation, cancellation, time/turn/token exhaustion, and a wrong patch are failures for this primary outcome. The report separately preserves grader success, execution error, exposure, health, interventions, races, and role-level usage. A higher health score is never a correctness label.

## Design

Two synthetic Python repositories each have a clean and degraded condition:

| Repository | Required fix | Hidden edge cases |
|---|---|---|
| Invoice | Fix invoice calculation and checkout integration | Half-up rounding after subtotal, huge integers, invalid types/ranges, empty input, input preservation |
| Pagination | Fix cursor pagination and multi-page export | Timestamp ties, deleted cursor rows, exclusive ordering, exact final page, row copying, invalid limits |

The task and fixture are identical between clean and degraded conditions. Degraded runs receive a stale result before turn 1, contradictory compatibility advice at turn 2, and a distractor at turn 3. The advice contradicts the authoritative specification but is not labelled “untrusted” or supplied with an explicit hint to ignore it. Early completion can avoid later injections; exposure counts are retained, not silently filtered out.

Every condition runs four modes:

- `plain`: core agent with no scoring or repair.
- `control`: asynchronous scoring only.
- `intervention`: scoring plus the default ladder and summarizer.
- `race`: the same configuration with Prune/Reanchor comparison when the production policy triggers it.

CLI run, dashboard sessions, and eval use `internal/runtime.Config`. Eval uses the default saturation/staleness/coherence scorers and default weights, without an embedding provider. Judge and summary calls use the same model as the agent. No policy thresholds are tuned for this pilot. Candidate workspaces are disposable copies and candidate edits are not merged.

Order is deterministically shuffled within each repetition. The seed is recorded. Requests are not assumed deterministic: this seed controls scheduling order, not model sampling. The first pilot has one run per mode per condition (16 runs), so it diagnoses workload difficulty and repair exposure; it cannot establish a statistically reliable improvement. Wilson intervals are shown per mode/scenario, not treated as a test of a paired quality difference.

## Evidence and grading

Each invocation creates a unique `traces-*` directory under the specified output directory. It retains the SQLite database and agent workspaces; report session IDs remain replayable. Reports include the scenario configuration, model, seed, policy, hidden-grader SHA-256, per-run output/errors, and all-role usage.

Hidden graders are validated against buggy and correct implementations. The authoritative SPEC.md is protected by comparison with the source fixture. Grader code is materialized only after agent completion in a separate grading copy, and executes under the normal tool filesystem sandbox. This is a coding-correctness evaluation, not an adversarial test-harness-escape benchmark: the existing shell sandbox restricts writes but is not a general confidentiality boundary against deliberate host reads.

Benchmark prompts use only the built-in coding prompt and fixture-local SYSTEM.md/AGENTS.md, if present. Home and ancestor instructions are excluded. The generated fixtures contain no user application code. Grader source is not included in the model prompt or copied into the agent's fixture.

## Budget and provider payload

The approved pilot uses `glm-5.3-flash` at `https://opencode.ai/zen/go/v1`, with 40,000 charged tokens per run across primary, judge, summary, and candidate calls. Before a request, the meter reserves serialized request bytes plus framing allowance and at most 2,048 output tokens. Successful calls reconcile against reported usage; failed or usage-less calls keep the full reservation. Benchmark calls disable SDK retries. Parent runs have a three-minute deadline and twelve-turn limit. Race calls share their parent's token budget.

The 16-run envelope is 640,000 charged tokens. Reserving $10 per million tokens yields a conservative $6.40 envelope under the authorized $10 ceiling. That is a planning ceiling, not a provider invoice. The [published Zen table](https://dev.opencode.ai/docs/zen) lists GLM 5.3 Flash at $0.15 input/$0.50 output per million tokens, while [OpenCode Go](https://opencode.ai/docs/go/) is subscription-based and may draw on allowance or configured overage. Account billing settings are not changed. Prices were checked on 2026-09-07; recheck before later pilots.

The external payload consists of the synthetic invoice/pagination task instructions, fixture code/specification, injected stale/contradictory/distracting text, and the resulting assistant/tool context needed for subsequent requests. The configured API key is used for authentication. OpenCode's documentation requests client identification and stable session IDs, so requests include a Mulch user-agent and session header.

## Reproduce

Offline (no provider calls):

```sh
make correctness-test
```

After external-transfer approval, run the concrete bounded pilot:

```sh
go build -o dist/mulch ./cmd/mulch
python3 scripts/correctness-pilot.py dist/mulch
```

The script creates a timestamped directory under `eval-output`, writes its plan, stops if the provider cannot complete any call, and retains all scenario reports. It does not retry failed invocations or change account settings.

Manual single-condition example:

```sh
mulch eval --runs 1 --concurrency 2 --seed 17 --token-budget 40000 \
  --output eval-output/invoice-degraded testdata/scenarios/invoice-degraded.yaml
mulch serve --addr 127.0.0.1:4141 --db /absolute/path/from/report/trace_db
```

## Decision after the pilot

- If no repairs fire, the pilot does not test repair efficacy. Diagnose exposure, score availability and routing before claiming any advantage.
- If all modes pass, use harder held-out tasks rather than reporting a quality win.
- If interventions hurt clean tasks, retain those failures and diagnose them before release claims.
- If repair improves these tasks, freeze the policy and evaluate on new tasks with repeated, paired trials, all-role cost and latency. Do not report a training-set gain as general superiority.
- A release can describe verified mechanics even when comparative quality remains unproven. The dashboard must display the same actual behavior being described.
