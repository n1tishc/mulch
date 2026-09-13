# Slice 2 — Provider resolver and shared contract harness

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 0](00-contracts.md), [Slice 1](01-settings-migration.md)

## User outcome

Every Mulch entry point resolves providers consistently, and new adapters can be added without editing the CLI, server, or runtime dispatch logic.

## Scope

- Implement the resolver and adapter registry described in the main architecture.
- Resolve immutable provider/model/judge/mode snapshots.
- Normalize model metadata, capability validation, errors, stop reasons, and usage.
- Route chat, run, resume, web, scoring, summarization, races, and evaluation through the resolver.
- Keep SDK clients, authentication, retries, endpoint construction, and discovery caches inside provider implementations.
- Build reusable fake-stream fixtures for text, fragmented tool JSON, multiple tools, usage, cancellation, rate limits, malformed streams, and replay.

## Acceptance criteria

- No runtime caller receives or combines an API key and base URL.
- A fake second adapter registers without adding provider conditionals outside the provider module.
- Settings changed after resolution cannot alter an in-flight resolved provider.
- Unsupported capabilities fail before an agent task starts.
- All externally visible errors are categorized and redacted.

## Tests and evidence

Resolver selection; unknown/incomplete profile; invalid model; capability rejection; immutable snapshot under concurrent changes; error normalization; secret scrubbing; contract harness self-test with a deliberately broken adapter.

## Out of scope

Production adapters beyond the reference OpenAI slice.
