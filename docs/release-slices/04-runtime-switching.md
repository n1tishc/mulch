# Slice 4 — Runtime switching and readiness repair

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: startup/readiness defect fixed and verified locally; full slice remains incomplete pending resolver/settings dependencies and persistence.
Depends on: [Slice 1](01-settings-migration.md), [Slice 2](02-provider-resolver.md), [Slice 3](03-openai-compatible.md)

## User outcome

Provider, model, and mode changes affect the next turn in CLI and web—even if Mulch started without usable credentials—without losing conversation history.

## Scope

- Always construct web execution control; do not make it conditional on startup credentials.
- Resolve one immutable selection when a task is accepted.
- Derive readiness from current resolution, capabilities, and execution availability.
- Persist provider/model defaults and apply the agreed mode semantics.
- Re-resolve or safely default the judge after provider changes.
- Return a redacted configuration snapshot after successful mutation.
- Keep settings changes from mutating running primary, judge, summary, or race calls.

## Acceptance criteria

- Start read-only, switch to a credentialed profile, and run without restarting.
- Switching to an incomplete profile disables sending with actionable guidance.
- Switching providers and models preserves the session ID and canonical history.
- Historical turns show their original selection; subsequent turns record the new one.
- A rejected update changes neither persisted nor in-memory active settings.

## Tests and evidence

Implemented regression: `TestServeProviderSwitchFromMissingCredentials` exercises the real daemon, missing-key rejection, valid selection, failed-update rollback, in-flight tool-turn settings, same-session resume with new settings, and reverse readiness. `TestPrepareRejectsUnreadyAndSnapshotsBeforeLaunch` verifies synchronous selection capture. A packaged-binary Ego Browser smoke passed. See [release verification](../release-verification.md).

Unready-to-ready web E2E; reverse transition; in-flight concurrency; run-config assertions; cross-provider resume; judge fallback; invalid-update rollback; readiness truthfulness; same behavior through CLI and server entry points.

## Out of scope

Model catalog UI and non-reference provider adapters.
