# Slice 15 — Packaging, release gates, and publication rehearsal

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: defect fixes and local release rehearsal complete; final release-commit CI and version/tag remain. Extended provider dependencies are deferred for the portfolio release.
Depends on: current portfolio release contract, not every roadmap slice

## User outcome

The exact source commit produces installable, complete, secret-free packages whose advertised provider behavior has been verified.

## Scope

- Track regenerated embedded UI assets and reject missing/stale asset references.
- Exclude `.DS_Store`, local databases, logs, evaluation scratch output, credentials, and machine-local artifacts.
- Fix the process-group cancellation leak and redesign its release gate using the evidence in [cancellation timeout research](../cancellation-research.md).
- Make the opt-in live provider gate exercise the production adapter with a non-empty session ID, then pass its text/tool/usage and cancellation checks.
- Run all offline, race, contract, correctness, web, and distribution gates.
- Run provider smoke tests in a protected manual/CI environment.
- Build six native archives, generate checksums, and test curl/npm/pnpm installation.
- Align version output, tags, archive names, npm metadata, and release notes.
- Document exact provider authentication limitations and intentionally unsupported features.

## Acceptance criteria

- A clean checkout creates byte-complete packages.
- Installed binaries open their embedded web UI without a development server.
- No credential or local artifact exists in the diff, archives, logs, screenshots, or npm package.
- Terminal/web smoke tests cover selection, one task, one follow-up, `/new`, and reopen.
- A rollback rehearsal is complete before publication.

## Required commands

Run the full command set in the main [release verification section](../release-readiness.md#verification-gates), plus archive-content, secret-scan, checksum, and installed-binary checks.

## Evidence

The [2026-09-13 verification record](../release-verification.md) covers the five concrete fixes, offline and live results, 100-run cancellation stress, installer checks, six snapshot archives, and packaged-binary Ego Browser smoke. The embedded asset guard now runs in CI and GoReleaser. Cross-platform cancellation execution remains a CI gate.

Attach command output, archive manifests, checksums, platform matrix, smoke-test results by provider, known limitations, and the draft release notes to the release issue.

## Out of scope

Publishing until every gate is green and the release owner explicitly approves the final artifacts.
