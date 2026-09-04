# Evaluation policy evidence

The checked-in `poisoned-context-smoke` scenario is exercised in automated tests with an offline recording provider. Control and intervention runs both retain a 100% success rate, so there is no measured regression that warrants changing the default intervention policy in this ticket.

Real-provider results are deliberately not claimed by the test suite. Run `mulch eval --runs 5 --concurrency 5 testdata/scenarios/poisoned-context.yaml` and retain the generated JSON and Markdown when tuning thresholds. If the intervention success rate is lower than control, adjust `intervene.DefaultPolicy` and record the before/after reports here.
