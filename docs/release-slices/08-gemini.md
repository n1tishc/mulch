# Slice 8 — Google Gemini end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

A user can configure Gemini, discover a capable model, use function calls, and continue cross-provider history.

## Scope

- Native Gemini request/content and system-instruction mapping.
- Streaming assembly for text and function calls.
- Function declarations, calls, results, multiple parts, finish reasons, usage, timeout, and cancellation.
- Safety, blocked response, missing candidate, authentication, quota, and malformed response handling.
- Model discovery and capability filtering.

## Acceptance criteria

- Terminal and web complete a tool-using task and follow-up.
- Blocked content is a clear provider outcome, never false success.
- Anthropic/OpenAI-originated tool history translates to Gemini and back.
- Primary, judge, summary, and race calls resolve compatible models.

## Tests and evidence

Shared contract; streaming function-call fixtures; multiple parts/tools; safety blocks; missing candidates; finish reasons; usage; quota/auth errors; cross-provider replay; cancellation; opt-in live smoke.

## Out of scope

Gemini media and server-side tools not represented by the current canonical model.
