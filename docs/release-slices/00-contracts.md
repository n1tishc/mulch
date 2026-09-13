# Slice 0 — Freeze product contracts and terminology

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: nothing

## User outcome

Every later slice implements the same provider, persistence, command, and session semantics.

## Scope

- Adopt the eight first-class providers and the custom `openai-compatible` type.
- Persist `/provider` and `/model` as future defaults. Store a default `/mode`, with conversation-level overrides.
- Decide whether browser `/api-key` remains terminal-only for this release.
- Define `/new`: clear active transcript, draft, pending request identity, queued input, selected session, transient notices/errors, and inspector selection; retain saved sessions and project files.
- Define the normalized provider error categories and stop reasons.
- Record the provider resolver seam and canonical message ownership in an architecture decision record.

## Likely code and documentation areas

`internal/provider`, `internal/runtime`, `internal/cli`, `internal/server`, `ui/src`, `README.md`, and workflow documentation.

## Acceptance criteria

- Help text, README, web copy, terminal copy, event metadata, and planning documents consistently use: provider profile, provider type, model, active selection, and conversation.
- Persistence and `/new` behavior are unambiguous and have no conflicting documentation.
- Provider-specific SDK types are declared internal to provider adapters.

## Tests and evidence

- Documentation links and command examples validate where automation exists.
- Architecture tests prevent CLI, runtime, server, scoring, and evaluation packages from importing provider SDK packages directly.
- The pull request includes the approved architecture decision and a list of intentionally deferred behavior.

## Out of scope

Provider implementations, schema migration, and UI implementation.
