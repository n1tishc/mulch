# Slice 13 — Google Vertex AI end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

GCP users run Vertex-hosted models through Application Default Credentials with explicit project and location selection.

## Scope

- Google SDK, ADC, and service-account file reference support.
- Project/location precedence and regional endpoint construction.
- Gemini-on-Vertex message, stream, function, result, usage, error, timeout, and cancellation mapping.
- Model discovery or configured/catalog fallback.
- Prevent credential JSON, access tokens, and credential paths from entering output or events.

## Acceptance criteria

- ADC works without copying tokens into Mulch.
- Missing project/location, unavailable region/model, permission, and quota failures are distinct.
- Configuration clearly states the selected project and region without exposing credentials.
- Cross-provider canonical history works in both directions.

## Tests and evidence

Shared contract through an injected Vertex client seam; ADC/reference paths; project/location precedence; regional endpoints; permission/quota errors; function streams; cross-provider replay; cancellation; opt-in live smoke.

## Out of scope

Managing Google identities, creating service accounts, or copying credential JSON into Mulch config.
