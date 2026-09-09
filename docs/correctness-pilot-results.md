# Correctness pilot — 2026-09-08

**This pilot did not demonstrate a correctness advantage for Mulch's repair modes.** The plain agent completed correctly in 3/4 conditions; scoring-only, intervention, and race modes each completed correctly in 0/4. No interventions or races fired. These are diagnostic observations from one run per mode per condition, not a statistical ranking of harnesses.

The [protocol](correctness-benchmark.md) was fixed before execution. It used GLM-5.3-Flash, the production default policy, a 40,000-token conservative per-run guard across all model roles, twelve turns, and the same synthetic repository fixtures in each mode. Degraded conditions exposed all three planned injections in all eight runs.

## Outcomes

“Completed correctly” requires normal session completion and a passing hidden grader. “Patch correct” grades the retained patch even when execution did not complete. Both were recorded by the evaluator; the primary outcome was not redefined after seeing results.

| Mode | Completed correctly | Patch correct |
|---|---:|---:|
| Plain | 3/4 | 4/4 |
| Scoring only | 0/4 | 3/4 |
| Intervention ladder | 0/4 | 3/4 |
| Repair race | 0/4 | 4/4 |

| Condition | Plain | Scoring only | Ladder | Race |
|---|---|---|---|---|
| Invoice, clean | Budget stop; correct patch | Budget stop; correct patch | Budget stop; correct patch | Budget stop; correct patch |
| Invoice, degraded | Pass | Completed; wrong rounding | Budget stop; correct patch | Budget stop; correct patch |
| Pagination, clean | Pass | Budget stop; correct patch | Budget stop; correct patch | Budget stop; correct patch |
| Pagination, degraded | Pass | Budget stop; correct patch | Budget stop; wrong pagination | Budget stop; correct patch |

There were 12 budget stops, one normal completion with a wrong patch, and three normal correct completions. Overall, 14/16 retained patches passed the hidden graders.

## What the traces establish

**The coherence signal never became informative in this pilot.** Every one of the 70 health records carried coherence = 1. All 30 partial coherence records reused the previous score. There were 30 judge requests: 24 failed at the provider-call layer and six returned usage. The traces do not retain sufficiently detailed judge failure reasons to attribute every partial result to a timeout, cancellation, response parsing, or another cause. A provider call returning usage is not proof that the scorer accepted its response.

**Scoring consumed a substantial part of the guarded allowance.** The primary agent made 112 calls, with 190,983 reported input-plus-output tokens and no failed provider calls. Judges accounted for 218,834 conservative charged tokens, including full reservations for failed/usage-less calls. That charged quantity is intentionally conservative and must not be presented as measured billed usage. No summary or candidate calls occurred.

**The guard can stop a run below 40,000 reported tokens.** It reserves request bytes plus framing and maximum output before dispatch, then reconciles successful calls. The plain clean-invoice run stopped at 21,169 actual reported tokens because the next request's conservative reservation would exceed the allowance. This guard protects the spend envelope, but is a confound when interpreting the pilot as an exact equal-token efficiency benchmark. Failed judge calls retain their reservations.

**Default routing cannot reach reanchor in this configuration while saturation remains healthy.** Relevance is disabled; active weights are 0.3 saturation, 0.2 staleness, and 0.2 coherence. With saturation = 1, the minimum possible composite is:

```text
100 × (0.3 × 1 + 0.2 × 0 + 0.2 × 0) / 0.7 = 42.86
```

Reanchor requires composite < 40. Even perfect detection of bad coherence and stale evidence cannot cross that threshold under those assumptions. Prune has no relevance signal, and high saturation health does not select compaction. Therefore race mode's Prune/Compact/Reanchor trigger cannot fire here. This is a structural calculation from the policy and weights; it does not claim that the live judge actually produced zero-valued scores.

The 49 ladder skip records were: eight with no health score, fourteen awaiting confirmation, five with an old score, and twenty-two reporting recovery within the confirmation window. There were no `intervene.fire` or `race.end` events.

## Budget

Across 142 provider calls, reported usage was 161,226 input tokens and 46,451 output tokens. Conservative charged usage was 409,817 tokens. At the protocol's deliberately high $10/M planning rate, that bounds the tracked envelope at **$4.09817**, below the authorized $10 cap. This is not a reading of the provider invoice.

The [published Zen pricing](https://opencode.ai/docs/zen/) was checked again on 2026-09-08; GLM 5.3 Flash was listed at $0.15 input/$0.50 output per million tokens. The actual endpoint was OpenCode Go, which may draw on subscription allowance or configured overage. No account billing settings were changed. Failed-call billing and caching were not inferred from missing usage.

## What to improve before making a quality claim

1. **Make scoring failure explicit.** Record failure categories and distinguish freshly verified coherence from a carried-forward initial value. A displayed value of 1 must not imply a successful contradiction check.
2. **Align scoring deadlines with the execution lifecycle.** Coherence has an eight-second deadline, while session completion waits at most five seconds for hooks. Diagnose actual request/parse failures before increasing timeouts or selecting a cheaper judge; merely waiting longer could worsen overhead.
3. **Make severe-dimension repairs reachable.** Design and test routing that can react to severe incoherence without requiring unrelated saturation to deteriorate. Preserve the safety, confirmation, and cooldown contracts. Treat any changed policy as a new experimental arm rather than rewriting this baseline.
4. **Separate spend safety from a precise efficiency comparison.** Keep a conservative global money guard, but clearly distinguish reported usage, reserved usage, and denied requests. Calibrate a task budget that permits normal completion before testing a quality difference.
5. **Use held-out tasks after tuning.** These fixtures are now development evidence. Freeze the revised policy, add longer tasks, repeat paired trials on new fixtures, and report regressions on clean context as well as gains on degraded context.

The event log and shared runtime make these failures inspectable. That remains a credible engineering feature. “Proven to improve correctness” is not a supported release claim yet.

## Retained artifacts and reproduction

- [Generated summary](../eval-output/correctness-pilot-20260908-111447/summary.md)
- [Machine-readable summary](../eval-output/correctness-pilot-20260908-111447/summary.json)
- [Predeclared plan](../eval-output/correctness-pilot-20260908-111447/plan.json)
- [Binary and fixture hashes](../eval-output/correctness-pilot-20260908-111447/provenance.json)

The linked summary, plan, and hashes are checked in. Detailed condition reports, SQLite databases, and workspaces are retained locally, not published. With those local artifacts present, regenerate the summary without making provider calls:

```sh
python3 scripts/summarize-correctness-pilot.py eval-output/correctness-pilot-20260908-111447
```

The executable was built from the recorded Git HEAD plus the uncommitted implementation changes. Its hash identifies that build; the Git commit alone does not identify the tested code. No changes to model, policy, or budget were made during the pilot.
