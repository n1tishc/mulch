#!/usr/bin/env python3
"""Run 16 diagnostic evaluations with an authorized $10 ceiling.

The 640,000 charged-token envelope reserves $6.40 at $10/million, above the
verified GLM-5.3-Flash rates. This is not a provider invoice estimate. Do not
change the model, number of runs, or budget without revisiting the envelope.
"""
import json
import os
import pathlib
import subprocess
import sys
import time

binary = str(pathlib.Path(sys.argv[1]).resolve())
root = pathlib.Path('eval-output') / ('correctness-pilot-' + time.strftime('%Y%m%d-%H%M%S'))
root.mkdir(parents=True, exist_ok=False)
(root / 'plan.json').write_text(json.dumps({
    'model': 'glm-5.3-flash', 'runs_per_mode_per_scenario': 1,
    'modes': ['plain', 'control', 'intervention', 'race'],
    'token_budget_per_run': 40000, 'maximum_runs': 16,
    'conservative_spend_envelope_usd': 6.40, 'authorized_cap_usd': 10,
    'purpose': 'diagnostic pilot, not superiority evidence',
}, indent=2) + '\n')
print(f'Pilot artifacts: {root}', flush=True)
env = dict(os.environ, MULCH_PROVIDER_BASE_URL='https://opencode.ai/zen/go/v1', MULCH_MODEL='glm-5.3-flash')
for name in ['invoice-clean', 'invoice-degraded', 'pagination-clean', 'pagination-degraded']:
    dest = root / name
    result = subprocess.run([binary, 'eval', '--runs', '1', '--concurrency', '2', '--seed', '17', '--token-budget', '40000', '--model', 'glm-5.3-flash', '--output', str(dest), f'testdata/scenarios/{name}.yaml'], env=env, capture_output=True, text=True, timeout=900)
    (root / (name + '.log')).write_text(result.stdout + result.stderr)
    if result.returncode:
        print(f'{name}: execution failed; inspect {root / (name + ".log")}', flush=True)
        sys.exit(result.returncode)
    report = json.loads((dest / 'eval_results.json').read_text())
    for run in report['runs']:
        print(f"{name} {run['mode']}: correct={run['success']} interventions={run['interventions']} races={run['races']} exposed={run['injections_seen']} charged_tokens={run['charged_tokens']}", flush=True)
    usages = [u for run in report['runs'] for u in run['usage'].values()]
    if not usages or all(u['calls'] == u['failed_calls'] for u in usages):
        print('Stopping: no successful provider calls. No further scenarios will run.', flush=True)
        sys.exit(1)
print(f'Completed pilot: {root}', flush=True)
