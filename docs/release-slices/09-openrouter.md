# Slice 9 — OpenRouter end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

Users can access routed models through complete `provider/model` IDs without creating a profile for every upstream vendor.

## Scope

- Canonical `OPENROUTER_API_KEY`, endpoint defaults, and `/models` discovery.
- Preserve full model slugs and normalized capabilities.
- Support optional app-attribution metadata and provider-routing-compatible request fields where exposed.
- Translate streaming tools, usage, authentication, privacy/routing, rate-limit, and upstream-provider errors.
- Tolerate additive response and routing metadata.

## Acceptance criteria

- Models from different upstream vendors are selectable under one profile.
- Model IDs are never truncated or confused with Mulch profile names.
- Catalog and request failures remain actionable and redacted.
- Switching between OpenRouter models preserves the conversation and records the actual selection.

## Tests and evidence

Shared contract; catalog fixtures and pagination if applicable; full-slug selection; optional headers; routing/privacy errors; additive metadata; fallback tolerance; cancellation; opt-in live smoke across at least two upstream vendors.

## Out of scope

Guaranteeing the behavior of every upstream model exposed by OpenRouter.
