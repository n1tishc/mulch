# What makes Mulch worth demonstrating

The strongest positioning is **auditable context repair for long-running agents**:

> Mulch measures an agent's working context, applies a repair before the next tool execution, and records enough evidence to inspect and replay the decision. Its experimental race mode compares two repairs in isolated workspace copies and adopts the healthier context mutation.

This is a defensible description of this repository, not a claim that no other harness does it. A recruiter can inspect the engineering behind the claim: durable state, asynchronous feedback, policy boundaries, concurrency, and deterministic regression tests. A small Go binary and embedded viewer make that easier to try; they are supporting features, not the central differentiator.

## Evidence and limits

| Capability | Evidence in this repository | What it does not establish |
|---|---|---|
| Repair without erasing history | SQLite visibility events, replacement summaries, replay tests, synthetic ladder scenario | Summaries always preserve every task-critical fact |
| Context-health feedback | Independent scorers, timeouts, previous-value fallback, recorded latency | Health is a calibrated probability of task success |
| Stable policy decisions | Confirmation, freshness, hysteresis, cooldown, escalation tests | Default thresholds are optimal for real workloads |
| Counterfactual repair comparison | Concurrent Prune/Reanchor candidates, parent mutation, retained loser; SQLite occurrence regression | The higher-scored candidate produces the better final answer |
| Practical distribution | Six CGO-free targets, embedded viewer, checksummed installer, script-free npm wrapper | Identical shell capabilities on every operating system |

The earlier checked-in live evaluation has five runs per arm, 100% success in both arms, and **zero interventions**. It is a provider smoke test, not evidence that repair helps. Scoring was on the critical path in 50.0% of control samples and 66.7% of intervention samples. Tool gain was 1.01x. Neither a quality uplift nor a speed advantage should be claimed from this sample. See [the measurements](evaluation-results.md). The subsequent [16-run correctness pilot](correctness-pilot-results.md) also failed to demonstrate an advantage: plain execution completed correctly in 3/4 conditions, each scoring/repair mode in 0/4, and no interventions fired. It exposed scorer failures, conservative budget stops, and unreachable repair thresholds in the tested configuration.

## The contract that must hold

1. Observations used as confirmations must come from distinct increasing turns. A configured four-observation window must actually be reachable.
2. Confirmed critical escalation must cancel pending execution even when race mode is enabled.
3. A repair must affect exactly its selected context events. Repeated text is not an event identifier; branching renumbers sequences.
4. Durable history must explain visibility changes and reconstruct provider messages after repairs.
5. Candidate execution must stay isolated from the parent's workspace; only the selected context mutation is adopted. Candidate tool edits are not merged.
6. Equal candidate health must have a deterministic tie-break: Prune wins. This favors the less intrusive candidate without claiming equal scores imply equal answer quality.

Run `make contract` for deterministic, provider-free tests across intervention, event-store, and scoring packages. These tests include all five ladder actions, protected context, compaction rollback, SQLite reopen after racing, scorer failure, and replay. They are a mechanism demonstration, not a model benchmark.

This review reproduced and fixed critical escalation being replaced by a race, confirmation history being capped at three regardless of policy, duplicate observations satisfying confirmation, and repeated payloads mapping a winning prune to the wrong parent event. It also made race ties deterministic.

## The next credible quality demonstration

Use several tasks with independently checkable answers. Inject stale and contradictory context that crosses the actual default thresholds; record whether a repair fired instead of assuming that a scenario name means it did. Compare scoring-only control, normal intervention, and race mode with the same task inputs, model configuration, and budgets. Preserve every trace, failure, token count, latency, action, and final answer.

Report success separately from health improvement. Include an ablation of the scorers and policy, repeated trials, uncertainty, and examples where intervention hurt. Do not tune on the same task set used to report results. A compelling viewer walkthrough shows the original evidence, observed degradation, precise repair, reconstructed context, and independently graded outcome.

Until those results exist, lead with inspectable correctness and the engineering of feedback-driven execution. Avoid “self-healing,” “prevents hallucinations,” or claims of a proven quality advantage.
