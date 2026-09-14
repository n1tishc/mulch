# Mulch 0.1.0 — draft release notes

Status: public GitHub release. The `v0.1.0` tag matches the package metadata and its artifacts pass the release checks. The npm packages are attached for inspection but are not published to the npm registry.

Mulch is a small Go coding harness with terminal and browser interfaces, durable conversations, and auditable context-repair experiments.

## Included

- A shared streaming agent runtime with file/shell tools, cancellation and same-session follow-ups.
- An embedded browser workspace with Markdown, tool details, saved history, reconnect and execution traces.
- Terminal slash commands for configured providers, models, modes, credentials and fresh conversations.
- Named OpenAI-compatible endpoint profiles; next-task settings snapshots preserve an in-flight task's configuration.
- Append-only SQLite events, replay, linked scoring/repair evidence and experimental isolated candidate comparisons.
- Reproducible offline tests, browser acceptance tests, race/cancellation checks, installer checks and six snapshot archive targets.
- A [no-key synthetic walkthrough](portfolio-demo.md) for exploring the mechanics before configuring a live model.

## Fixes in this release

Web execution can become ready after switching away from an uncredentialed startup profile. Named-profile models take precedence over legacy flat defaults. Task cancellation bounds inherited-pipe waits and verifies process-group cleanup. Live-provider checks exercise the production adapter, and packaging rejects missing embedded web assets.

Production OpenAI-compatible errors redact the configured API key before reaching output or saved traces, including when an upstream authentication error echoes that key.

## Requirements and limitations

This is a source-visible release without a reuse license for the project code, as selected by its owner. Third-party dependencies retain their own licenses. See [security and safe use](../SECURITY.md) before running tasks.

Use macOS or Linux for coding tasks; Linux shell execution requires Bubblewrap and host sandbox support. Windows shell execution is unsupported. Configure a compatible Chat Completions endpoint and a streaming, tool-capable model for live coding; native Anthropic, Gemini and enterprise-cloud adapters are not included.

Slash selection changes are runtime-only; persist defaults with `mulch config`. API keys are local plaintext with owner-only permissions on Unix, entered in the terminal and redacted on display. Keep the web server local. Tasks may change files, and cancellation does not undo those edits. `/new` preserves saved history.

Scoring and context repair remain experiments. The current pilot does not demonstrate a correctness advantage. Synthetic demo records illustrate architecture, not real model performance.

## Install and verify

Use the checksummed GitHub archive installer or the [source build](../README.md#install). The optional npm wrapper described in [distribution](releases.md) is staged but not registry-published. See the [verification report](release-verification.md) for exact local and CI evidence; cross-compilation alone does not establish native execution on all six targets.

## Rollback

Before replacing a binary, retain its versioned archive and checksums. Stop Mulch before backing up its database and local configuration; keep credential backups private. Install the previous reviewed binary to a separate versioned path and check `mulch version` before use. This candidate introduces no database schema migration, but do not overwrite or delete saved history to roll back an executable. The checksum-rejection installer test verifies that a rejected download leaves the installed binary intact.
