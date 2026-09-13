# Slice 14 — Conversation presentation and slash-command parity

[Back to extended roadmap](../provider-roadmap.md#implementation-slice-index) · [Current release scope](../release-readiness.md)

This is deferred roadmap work unless explicitly included in the current portfolio release contract.

Status: not started
Depends on: [Slices 4–6](../release-readiness.md#implementation-slice-index); evolves with provider slices

## User outcome

CLI and web remain clear, accessible, and behaviorally consistent across providers and session boundaries.

## Scope

- Finish structured Markdown rendering and visual role separation.
- Safely render headings, lists, tables, links, code, long lines, and tool output.
- Align `/new`, `/provider`, `/model`, `/mode`, `/api-key`, `/help`, `/status`, and command-palette wording/state transitions.
- Distinguish active settings from historical per-turn provider/model/mode.
- Preserve drafts and a clear recovery path after errors.
- Keep terminal, desktop, and mobile layouts usable with long/unicode identities.

## `/new` acceptance contract

Clear active transcript, draft, pending submission identity, queued input, selected session, transient notice/error, and inspector selection. Create a new session only when the next task is sent. Retain saved sessions, branches, and project files.

## Acceptance criteria

- Equivalent commands produce equivalent state changes in CLI and web.
- User and assistant output are immediately distinguishable.
- Raw Markdown markers do not replace intended formatting.
- Historical identity is not presented as the currently active selection.
- All command results and failures are accessible status or alert messages.

## Tests and evidence

Terminal semantic/golden tests; Playwright desktop/mobile flows; Markdown/HTML injection; unsafe links; long/unicode identities; empty/loading/error states; keyboard-only navigation; screen-reader labels/status; retry identity; exhaustive `/new` cleanup.

## Out of scope

A broad visual redesign unrelated to conversation clarity or provider commands.
