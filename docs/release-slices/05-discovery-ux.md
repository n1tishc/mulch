# Slice 5 — Provider and model discovery UX

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slice 4](04-runtime-switching.md)

## User outcome

`/provider` and `/model` expose useful, trustworthy choices in terminal and web.

## Scope

- Expose only redacted provider summaries and normalized model metadata.
- Implement discovery caching, deterministic sorting, deduplication, explicit refresh, configured fallback, and stale-state presentation.
- Mark active, incomplete, disabled, unavailable, configured-only, cached, discovered, and built-in entries.
- Back terminal commands, web slash commands, and command palette with the same data.
- Display provider/model identity without exposing endpoints or credentials.
- Preserve an already configured model during a transient catalog failure.

## Acceptance criteria

- Bare `/provider` lists configured profiles and explains unusable ones.
- Bare `/model` lists the active provider's models and capability status.
- Selection feedback states whether the choice was persisted.
- Models missing required streaming/tool capabilities cannot start coding tasks.
- Catalog outages use the last successful or explicitly configured result.

## Tests and evidence

Empty/loading/error/stale/large catalogs; active markers; cache expiry and refresh; deduplication; keyboard and screen-reader interaction; mobile layout; unknown selections; long/unicode model IDs; unsupported-capability message.

## Out of scope

Maintaining an exhaustive global model database.
