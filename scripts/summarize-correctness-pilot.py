#!/usr/bin/env python3
"""Summarize retained pilot evidence without contacting a model provider."""
import collections
import json
import pathlib
import sqlite3
import sys

root = pathlib.Path(sys.argv[1]).resolve()
reports = sorted(root.glob('*/eval_results.json'))
if len(reports) != 4:
    raise SystemExit(f'Expected four completed scenario reports; found {len(reports)}')
rows = []
usage = collections.Counter()
usage_by_role = collections.defaultdict(collections.Counter)
failures = collections.Counter()
actions = collections.Counter()
partials = collections.Counter()
latencies = []
for path in reports:
    report = json.loads(path.read_text())
    db = pathlib.Path(report['trace_db'])
    with sqlite3.connect(db.as_uri() + '?mode=ro', uri=True) as conn:
        for run in report['runs']:
            events = [(typ, json.loads(payload)) for typ, payload in conn.execute(
                'select type,payload from events where session_id=? order by seq', (run['session_id'],))]
            partials.update(p.get('name', 'unknown') for typ, p in events if typ == 'score.partial')
            rows.append({'scenario': report['scenario'], **run})
            actions.update(run['intervention_counts'])
            for role_name, role in run['usage'].items():
                for key in ['calls', 'input_tokens', 'output_tokens', 'failed_calls', 'reserved_tokens']:
                    usage[key] += role[key]
                    usage_by_role[role_name][key] += role[key]
            if run.get('error'):
                failures[run['error']] += 1
            elif not run['success']:
                failures['grader rejected output'] += 1
            latencies.append(run['wall_ms'])
if len(rows) != 16:
    raise SystemExit(f'Expected 16 runs; found {len(rows)}')
by_mode = {}
for mode in ['plain', 'control', 'intervention', 'race']:
    selected = [r for r in rows if r['mode'] == mode]
    by_mode[mode] = {'runs': len(selected), 'correct': sum(r['success'] for r in selected), 'grader_passed': sum(r['grader_passed'] for r in selected)}
summary = {
    'runs': len(rows), 'correct': sum(r['success'] for r in rows),
    'grader_passed': sum(r['grader_passed'] for r in rows),
    'races': sum(r['races'] for r in rows), 'actions': dict(actions),
    'by_mode': by_mode, 'usage_by_role': {k: dict(v) for k, v in usage_by_role.items()},
    'provider_usage': dict(usage), 'execution_failures': dict(failures),
    'partial_scores': dict(partials),
    'conservative_spend_upper_usd': usage['reserved_tokens'] * 10 / 1_000_000,
}
(root / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
lines = ['# Correctness pilot results', '',
         'One trial per mode per scenario. This is a diagnostic pilot, not evidence of general superiority.', '',
         '| Scenario | Plain | Scoring only | Ladder | Race |',
         '|---|---|---|---|---|']
for scenario in sorted({r['scenario'] for r in rows}):
    selected = {r['mode']: r for r in rows if r['scenario'] == scenario}
    def outcome(mode):
        r = selected[mode]
        return 'PASS' if r['success'] else ('FAIL (execution)' if r.get('error') else 'FAIL (grader)')
    lines.append('| ' + scenario + ' | ' + ' | '.join(outcome(m) for m in ['plain','control','intervention','race']) + ' |')
lines += ['', '| Mode | Completed correctly | Patch passed hidden grader |', '|---|---:|---:|']
for mode, counts in by_mode.items():
    lines.append(f"| {mode} | {counts['correct']}/{counts['runs']} | {counts['grader_passed']}/{counts['runs']} |")
lines += ['', f"Correct: {summary['correct']}/{summary['runs']}. Grader passed: {summary['grader_passed']}. Repair races: {summary['races']}.",
          f"Actions: `{dict(actions)}`. Partial scores: `{dict(partials)}`.",
          f"Reported usage: {usage['input_tokens']:,} input tokens and {usage['output_tokens']:,} output tokens across {usage['calls']} provider calls ({usage['failed_calls']} failed).",
          f"Conservative charged tokens: {usage['reserved_tokens']:,}; at $10/M this is an upper planning estimate of ${summary['conservative_spend_upper_usd']:.4f}, not an account invoice.",
          '', '## Failed outcomes', '']
for error, count in failures.items():
    lines.append(f'- {count}: {error.replace(chr(10), " ")}')
lines += ['', '## Retained evidence', '']
for path in reports:
    lines.append(f'- [{path.parent.name}]({path.relative_to(root).as_posix()})')
(root / 'summary.md').write_text('\n'.join(lines) + '\n')
print(json.dumps(summary, indent=2))
