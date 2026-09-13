# Slice 6 — Credential flow and secret safety

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slices 1–5](../release-readiness.md#implementation-slice-index)

## User outcome

Users can configure each provider without leaking credentials, while local providers work without fake keys.

## Scope

- Make terminal `/api-key` provider-aware.
- Mask by rune count, support paste/cancel, and clear secret buffers promptly.
- Centralize redaction for settings, errors, diagnostics, logs, HTTP responses, events, screenshots, and test artifacts.
- Use AWS and Google credential-chain references instead of storing temporary cloud tokens.
- Treat Ollama as credentialless by default.
- Keep browser credential entry disabled with direct terminal guidance unless Slice 0 approved a secure local flow.
- Never return stored credential material from web configuration endpoints.

## Acceptance criteria

- A saved key enables the next turn immediately.
- Cancellation and failed validation do not mutate configuration.
- No supported secret shape appears in output or durable events.
- Config files remain owner-only and updates remain atomic.
- AWS/Vertex profiles store only non-secret references; Ollama requires no key.

## Tests and evidence

Secret success/cancel/paste/Unicode; buffer clearing; permissions; redaction table/fuzz cases; HTTP/event/log scans; provider without key; cloud reference validation; error scrubbing; malicious provider response containing submitted key.

## Out of scope

Building a general OS keychain integration unless separately approved.
