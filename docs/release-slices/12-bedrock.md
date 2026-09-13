# Slice 12 — Amazon Bedrock end to end

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 2](02-provider-resolver.md), [Slices 4–6](../release-readiness.md#implementation-slice-index)

## User outcome

AWS users run Mulch through the standard credential chain without storing AWS secrets in Mulch configuration.

## Scope

- AWS SDK and default credential-chain integration.
- Profile and region selection; model and inference-profile IDs.
- Bedrock streaming/Converse translation, tool configuration/results, usage, throttling, permission errors, timeout, and cancellation.
- Model listing where permissions allow, with configured fallback otherwise.
- Store only non-secret AWS profile/region references.

## Acceptance criteria

- Environment, shared-config profile, and workload credentials work through the SDK chain.
- Missing region, credentials, model access, and permissions have distinct redacted errors.
- Lack of catalog permission does not break an explicitly configured model.
- Cross-provider canonical history works in both directions.

## Tests and evidence

Shared contract through an injected Bedrock client seam; credential/region resolution; event-stream fixtures; tool replay; throttling/access-denied mapping; catalog-permission fallback; cancellation; cross-provider replay; opt-in live smoke.

## Out of scope

Storing AWS access keys or temporary session tokens in Mulch settings.
