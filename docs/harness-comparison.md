# Comparing Mulch with other coding harnesses

Researched 2026-09-08 against official documentation and first-party research. This is a proposed evaluation plan, not a completed external comparison. No competitor adapters or paid comparison runs were created as part of this research.

Start with the question: **which harness completes my tasks correctly, within my budget, and leaves enough evidence to understand failures?** There need not be one winner. A harness can improve recovery on long tasks while adding unacceptable overhead on small tasks.

Mulch's [16-run pilot](correctness-pilot-results.md) found no correctness advantage: plain completed correctly in 3/4 conditions, the other modes in 0/4, and no repairs or races fired. Twelve runs hit the conservative budget guard. Fix scorer reliability, repair reachability, and budget calibration before interpreting a new comparison as evidence about repair efficacy. The existing invoice/pagination fixtures are now development tasks; reserve new tasks for final evaluation.

## Useful comparison candidates

| Candidate | What it lets you investigate | Practical integration and caveat |
|---|---|---|
| Mulch plain, scoring only, ladder, race | Whether Mulch's own additional machinery changes correctness and overhead | Already supported by `mulch eval --modes plain,control,intervention,race`. This is the strongest controlled test of the repair mechanism. |
| Pi | A minimal coding harness with extensibility, compaction, and session history | Official docs expose print/JSON, RPC, and an SDK, with read/write/edit/bash defaults. A small external runner is plausible; equivalent tool semantics and injection delivery still require implementation and validation. [Pi documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md) |
| mini-SWE-agent | A small baseline with established repository-benchmark execution | Official SWE-bench runner supports task selection, model/config selection, and container environments. Harbor also documents a Mini-SWE-Agent integration. Use pinned releases/configurations, not a headline score copied from its site. [mini SWE-bench guide](https://mini-swe-agent.com/latest/usage/swebench/), [Harbor agents](https://www.harborframework.com/docs/agents) |
| Aider | A different editing/pair-programming workflow | Its CLI accepts a message or message file, edits, and exits. Prefer that interface over its explicitly unsupported Python scripting API. Preselecting helpful files only for Aider would bias the test; either give every arm the same file list or declare this a product-workflow comparison. [Aider scripting](https://aider.chat/docs/scripting.html) |
| SWE-agent | A more configurable repository-issue agent with inspectable trajectories | CLI supports batch runs and trajectory replay; output includes experiment configuration and trajectories. Useful later if you want another full issue-solving workflow. Event logs alone are therefore not unique to Mulch. [SWE-agent CLI](https://swe-agent.com/latest/usage/cli/), [output files](https://swe-agent.com/latest/usage/trajectories/) |

**Recommended first external baseline: Pi.** Its familiar tool surface and headless interface make it a practical comparison for this project. This is an engineering judgment, not a prediction that it will win or lose. Add mini-SWE-agent when moving into standardized container benchmarks. Keep the first matrix small enough to inspect every failure.

## Run two separate tracks

### Controlled comparison

Hold the exact model endpoint/version, sampling/reasoning settings, task text, starting repository, tool permissions, environment, network policy, and stopping allowance constant. Use fresh isolated workspaces and clean configuration directories so home instructions, plugins, prior conversations, and credentials unrelated to the provider do not leak into runs.

Match **capabilities and information**, not merely tool names: file-read truncation, edit errors, shell timeouts, output clipping, and search access affect results. Record any mismatch. Keep each harness's necessary native prompt and edit protocol, archive them, and acknowledge that the result compares these configured systems. Replacing every prompt/tool implementation would produce a modified harness experiment.

For a claim specifically about Mulch repair, use the internal four-arm ablation: those modes share the runtime and tool implementation. An external comparison cannot by itself isolate the causal effect of Mulch's repair algorithm.

### Product-default comparison

Use each product's documented normal configuration, including its own context handling and tools. Keep the task, starting environment, human assistance policy, and total spending/time envelope fixed; record all remaining differences. If a product uses additional models, list them. This answers which setup is more useful to you, not which algorithm is intrinsically better.

Keep human-guided Aider or interactive dashboard sessions in a separate assisted track. Record minutes of human help and interventions; do not mix them with unattended completion rates.

## Tasks, grading, and failure evidence

1. **Author and validate tasks before running agents.** Include multi-file bug fixes, boundary-value handling, regression prevention, a longer investigation, and an interruption/resume task. Pair clean tasks with stale evidence, contradictory advice, or distracting output where context repair is relevant. Verify graders reject original bugs and plausible incomplete fixes while accepting multiple correct implementations.
2. **Keep grading independent.** Run hidden acceptance and regression tests after the agent stops, in a clean grading environment using trusted test definitions. Do not let edits to tests or the specification redefine success. For stronger isolation, keep graders outside the agent container and mount them only for evaluation; Mulch's current write sandbox is not a confidentiality boundary against deliberate host reads. Review surprising passes and failures without knowing the harness label where feasible.
3. **Define completion before execution.** Primary outcome: normal completion plus independent grader pass. Also report patch correctness after timeout, budget exhaustion, escalation, tool errors, and provider failures as separate outcomes. An exit code, confident explanation, or health score is insufficient.
4. **Make degradation equivalent.** Mulch's turn numbers are not automatically comparable to another harness's turns. For the first external test use identical fixture-local stale documents or tool-output events at a defined operation, after implementing and checking delivery. Record whether each run actually encountered the material. Report all assigned runs as primary results; exposure-only slices are diagnostic because fast completion can avoid later injections.
5. **Preserve the evidence.** Save task/grader hashes, source commits and local diff, binary/package versions, complete configuration, prompts, redacted trajectories, patches, stop reasons, timings, and usage. Never put provider secrets in result artifacts.

These are proposed controls for this project. Aider's historical SWE-bench methodology illustrates independent held-out acceptance testing; its 2024 score is not a current comparison target. [Aider methodology](https://aider.chat/2024/05/22/swe-bench-lite.html)

## Budgets and repeated trials

Charge the entire system: agent, judge, summarizer, repair candidates, helper models, embeddings, and retries. Log reported input/output/cache usage separately from reserved tokens and estimated dollars. Include failed requests and unknown usage explicitly. Compare both total cost and wall time, not just primary-agent tokens.

Mulch's current meter reserves serialized request bytes and can stop below 40,000 reported tokens. Do not compare that number directly with a competitor's actual-token cap. First calibrate on development tasks, then apply a common external budget controller or explicitly label differing guards as a limitation. Use a conservative global spend ceiling as well as the experimental allowance. Exact equal-token comparisons are meaningful only when accounting and enforcement match. Turn limits alone are not fair across different tool protocols.

Use paired task/repetition blocks with randomized harness order. A shared scheduling seed does not make provider responses deterministic. Repeat runs from clean state; report average single-attempt correctness, not the best patch across attempts. Best-of-N selection is a separate experiment whose selector and all N attempts must be costed.

For analysis, report task-level wins/losses/ties and clean/degraded results separately. Calculate uncertainty for the **paired difference**, for example by bootstrapping whole task families while retaining their conditions and repetitions together. Repetitions and clean/degraded variants are not independent new tasks. Small samples provide diagnostic evidence; choose a larger sample based on the smallest improvement you care about and observed variation before making a broad claim.

## A feasible first run plan

This staged plan requires a new external runner; the current `mulch eval` only runs Mulch modes.

1. Finish and verify the pilot's scorer/routing/accounting fixes offline. Freeze the revised policy under a new identifier; preserve the old pilot.
2. Implement a runner for Mulch and Pi that creates fresh workspaces, captures outcomes/usage, enforces the agreed allowance, and invokes the same independent grader. Smoke-test it on the existing two development fixtures. Validate cancellation, provider failure, malformed output, and budget stops using mocked responses first.
3. Author **10 new task families**, each with clean/degraded conditions, before inspecting evaluation outcomes. A first held-out matrix with Mulch plain, one frozen Mulch repair mode, and Pi, repeated three times, is `10 × 2 × 3 × 3 = 180 runs`. This is a proposed diagnostic study, not a guaranteed statistically adequate sample or authorization to spend. Estimate a fresh dollar ceiling from development usage before executing; use fewer tasks for a smoke test rather than silently exceeding a cap.
4. Inspect every disagreement and classify it: wrong solution, insufficient tests, bad tool behavior, lost constraint, unhelpful repair, scorer failure, unfinished correct patch, or environment/provider failure. Retain original scores; any grader correction should be applied to all arms and reported in a separately versioned result.
5. If the frozen repair mode helps, add a new held-out set and more families. If it hurts clean tasks or never fires, investigate before expanding. Publish the result that occurred, including tradeoffs.

## Public benchmarks: useful checks, not final authority

**SWE-bench:** the official evaluator consumes patches and grades repository tasks in containerized environments. A pinned, audited subset is useful for checking whether gains extend beyond synthetic tasks. Run the official evaluator rather than relabeling Mulch's internal grader as a SWE-bench score. [SWE-bench evaluation guide](https://www.swebench.com/SWE-bench/guides/evaluation/)

**Verified and Pro need current caveats.** OpenAI's February 23, 2026 analysis described test-design and contamination problems in SWE-bench Verified; its 59.4% issue figure refers to an audited 138-task difficult subset, not the whole benchmark. Its July 8 analysis then estimated roughly 30% of SWE-bench Pro tasks were broken and retracted its earlier Pro recommendation. These are first-party audit findings, not proof that every task is unusable. Use reviewed tasks, retain official scores separately from any corrected grading, and supplement public data with privately authored held-out work. [Verified audit](https://openai.com/index/why-we-no-longer-evaluate-swe-bench-verified/), [later Pro audit](https://openai.com/index/separating-signal-from-noise-coding-evaluations/)

**Terminal-Bench/Harbor:** useful for broader terminal work, tool handling, and longer execution. Terminal-Bench is the task suite; Harbor is the evaluation framework. The official site lists multiple versions, so pin one dataset and never mix their scores. Version 2.0 is a documented starting option, not a claim that it is the newest release. [Terminal-Bench versions](https://www.tbench.ai/leaderboard), [2.0 and Harbor announcement](https://www.tbench.ai/news/announcement-2-0)

Harbor supports custom installed agents running headlessly inside its environment. A Mulch adapter would need installation, execution, trajectory/outcome collection, and accounting; it does not exist in this repo yet. Its ordinary CLI must also be checked against the evaluation container's sandbox and resource constraints. The documented 2.0 leaderboard disallows changing resource limits or timeouts, so custom stress settings produce a private experiment rather than a comparable leaderboard score. [Harbor integration interface](https://www.harborframework.com/docs/agents), [2.0 leaderboard rules](https://www.tbench.ai/leaderboard/terminal-bench/2.0?verified=true)

## What a satisfying result would say

Use a statement scoped to evidence: “On this frozen set of tasks, model, versions, and allowance, Mulch completed X/Y tasks correctly versus Z/Y, with this uncertainty, cost, latency, and these failure cases.” Include the repair activation rate and whether it preserved clean-task correctness.

Mulch's promising feature is an inspectable context-repair experiment. Neither the existing pilot nor this research establishes superiority. Finding where another harness works better is useful evidence for deciding what to improve and when to use each tool.
