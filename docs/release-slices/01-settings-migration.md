# Slice 1 — Versioned settings and safe migration

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 0](00-contracts.md)

## User outcome

Users can configure multiple typed providers without breaking existing Mulch installations.

## Scope

- Add typed, versioned settings and provider-profile structures.
- Centralize flag, environment, named-profile, legacy, and built-in precedence.
- Read flat keys and `provider.NAME.*` keys as version 1 input.
- Migrate in memory; write version 2 only on explicit migration or first mutation.
- Back up the old file before the first schema rewrite.
- Validate provider-specific fields, redact secrets, serialize updates, and retain atomic rename plus owner-only permissions.
- Preserve `MULCH_CONFIG`, XDG paths, `.env`, and legacy environment compatibility.

## Precedence contract

1. Explicit launch flag.
2. Environment variable or `.env`.
3. Active named provider profile.
4. Legacy flat setting.
5. Built-in default.

## Likely code areas

Create a settings module and move responsibility out of `internal/cli/config.go`. CLI and web should consume immutable redacted/effective snapshots rather than reconstructing configuration.

## Acceptance criteria

- Existing files load without modification.
- A named provider uses its own default model even when legacy flat settings exist.
- Failed validation or persistence leaves the prior file usable.
- `config show` cannot reveal any supported credential field.
- Errors identify the invalid profile and field without printing its value.

## Tests and evidence

Table-driven precedence tests; v1 migration fixtures; unknown/malformed version tests; concurrent mutation tests; permission and atomic-write tests; redaction snapshots/fuzzing; interrupted-write simulation; migration backup/rollback; legacy-model regression.

## Out of scope

Provider network calls and model discovery.
