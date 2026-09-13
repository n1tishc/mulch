# Slice 7 — Anthropic end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slice 4](04-runtime-switching.md), [Slice 5](05-discovery-ux.md), [Slice 6](06-credential-safety.md)

## User outcome

A user can configure Anthropic, select a Claude model, run tools, switch away and back, and continue one conversation.

## Scope

- Native Messages API and version headers.
- System-message and ordered content-block translation.
- Streaming event assembly for text and tool input.
- Tool-use/result replay, multiple tool calls, stop reasons, usage, retries/errors, timeout, and cancellation.
- Model discovery or maintained/configured catalog fallback.
- Valid Anthropic defaults for primary, judge, summary, and race calls.

## Acceptance criteria

- Terminal and web complete a tool-using task and follow-up.
- OpenAI-originated history continues through Anthropic and returns to OpenAI.
- Mixed text/tool blocks and error tool results preserve canonical order and identity.
- Authentication, overload, rate-limit, and invalid-model failures are distinct and redacted.

## Tests and evidence

Shared contract; Anthropic streaming fixtures; fragmented tool input; mixed/multiple blocks; error results; malformed event order; cross-provider replay; model fallback; cancellation; opt-in live text/tool/follow-up/cancel smoke.

## Out of scope

Anthropic-specific features not represented by Mulch's canonical conversation model.
