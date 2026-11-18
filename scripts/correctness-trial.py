#!/usr/bin/env python3
"""Run a held-out, two-arm correctness trial under a declared spend ceiling.

Arms
  baseline  the 2026-09-08 pilot's composite-only policy
            (testdata/policies/pilot-2026-09-08.json)
  severe    the built-in policy with per-dimension severe thresholds
Plain and scoring-only control never consult the intervention policy, so they run
once per scenario and are shared by both arms.

Phase 1 calibrates the per-run token budget: plain mode runs once per scenario
with an allowance of --max-budget, and the trial budget is fixed at twice the
largest charged usage (rounded up to 5,000) so a normal plain run can finish. This
rule is declared here, before any result is seen. Phase 2 runs every condition.

Spend is bounded before anything is sent. The worst case assumes every run is
charged the full --max-budget; if that exceeds --cap-usd the script refuses to start.
If calibration yields a budget above --max-budget, it stops before Phase 2. The plan
is written before calibration and updated with the calibrated budget. Charged tokens
are the meter's conservative figures priced at a deliberately high planning rate
(default $10 per million): an upper planning bound, not a provider invoice estimate.

Usage
  scripts/correctness-trial.py run dist/mulch --cap-usd CAP [--runs 3] [--max-budget 60000] [--dry-run]
  scripts/correctness-trial.py summarize eval-output/correctness-trial-YYYYmmdd-HHMMSS
--dry-run prints the worst-case envelope and exits without a key or any spend.
Provider settings come from MULCH_PROVIDER_API_KEY and MULCH_PROVIDER_BASE_URL.
Do not change the scenarios, arms, or budget rule after seeing results; a changed
policy is a new arm and needs fresh held-out tasks.
"""
import argparse
import collections
import json
import math
import os
import pathlib
import sqlite3
import subprocess
import sys
import time

SCENARIOS = ['ratelimit-clean', 'ratelimit-degraded', 'settings-clean', 'settings-degraded']
BASELINE_POLICY = 'testdata/policies/pilot-2026-09-08.json'
ARMS = {
    # arm name: (policy file or None for the built-in policy, modes)
    'severe': (None, ['plain', 'control', 'intervention', 'race']),
    'baseline': (BASELINE_POLICY, ['intervention', 'race']),
}
SEED = 23


def usd(tokens, rate):
    return tokens * rate / 1_000_000


def eval_once(args, scenario, dest, modes, runs, budget, policy):
    command = [args.binary, 'eval', '--runs', str(runs),
               '--concurrency', '2', '--seed', str(SEED), '--token-budget', str(budget),
               '--modes', ','.join(modes), '--model', args.model, '--output', str(dest)]
    if args.judge_model:
        command += ['--judge-model', args.judge_model]
    if policy:
        command += ['--policy', policy]
    command.append(f'testdata/scenarios/{scenario}.yaml')
    dest.mkdir(parents=True, exist_ok=False)
    result = subprocess.run(command, capture_output=True, text=True, timeout=3600)
    (dest / 'eval.log').write_text(result.stdout + result.stderr)
    if result.returncode:
        raise SystemExit(f'{scenario} ({dest.name}): execution failed; inspect {dest / "eval.log"}')
    report = json.loads((dest / 'eval_results.json').read_text())
    usages = [u for run in report['runs'] for u in run['usage'].values()]
    if not usages or all(u['calls'] == u['failed_calls'] for u in usages):
        raise SystemExit(f'{scenario} ({dest.name}): no successful provider calls; stopping before further spend.')
    for run in report['runs']:
        print(f"{scenario} {dest.name} {run['mode']}: correct={run['success']} interventions={run['interventions']} "
              f"races={run['races']} charged_tokens={run['charged_tokens']}", flush=True)
    return report


def trial_runs(runs):
    return len(SCENARIOS) * sum(len(modes) for _, modes in ARMS.values()) * runs


