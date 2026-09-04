# Measured evaluation results

These results come from one five-run-per-mode execution of `poisoned-context-smoke`. They are measurements, not projections. The sample is too small for statistical conclusions, and provider latency and output vary between runs. Neither arm fired an intervention, so this run does not establish intervention efficacy.

| Mode | N | Success | Mean turns | Input tokens | Min health | Interventions | Score latency | Critical path | Tool gain |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| control | 5 | 100.0% | 4.40 | 2398.20 | 92.83 | 0 | 1269.70 ms | 50.0% | 1.01x |
| intervention | 5 | 100.0% | 4.80 | 2721.20 | 94.21 | 0 | 2907.33 ms | 66.7% | 1.01x |

Total wall time was 27,524 ms for ten runs. The 1.01x tool gain is effectively no demonstrated parallel speedup in this scenario; it should not be generalized to other workloads. Reproduce with:

```sh
mkdir -p eval-output
mulch eval --runs 5 --concurrency 5 --output eval-output testdata/scenarios/poisoned-context.yaml
```
