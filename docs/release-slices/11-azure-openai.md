# Slice 11 — Azure OpenAI end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

Azure users can map Mulch model choices to deployments and run the standard agent workflow.

## Scope

- Resource or endpoint validation, deployment mapping, and API version.
- API-key authentication and Azure request URL construction.
- Streaming text/tools, usage, error envelopes, timeout, cancellation, and deployment-oriented model presentation.
- Support multiple deployments in one profile.
- Keep Microsoft Entra authentication explicitly deferred unless implemented and tested in this slice.

## Acceptance criteria

- `/model` presents stable names mapped to deployments.
- Missing deployment, endpoint, or API version fails before execution.
- Two deployments can be selected without separate credentials.
- Direct OpenAI history resumes through Azure and back.

## Tests and evidence

Shared contract; URL/API-version fixtures; model-to-deployment mapping; multiple deployments; Azure error envelopes; secret redaction; cross-provider replay; cancellation; opt-in live smoke.

## Out of scope

Microsoft Entra ID unless the slice scope is explicitly expanded before implementation.