def run(args):
    calibration_tokens = len(SCENARIOS) * args.max_budget
    worst_case = usd(calibration_tokens + trial_runs(args.runs) * args.max_budget, args.rate)
    print(f'Worst case: {len(SCENARIOS)} calibration runs + {trial_runs(args.runs)} trial runs, each charged at most '
          f'{args.max_budget:,} tokens at ${args.rate:g}/M = ${worst_case:.2f} (cap ${args.cap_usd:.2f}).', flush=True)
    if worst_case > args.cap_usd:
        raise SystemExit(f'Refusing to start: the worst case exceeds the cap. Raise --cap-usd to at least {worst_case:.2f}, '
                         'or lower --runs or --max-budget.')
    if args.dry_run:
        return
    if not os.environ.get('MULCH_PROVIDER_API_KEY'):
        raise SystemExit('MULCH_PROVIDER_API_KEY is required')
    args.binary = str(pathlib.Path(args.binary).resolve())
    root = pathlib.Path('eval-output') / ('correctness-trial-' + time.strftime('%Y%m%d-%H%M%S'))
    root.mkdir(parents=True, exist_ok=False)
    print(f'Trial artifacts: {root}', flush=True)
    plan = {
        'purpose': 'held-out two-arm correctness trial; diagnostic, not a superiority claim',
        'scenarios': SCENARIOS, 'arms': {name: {'policy': policy or 'built-in', 'modes': modes} for name, (policy, modes) in ARMS.items()},
        'runs_per_mode': args.runs, 'seed': SEED, 'model': args.model, 'judge_model': args.judge_model or args.model,
        'calibration': {'allowance_per_run': args.max_budget, 'rule': 'budget = max(5000, ceil(2 * max(plain charged) / 5000) * 5000)'},
        'max_budget_per_run': args.max_budget, 'planning_rate_usd_per_million': args.rate,
        'worst_case_envelope_usd': round(worst_case, 4), 'authorized_cap_usd': args.cap_usd,
    }
    write_plan = lambda: (root / 'plan.json').write_text(json.dumps(plan, indent=2) + '\n')
    write_plan()

    calibration = []
    for scenario in SCENARIOS:
        report = eval_once(args, scenario, root / 'calibration' / scenario, ['plain'], 1, args.max_budget, None)
        calibration += [run['charged_tokens'] for run in report['runs']]
    budget = max(5_000, math.ceil(2 * max(calibration) / 5_000) * 5_000)
    plan['calibration']['plain_charged_tokens'] = calibration
    plan['token_budget_per_run'] = budget
    plan['conservative_envelope_usd'] = round(usd(calibration_tokens + trial_runs(args.runs) * budget, args.rate), 4)
    write_plan()
    if budget > args.max_budget:
        raise SystemExit(f'Calibrated budget {budget:,} exceeds --max-budget {args.max_budget:,}: plain runs need more than '
                         f'half the allowance. Stopping before Phase 2; plan written to {root / "plan.json"}')
    print(f'Calibrated per-run budget: {budget:,} tokens. Envelope ${plan["conservative_envelope_usd"]:.2f}.', flush=True)
    for scenario in SCENARIOS:
        for arm, (policy, modes) in ARMS.items():
            eval_once(args, scenario, root / scenario / arm, modes, args.runs, budget, policy)
    summarize(root)
    print(f'Completed trial: {root}', flush=True)


