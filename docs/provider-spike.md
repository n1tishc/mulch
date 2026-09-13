# Provider spike

Ticket [#1](https://github.com/n1tishc/mulch/issues/1) gates Mulch's provider adapter on a live, single-step streaming probe. The probe is isolated behind the `spike` build tag because it makes billable network requests.

Run it with:

```sh
MULCH_PROVIDER_API_KEY=... go test -tags=spike -run TestLiveProviderGate -v ./internal/provider
```

The defaults target OpenCode Go's OpenAI-compatible endpoint:

```text
MULCH_PROVIDER_BASE_URL=https://opencode.ai/zen/go/v1
MULCH_MODEL=glm-5.3-flash
```

Both values can be overridden, so the gate is not coupled to OpenCode Go or to a provider-owned environment variable. `MULCH_PROVIDER_BASE_URL` is the API root; the OpenAI SDK appends `/chat/completions`.

The test proves that the selected provider adapter:

- returns assistant text and a provider-assigned tool-call identifier without executing the supplied tool;
- reports positive input and output token counts through the production adapter (exact fixture counts are checked offline);
- accepts the exact assistant response blocks and tool-call identifier on the next request; and
- returns `context.Canceled` within a two-second watchdog after streamed text begins and the request is cancelled.

The gate calls `provider.NewOpenAI(...).Stream`, supplies a synthetic session ID on every request, and replays Mulch's canonical messages. This exercises the production `x-opencode-session` header and conversion code. Provider errors redact the configured API key. The cancellation watchdog is a hang detector, not a latency service-level objective.

## Decision

GoAI `v0.10.0` passed content, tool-call identifier, usage, and replay checks against OpenCode Go, but failed the cancellation contract: cancelling the stream returned no error. Per the gate's fail-fast rule, GoAI was rejected.

The fallback uses the official OpenAI Go SDK `github.com/openai/openai-go/v3` pinned at `v3.47.0`, configured against the same OpenAI-compatible endpoint. OpenCode Go does not advertise an embedding endpoint, so embeddings remain separately configured; Voyage will use a standard-library HTTP adapter and therefore adds no dependency.
