# Correctness pilot results

One trial per mode per scenario. This is a diagnostic pilot, not evidence of general superiority.

| Scenario | Plain | Scoring only | Ladder | Race |
|---|---|---|---|---|
| invoice-clean | FAIL (execution) | FAIL (execution) | FAIL (execution) | FAIL (execution) |
| invoice-degraded | PASS | FAIL (grader) | FAIL (execution) | FAIL (execution) |
| pagination-clean | PASS | FAIL (execution) | FAIL (execution) | FAIL (execution) |
| pagination-degraded | PASS | FAIL (execution) | FAIL (execution) | FAIL (execution) |

| Mode | Completed correctly | Patch passed hidden grader |
|---|---:|---:|
| plain | 3/4 | 4/4 |
| control | 0/4 | 3/4 |
| intervention | 0/4 | 3/4 |
| race | 0/4 | 4/4 |

Correct: 3/16. Grader passed: 14. Repair races: 0.
Actions: `{}`. Partial scores: `{'coherence': 30}`.
Reported usage: 161,226 input tokens and 46,451 output tokens across 142 provider calls (24 failed).
Conservative charged tokens: 409,817; at $10/M this is an upper planning estimate of $4.0982, not an account invoice.

## Failed outcomes

- 12: evaluation token budget exhausted
- 1: grader rejected output

## Retained evidence

- [invoice-clean](invoice-clean/eval_results.json)
- [invoice-degraded](invoice-degraded/eval_results.json)
- [pagination-clean](pagination-clean/eval_results.json)
- [pagination-degraded](pagination-degraded/eval_results.json)