def summarize(root):
    root = pathlib.Path(root).resolve()
    rows = []
    partial_reasons, skip_reasons = collections.Counter(), collections.Counter()
    denied = collections.Counter()
    for path in sorted(root.glob('*/*/eval_results.json')):
        if path.parent.parent.name == 'calibration':
            continue
        report = json.loads(path.read_text())
        arm = path.parent.name
        with sqlite3.connect(pathlib.Path(report['trace_db']).as_uri() + '?mode=ro', uri=True) as conn:
            for run in report['runs']:
                for typ, payload in conn.execute('select type, payload from events where session_id=? order by seq', (run['session_id'],)):
                    p = json.loads(payload)
                    if typ == 'score.partial':
                        partial_reasons[f"{p.get('name', 'unknown')}:{p.get('reason') or 'unrecorded'}"] += 1
                    elif typ == 'intervene.skip':
                        skip_reasons[(p.get('reason') or 'unrecorded').split(';')[0]] += 1
                for role, usage in run['usage'].items():
                    denied[role] += usage.get('denied_calls', 0)
                shared = run['mode'] in ('plain', 'control')
                rows.append({'scenario': report['scenario'], 'arm': 'shared' if shared else arm, **run})
    if not rows:
        raise SystemExit(f'No trial reports under {root}')
    groups = collections.defaultdict(list)
    for row in rows:
        groups[(row['arm'], row['mode'])].append(row)
    table = []
    for (arm, mode), selected in sorted(groups.items()):
        actions = collections.Counter()
        for r in selected:
            actions.update(r['intervention_counts'])
        table.append({'arm': arm, 'mode': mode, 'runs': len(selected), 'correct': sum(r['success'] for r in selected),
                      'grader_passed': sum(r['grader_passed'] for r in selected),
                      'budget_stops': sum('budget' in (r.get('error') or '') for r in selected),
                      'actions': dict(actions), 'races': sum(r['races'] for r in selected)})
    summary = {'conditions': table, 'scorer_failures': dict(partial_reasons), 'skip_reasons': dict(skip_reasons.most_common(12)),
               'denied_calls_by_role': {k: v for k, v in denied.items() if v}}
    (root / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    lines = ['# Correctness trial results', '',
             'Held-out tasks, repeated trials. Correct means normal completion and a passing hidden grader. '
             'Diagnostic evidence, not a general superiority claim.', '',
             '| Arm | Mode | Correct | Patch passed | Budget stops | Repairs fired | Races |', '|---|---|---:|---:|---:|---|---:|']
    for row in table:
        lines.append(f"| {row['arm']} | {row['mode']} | {row['correct']}/{row['runs']} | {row['grader_passed']}/{row['runs']} | "
                     f"{row['budget_stops']} | {row['actions'] or '—'} | {row['races']} |")
    lines += ['', f'Scorer failures by reason: `{dict(partial_reasons)}`.', f'Most common skip reasons: `{dict(skip_reasons.most_common(6))}`.',
              f"Requests refused by the budget guard: `{summary['denied_calls_by_role']}`."]
    (root / 'summary.md').write_text('\n'.join(lines) + '\n')
    print('\n'.join(lines))


def main():
    parser = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    sub = parser.add_subparsers(dest='command', required=True)
    r = sub.add_parser('run')
    r.add_argument('binary')
    r.add_argument('--cap-usd', type=float, required=True, help='authorized conservative spend ceiling')
    r.add_argument('--runs', type=int, default=3, help='repeated trials per mode per arm per scenario')
    r.add_argument('--model', default=os.environ.get('MULCH_MODEL', 'glm-5.3-flash'))
    r.add_argument('--judge-model', default=os.environ.get('MULCH_JUDGE_MODEL', ''))
    r.add_argument('--rate', type=float, default=10.0, help='planning rate in USD per million charged tokens')
    r.add_argument('--max-budget', type=int, default=60_000, help='largest per-run token budget the cap must cover')
    r.add_argument('--dry-run', action='store_true', help='print the worst-case envelope and exit')
    s = sub.add_parser('summarize')
    s.add_argument('root')
    args = parser.parse_args()
    if args.command == 'run':
        if args.runs < 1 or args.max_budget < 5_000:
            raise SystemExit('--runs must be positive and --max-budget at least 5000')
        run(args)
    else:
        summarize(args.root)


if __name__ == '__main__':
    main()
