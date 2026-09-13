# Slice 10 — Ollama end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

Local models work without cloud credentials, with capability problems detected before a task starts.

## Scope

- Default and custom local endpoints without a required API key.
- Installed-model discovery and capability inspection.
- Streaming text/tool mapping, usage when available, cancellation, connection diagnostics, and timeout guidance.
- Choose native or OpenAI-compatible Ollama protocol based on full contract coverage and lower interface complexity.
- Clearly separate daemon unavailable, empty catalog, model absent, and model lacks tools.

## Acceptance criteria

- A fresh local profile discovers installed models without credentials.
- A capable model completes a tool task and follow-up in CLI and web.
- Unsupported models remain visible but disabled with a reason.
- Slow local inference cancels promptly without orphaning tool processes.

## Tests and evidence

Shared contract; no-auth path; unreachable daemon; empty catalog; capability filtering; partial/slow streams; custom endpoint; same-conversation cloud/local switching; opt-in local smoke.

## Out of scope

Installing, downloading, or managing Ollama models on the user's behalf.
