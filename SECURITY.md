# Security and safe use

Mulch is a local coding harness, not a hosted service or a multi-user security boundary. Use disposable repositories and non-sensitive test data until you have inspected its behavior for your environment.

## Operating boundaries

- Keep the browser server on loopback. Do not expose it through a public bind address, port forwarding, tunnel, or reverse proxy. Its origin checks are not user authentication.
- Tools execute model-selected operations and may modify files. Inspect changes independently. Cancellation stops work; it does not roll back completed edits.
- Linux shell execution needs Bubblewrap and compatible host sandbox support. Windows Bash execution is unsupported. Do not disable containment to make a task run.
- API keys are stored in local plaintext configuration with owner-only permissions on Unix. Enter them using the hidden terminal prompt or stdin, never in task messages. `config show` redacts key values.
- Configured OpenAI adapter keys are redacted from provider errors before those errors reach the CLI or event log. This is not general-purpose content redaction: prompts, model text, tool output and files can still contain sensitive information.
- SQLite sessions, evaluation artifacts, browser drafts and screenshots can contain project content. Treat exports and backups as sensitive. `/new` does not erase saved history.
- An abrupt process crash may leave a persisted running lease. Verify its owner is stopped and start a new conversation; do not run competing owners against the same session.

## Reporting

Do not put credentials, private source code, full databases, or exploitable sensitive details in a public issue. Contact the maintainer through an available private channel on their GitHub profile first. If GitHub private vulnerability reporting is enabled for the repository, use that channel. There is no guaranteed response-time commitment for this portfolio project.

For a non-sensitive bug report, provide the Mulch version, OS/architecture, redacted reproduction steps, expected behavior and actual behavior. Reproduce with a synthetic key and disposable workspace where possible.

## Release integrity

Use versioned artifacts and verify their SHA-256 values against the release checksums. Checksums detect corruption relative to the release metadata; they are not independent artifact signatures. No npm or GitHub artifact is public until explicitly published by the maintainer.
