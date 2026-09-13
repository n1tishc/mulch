# Slice 3 — OpenAI and custom OpenAI-compatible provider

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md)

## User outcome

Existing users keep working through the new provider architecture and can select either first-class OpenAI or a custom compatible endpoint.

## Scope

- Move current OpenAI construction behind the resolver.
- Separate `openai` defaults from user-defined `openai-compatible` endpoints.
- Preserve text streaming, ordered mixed blocks, multiple tool calls, tool-result replay, usage, cancellation, no-retry behavior, and session attribution.
- Add stable model discovery when supported, falling back to configured models.
- Normalize missing usage, malformed streams, invalid models, authentication failures, and rate limits.
- Clearly report which custom-endpoint capabilities were not verified.

## Acceptance criteria

- Existing credentials migrate without user action.
- CLI and web can select both profile types and continue the same conversation.
- `/new` remains the only clean-session boundary.
- A custom endpoint that lacks tool support is rejected before task execution.

## Tests and evidence

Shared adapter contract; existing provider spike; migration; custom base URL; optional usage; fragmented/multiple tool calls; invalid model; cross-profile switching; live OpenAI smoke; one documented compatible-endpoint smoke.

## Out of scope

Guaranteeing compatibility with every service that resembles the OpenAI protocol.
