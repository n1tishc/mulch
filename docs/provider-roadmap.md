# Extended provider roadmap (deferred)

Scope changed on 2026-09-13: the owner chose a small, working portfolio release rather than a multi-provider product. This document preserves the earlier plan and all slice links for optional later implementation. Its uses of “release”, “required”, and “blocker” refer to that superseded expanded scope, not the current portfolio release. The current contract is [release readiness](release-readiness.md).

Last reviewed: 2026-09-13

## Current verdict

The five concrete defect fixes below are implemented and their local release gates pass. This is still **not approval to publish the full planned release**: the eight-provider scope below remains unfinished, and the eventual release commit must pass CI, including the new Linux/macOS cancellation matrix. A separate local checkout passed the full verification suite. See [release-fix evidence](release-verification.md) for commands, outcomes, and remaining requirements.

The provider scope for the next release is a curated set commonly supported by coding-agent harnesses: **OpenAI, Anthropic, Google Gemini, OpenRouter, Ollama, Azure OpenAI, Amazon Bedrock, and Google Vertex AI**. Mulch currently has only an OpenAI Chat Completions adapter with configurable API keys and base URLs, so the remaining first-class adapters and authentication flows are release work.

This shortlist is based on the provider matrices published by [OpenCode](https://opencode.ai/v2/docs/providers), [Aider](https://aider.chat/docs/llms.html), [Continue](https://docs.continue.dev/customize/model-providers/overview), and [Cline](https://docs.cline.bot/sdk/model-providers). Those tools consistently support the direct frontier providers, a multi-provider gateway, local models, and the three major enterprise cloud platforms.

## Release blockers

Implementation status as of 2026-09-13:

| Blocker | Current status |
|---|---|
| Startup/provider switching | Fixed; real-daemon regression and packaged-browser smoke pass. |
| Embedded web assets | Rebuilt and staged; tracked-asset guard, embedded HTTP test, and six-archive rehearsal pass. |
| Named-provider model precedence | Fixed; legacy/profile/environment regression passes. |
| Cancellation and verification | Fix implemented; tool and agent stress pass locally, including race testing. Cross-platform CI still required. |
| Live provider gate | Fixed and passed against the default endpoint through the production adapter. |

### 1. Make provider switching usable from every startup state

Previously, the web session manager was created only when the startup provider had an API key. It is now always constructed. A synchronous per-task preparation hook validates credentials and snapshots the selected provider/model/mode before launch. Invalid settings updates do not partially mutate the selection; reported readiness includes actionable missing-key guidance.

Release-ready behavior:

- The server can start without credentials and remain useful as a read-only history viewer.
- Switching to a configured provider with credentials enables task start and resume without restarting Mulch.
- Switching to a provider without credentials disables sending and explains how to add the missing key.
- Each task snapshots provider, model, and mode when it begins. A settings change affects subsequent tasks and does not mutate an in-flight task.
- The UI must never report that execution is ready when the backend cannot start a task.

Required regression coverage:

- Start web mode with an uncredentialed active provider, switch to a credentialed provider, then start and resume a task successfully.
- Switch back to an uncredentialed provider and verify that sending is disabled with an actionable message.
- Change settings while a task is running and verify that the running task keeps its original configuration.

### 2. Ship a complete embedded web build

The replacement Vite assets and index are now staged, together with removal of the old hashes. `scripts/check-web-assets.mjs --tracked` verifies the index references and Git tracking; CI and GoReleaser invoke it. The embedded HTTP regression verifies the actual asset responses rather than accepting an HTML fallback. The final release commit must include these files and the new checker.

Release-ready behavior:

- Run the production UI build from a clean checkout.
- Confirm every file referenced by `internal/server/ui_dist/index.html` is tracked.
- Confirm no stale hashed assets are required by the built page.
- Build the Go binary and open its embedded web interface without a development server.

### 3. Resolve named-provider configuration precedence

The legacy flat `model` no longer overrides an active named profile's first configured model. The regression covers coexisting legacy/profile configuration and an explicit environment override; README documents the precedence.

The documented precedence should be:

1. Explicit launch flag.
2. Environment variable or `.env` value.
3. Active named provider configuration.
4. Legacy flat configuration.
5. Built-in default.

Add migration coverage where legacy flat settings and named profiles coexist.

### 4. Produce a clean release verification run

The reproduced orphan/pipe leak is addressed with group `SIGSTOP`, a 10ms settle interval, group `SIGKILL`, a one-second `Cmd.WaitDelay`, and a bounded post-wait group-exit check. `ESRCH` maps to `os.ErrProcessDone`. Agent bookkeeping now has separate synchronized fake-tool coverage; OS tests retain cleanup checks and use watchdogs rather than a two-second latency assertion. Both 100-run stress loops pass locally. Linux and macOS CI stress jobs are added; a cross-platform pass remains required before publication. See [cancellation timeout research](cancellation-research.md).

Before release, the following must all pass from the intended release commit:

```sh
make verify
make contract
make correctness-test
make web-test
make distribution-test
make release-dry
```

Also verify a clean checkout with:

```sh
cd ui
npm ci
npm test
npm run build
npm run test:e2e
cd ..
go vet ./...
go test -race ./...
git diff --check
```

### 5. Repair and pass the live provider gate

The spike now calls the production `provider.NewOpenAI(...).Stream` adapter and supplies a synthetic session ID on every request. The live text/tool/usage/replay and cancellation subtests pass against the default OpenCode endpoint. An offline stream fixture also checks session/auth headers, fragmented tool arguments, exact token counts, actual model identity, and canonical message replay.

Release-ready behavior:

- Exercise the production provider adapter rather than duplicating its SDK setup in the spike test.
- Give every live request a non-empty synthetic session ID.
- Keep credentials in the environment and redact provider errors.
- Pass the text-plus-tool round trip, usage reporting, prompt cancellation, and same-session follow-up against the documented default endpoint.
- Keep live tests opt-in and clearly identify calls that may consume provider credits.

## Supported provider target

### First-class providers

The release should expose these provider types explicitly rather than representing every service as a base URL for the OpenAI adapter:

| Provider | Required integration behavior |
|---|---|
| OpenAI | API-key authentication, supported OpenAI request/streaming protocol, tool calls, usage accounting, and model discovery or a maintained model catalog. |
| Anthropic | Native Messages API, Anthropic API-key authentication, streaming text/tool calls, stop-reason translation, and usage accounting. |
| Google Gemini | Native Gemini API, Google API-key authentication, streaming content/function calls, safety/error translation, and usage accounting. |
| OpenRouter | OpenRouter endpoint and credentials, full provider/model IDs, optional app-attribution headers, model discovery, streaming tool calls, and provider-routing-compatible request handling. |
| Ollama | Local endpoint support without requiring a fake API key, local model discovery, streaming, tool-capability detection, and a clear unsupported-capability error. |
| Azure OpenAI | Resource or endpoint, deployment name, API version, API-key authentication, and model-to-deployment mapping. Microsoft Entra authentication may follow after the first release if clearly documented. |
| Amazon Bedrock | AWS credential-chain authentication, region selection, model IDs/inference profiles, SigV4 requests through the AWS SDK, streaming tool calls, and usage accounting. |
| Google Vertex AI | Application Default Credentials or service-account authentication, project and location configuration, model selection, streaming tool calls, and usage accounting. |

All eight providers must implement the same Mulch-level contract: system/user/assistant/tool messages, streaming text, parallel tool calls where supported, cancellation, timeouts, provider errors, token usage, context limits, and recorded provider/model identity.

### Custom and secondary providers

Keep one `openai-compatible` provider type for custom gateways and services that implement the expected protocol. DeepSeek, Mistral, xAI, Groq, Together AI, and other vendors do not need separate first-class adapters for this release when they are usable through OpenRouter or the custom OpenAI-compatible configuration.

Compatibility must be described accurately: accepting a base URL does not guarantee that every OpenAI-like service implements streaming, tool calls, usage fields, or message replay in the same way.

### Provider and model listing

- `/provider` lists only configured and usable providers, marks the active provider, and explains why an incomplete provider is unavailable.
- `/model` lists models for the active provider and marks the selected model.
- Use provider model discovery where a stable catalog API exists. When discovery is unavailable, list explicitly configured or maintained catalog entries and label the source.
- Cache discovered models for a bounded period and retain the last successful list during transient provider failures.
- A model may be selected only if its provider can satisfy Mulch's required agent capabilities, especially tool use and the configured context window.

### Provider contract tests

Every first-class adapter needs offline protocol tests using a fake HTTP server or provider SDK seam. Tests must cover authentication without exposing secrets, request translation, streamed text, fragmented tool arguments, multiple tool calls, usage reporting, cancellation, rate limits, malformed responses, and replay of prior assistant/tool messages.

Maintain an opt-in live smoke test for each provider. Live tests should perform one text response, one tool call, cancellation, and a same-conversation follow-up, and must be skipped when credentials are absent.

## Implementation architecture

### Canonical conversation model

Keep `internal/provider.Request`, `Message`, `Block`, `ToolSpec`, `Delta`, and `Response` as the provider-neutral conversation representation. The event log and agent runtime should never store or depend on Anthropic, Gemini, OpenAI, AWS, Azure, or Vertex wire types.

Provider adapters translate between this canonical representation and their external protocol. This is what makes a conversation portable: an assistant tool call originally produced by Anthropic must be reconstructable as valid input when the next turn runs through OpenAI, Gemini, or another configured provider.

Before adding adapters, document and test these canonical invariants:

- A message has one role and an ordered list of blocks.
- Assistant messages may contain text and multiple tool-use blocks.
- Tool-result blocks retain the originating call ID, tool name, output, and error state.
- Adapters preserve block order wherever the provider protocol permits it.
- Stream deltas contain displayable assistant text only; the final response is authoritative for persisted blocks.
- A response must identify the actual model returned by the provider when available.
- Provider-specific stop reasons map to a small documented Mulch vocabulary.
- Unknown additive provider response fields do not break a run.
- Secrets, raw authorization headers, credential files, and cloud tokens never enter canonical messages or events.

### Provider module and seam

Create one deep provider module that hides profile lookup, authentication, adapter construction, model discovery, capability validation, and error normalization. CLI, web, runtime, scoring, summarization, races, evaluation, and one-shot resume should consume the same module instead of constructing providers independently.

The exact Go signatures can evolve, but the external interface should remain approximately this small:

```go
type Resolver interface {
    Resolve(context.Context, Selection) (ResolvedProvider, error)
    Providers(context.Context) ([]ProviderSummary, error)
    Models(context.Context, string) ([]ModelInfo, error)
}

type Selection struct {
    Provider string
    Model    string
    Mode     runtime.Mode
}

type ResolvedProvider struct {
    LLM        provider.LLM
    Provider   string
    Model      string
    JudgeModel string
    Capabilities ModelCapabilities
}
```

The interface includes behavioral requirements beyond its type signature:

- `Resolve` returns an immutable snapshot safe to retain for the duration of a task.
- A settings change never mutates a previously resolved provider.
- Resolution validates credentials, endpoint configuration, model selection, and required capabilities before execution starts.
- `Providers` and `Models` return redacted presentation data only.
- Provider-specific SDK clients, retry policy, credential chains, discovery caching, and endpoint construction remain inside the module.
- Errors are normalized into authentication, rate-limit, unavailable, invalid-model, unsupported-capability, timeout, cancellation, and malformed-response categories while retaining a redacted provider message for diagnosis.

The existing `provider.LLM` interface remains the runtime seam for inference. A registry/factory inside the provider module supplies the correct adapter. Avoid provider switches in `internal/cli`, `internal/runtime`, or UI code; adding a ninth provider should primarily add one adapter, registration metadata, configuration validation, and contract fixtures.

### Settings module

Move configuration parsing, precedence, validation, redaction, atomic persistence, and migration behind a settings module. Callers should not assemble effective settings by repeatedly querying environment variables.

The settings module should expose:

- One immutable effective snapshot.
- A redacted list of configured profiles for presentation.
- Atomic updates for active provider, default model, mode, endpoints, and secrets.
- A single documented precedence implementation.
- Schema-version migration from the current flat string map.
- A subscription or callback used by the local web server to publish updated readiness.

Recommended versioned configuration shape:

```json
{
  "version": 2,
  "active_provider": "anthropic-personal",
  "defaults": {
    "mode": "intervention"
  },
  "providers": {
    "anthropic-personal": {
      "type": "anthropic",
      "default_model": "configured-model-id",
      "models": ["configured-model-id"],
      "api_key": "redacted-on-output"
    },
    "local": {
      "type": "ollama",
      "endpoint": "http://127.0.0.1:11434"
    }
  }
}
```

Provider-specific fields belong inside their profile. Expected examples include Azure resource/deployment/API version, AWS profile/region, Vertex project/location/credential reference, and optional OpenRouter attribution values. Do not force these into generic API-key/base-URL fields.

Configuration migration requirements:

- Read version 1 flat keys without requiring manual edits.
- Preserve environment and explicit flag precedence.
- Convert legacy values in memory first; write version 2 only after an explicit mutation or migration command.
- Create a backup before the first on-disk schema rewrite.
- Never include secret values in migration logs or error messages.
- Reject partially migrated or structurally invalid profiles with a profile-specific actionable error.
- Preserve owner-only file permissions and atomic rename behavior.

### Runtime selection and lifecycle

Represent the active provider, model, judge model, and mode as one selection. Starting or resuming a task resolves that selection once and passes the immutable result into the runtime.

The same resolved adapter must be used consistently for:

- Primary agent calls.
- Coherence judging unless a separate valid judge selection is explicitly configured.
- Context summarization.
- Race candidates.
- One-shot `run` and `resume` commands.
- Interactive terminal turns.
- Web-started and web-resumed turns.
- Evaluations.

If the judge uses a different provider or model, model it as a second explicit selection and resolve it independently. Never carry a judge model name across a provider change without validating that the new provider supports it.

Readiness is derived state, not a mutable boolean. The backend is ready only when the selected profile resolves, the chosen model satisfies required capabilities, and execution control exists. The web server should always construct its session manager; an unresolved selection should cause a clear task-start error rather than remove the control path for the lifetime of the process.

### Model metadata and capabilities

Normalize provider catalogs into a stable `ModelInfo` presentation type with at least:

```text
provider, id, display name, source, context window,
maximum output, tool support, streaming support,
parallel-tool support, availability, and unavailability reason
```

Do not hard-code fast-changing model IDs into UI code. Discovery order should be:

1. A provider catalog endpoint or SDK when stable and authenticated.
2. Explicit models in the user's provider profile.
3. A maintained built-in fallback catalog with a documented update date.

Catalog failure must not break an already configured model. Use a bounded cache, retain the last successful result, deduplicate by provider plus model ID, sort deterministically, and identify whether each result was discovered, configured, cached, or built in.

Mulch currently requires text streaming and tool calling for coding tasks. Models without those capabilities may appear in diagnostics but must be disabled for agent selection with an explanation.

### Event-log compatibility

Increment `run.config` metadata to a new documented version while keeping old events readable. Record only stable, non-secret execution identity:

```json
{
  "version": 2,
  "provider_profile": "anthropic-personal",
  "provider_type": "anthropic",
  "model": "configured-model-id",
  "judge_provider_type": "anthropic",
  "judge_model": "configured-model-id",
  "mode": "intervention"
}
```

Do not record API keys, cloud tokens, credential paths, raw authorization errors, or complete provider configuration. Replay and the inspector must continue to render version 1 events. Resuming an old session uses current resolved settings for the next turn while retaining the originally recorded configuration for historical turns.

## Implementation slice index

Each slice is an independently reviewable implementation document. A slice is complete only when its user-visible behavior, backend path, documentation, automated tests, and completion evidence land together. Update the status inside the slice file as work begins; keep this table as the canonical order.

| Slice | Document | Outcome | Depends on |
|---:|---|---|---|
| 0 | [Contracts and terminology](release-slices/00-contracts.md) | Freeze provider, persistence, command, and session semantics. | — |
| 1 | [Versioned settings and migration](release-slices/01-settings-migration.md) | Safely support typed provider profiles and legacy configurations. | 0 |
| 2 | [Provider resolver and contract harness](release-slices/02-provider-resolver.md) | Establish one deep provider seam and shared adapter verification. | 0–1 |
| 3 | [OpenAI and OpenAI-compatible](release-slices/03-openai-compatible.md) | Move existing behavior through the new architecture. | 2 |
| 4 | [Runtime switching and readiness](release-slices/04-runtime-switching.md) | Make next-turn switching truthful and usable from every startup state. | 1–3 |
| 5 | [Provider/model discovery UX](release-slices/05-discovery-ux.md) | Power terminal and web choices from one normalized catalog. | 4 |
| 6 | [Credential and secret safety](release-slices/06-credential-safety.md) | Configure every provider without leaking secrets. | 1–5 |
| 7 | [Anthropic](release-slices/07-anthropic.md) | Add native Anthropic support end to end. | 2, 4–6 |
| 8 | [Google Gemini](release-slices/08-gemini.md) | Add native Gemini support end to end. | 2, 4–6 |
| 9 | [OpenRouter](release-slices/09-openrouter.md) | Add routed multi-vendor model access end to end. | 2, 4–6 |
| 10 | [Ollama](release-slices/10-ollama.md) | Add credentialless local-model support end to end. | 2, 4–6 |
| 11 | [Azure OpenAI](release-slices/11-azure-openai.md) | Add deployment-based Azure support end to end. | 2, 4–6 |
| 12 | [Amazon Bedrock](release-slices/12-bedrock.md) | Add AWS credential-chain support end to end. | 2, 4–6 |
| 13 | [Google Vertex AI](release-slices/13-vertex-ai.md) | Add ADC-based Vertex support end to end. | 2, 4–6 |
| 14 | [Command and presentation parity](release-slices/14-command-ui-parity.md) | Finish consistent CLI/web behavior and presentation. | 4–6; evolves with providers |
| 15 | [Packaging and release rehearsal](release-slices/15-release-packaging.md) | Verify and package the exact release commit. | All release-scoped slices |

When starting implementation, open the selected slice and treat its **Scope**, **Acceptance criteria**, **Tests and evidence**, and **Out of scope** sections as the issue contract. Shared architecture and global release gates remain in this main document.
## Slice dependency and parallelization guide

```text
Slice 0: contracts
   |
Slice 1: settings
   |
Slice 2: resolver + contract harness
   |
Slice 3: OpenAI reference adapter
   |
Slice 4: runtime switching/readiness
   |
+-- Slice 5: discovery UX -- Slice 6: credential safety --+
|                                                       |
+-- Slices 7–10: Anthropic / Gemini / OpenRouter / Ollama (parallel)
|
+-- Slices 11–13: Azure / Bedrock / Vertex (parallel)
|
+-- Slice 14: presentation and command parity
   |
Slice 15: packaging and release rehearsal
```

Slice 14 does not need to wait for every provider implementation; its common states can land after Slice 6, with provider-specific states added inside each provider slice. Enterprise adapters can begin once the resolver, settings schema, contract harness, runtime snapshot, discovery surface, and credential policy are stable.

## Per-slice pull request template

Use this checklist when turning a slice into an implementation issue or pull request:

- **User outcome:** one sentence describing what a user can do after merge.
- **In scope:** explicit behavior, entry points, providers, and platforms.
- **Out of scope:** deferred authentication methods, capabilities, or UI work.
- **Interface changed:** provider resolver, settings, runtime selection, HTTP shape, events, or none.
- **Migration:** old config/events/sessions/packages affected and rollback behavior.
- **Security:** credentials handled, redaction points, local/network exposure, and threat assumptions.
- **Failure states:** authentication, rate limit, unavailable service, invalid model, missing capability, timeout, cancellation, and malformed response.
- **Tests:** offline contract, unit/integration, CLI, web E2E, live smoke, and clean-checkout command.
- **Documentation:** help, README, workflow docs, config examples, and release notes.
- **Evidence:** exact commands and results; screenshots only for visual changes.
- **Definition of done:** every acceptance statement demonstrably satisfied.

## Product decisions required before release

### Settings persistence

`/provider`, `/model`, and `/mode` currently change the running process, while `/api-key` writes configuration to disk. Decide on one clear contract and apply it consistently:

- Persist slash-command changes as the defaults for future launches; or
- Label them as session/runtime-only changes and provide an explicit persistence command.

The recommended behavior is to persist provider and model choices, while treating mode as a conversation default that can be changed per session. In all cases, the UI should state whether a change is temporary or saved.

### API-key entry in the browser

The terminal supports hidden `/api-key` entry. The browser currently redirects users to local terminal configuration. Before release, either:

- Implement a local-only, redacted browser credential flow with no key returned by APIs, logged, stored in browser storage, or recorded in events; or
- Keep browser key entry intentionally unsupported and document that `/api-key` is terminal-only.

The second option is acceptable for the curated provider release, provided the browser message and documentation are unambiguous.

### Judge model after a provider switch

Switching providers must not silently keep a judge model that the new provider cannot serve. On provider change, either select the new profile's configured judge model, default the judge to the selected primary model, or disable judge-dependent modes with a clear explanation.

## Required functional coverage

The release suite must cover the following behavior in addition to existing tests:

| Area | Required checks |
|---|---|
| `/provider` | Bare command lists configured profiles and marks the active one; valid switching preserves conversation history; unknown profiles fail clearly. |
| `/model` | Bare command lists models for the active provider and marks the selected one; switching affects the next turn without creating a session. |
| `/mode` | Valid modes switch subsequent turns; invalid values are rejected; recorded run configuration matches the mode used. |
| `/api-key` | Input is masked, cancellation does not write a value, successful entry writes an owner-only config file, and output never contains the secret. |
| `/new` | Active transcript, draft, pending request identity, and active session selection are cleared; saved history remains available through session navigation. |
| Provider/model continuity | Multiple turns can use different configured providers and models—including providers with different native protocols—while retaining the same conversation history. Each turn records its actual provider and model. |
| Provider adapters | Each of the eight first-class providers passes the shared offline contract suite and its opt-in live smoke test before publication. |
| Configuration migration | Legacy flat configuration remains usable and does not unexpectedly override an explicitly selected named profile. |
| Web readiness | Reported readiness always matches the backend's ability to start and resume a task. |
| Presentation | User and assistant turns remain visually distinct; Markdown, code blocks, links, tables, and long content render safely on desktop and mobile. |

Tests must assert behavior at both the UI and backend/event-log levels. A status notice alone is not evidence that the runtime actually used the selected provider, model, or mode.

## Security and privacy gates

- Configuration files containing credentials are created with owner-only permissions on Unix.
- API keys are redacted from `config show`, errors, logs, browser responses, event payloads, screenshots, and test artifacts.
- Provider base URLs reject embedded credentials and unsupported URL schemes.
- Rendered Markdown cannot execute raw HTML or unsafe links.
- Runtime configuration endpoints remain loopback-only by default and do not expose secrets.
- Existing saved sessions and project files are not deleted by `/new`.

## Packaging and release hygiene

- The release commit contains only intended source, documentation, generated web assets, and packaging changes.
- `.DS_Store`, evaluation scratch directories, logs, local databases, API keys, and other machine-local artifacts are excluded.
- Version output, archive names, checksums, npm wrapper version, release notes, and tag all agree.
- All six configured native archives build successfully.
- Installation is tested through both the checksum-verified shell installer and npm/pnpm packages.
- A packaged binary passes a smoke test for terminal startup, web startup, provider/model switching, one task, one follow-up, `/new`, and session reopening.

## Definition of done

The release is ready when:

- All five release blockers above have implementation and verification evidence from the intended release commit.
- All eight first-class providers pass the shared adapter contract, and the provider support claim matches the implemented protocol support.
- Persistence, browser API-key handling, and judge-model behavior are explicitly decided and documented.
- Required regression tests exist and pass.
- The full race, web, contract, correctness, distribution, and dry-release gates pass from a clean checkout.
- Embedded web assets and packaging outputs are complete and tracked.
- No credentials or local artifacts are present in the release diff or archives.
- A final packaged-binary smoke test passes on supported platforms.
