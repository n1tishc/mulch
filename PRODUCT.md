# Mulch product context

Mulch is a local coding harness for developers working in repositories. Its native Go binary supports an interactive terminal (`mulch`), a browser coding workspace (`mulch web`), and one-shot tasks. The terminal and browser share the agent runtime and durable SQLite session log. The browser is an operating interface for real repository tasks, not a separate agent or a hosted service.

The primary workflow is: choose a local workspace, send a task, read streamed assistant/tool output, inspect edits and evidence, then continue the same conversation. Preserve drafts and history, make cancellation explicit, and distinguish daemon-owned work from tasks running in another terminal. Browser closure does not cancel backend work. Stopping does not undo completed file edits.

The differentiator under investigation is auditable context repair: recorded context-health scores, bounded repair decisions, exact context occurrences, and isolated candidate comparisons. Health and candidate selection are not independent correctness grades. Ordinary sessions remain ungraded. Existing pilot evidence has not established a correctness advantage; never describe the harness as universally better or its repairs as proven beneficial.

The approved browser direction is conversation first, repair inspection alongside it. Preserve the established warm neutral and olive identity, restrained interface typography, and serif welcome treatment. Prioritize readable text, predictable controls, truthful states, keyboard access, and narrow-screen drawers. No visual rebrand or imagery is needed.

Provider secrets stay in the backend. `mulch config` saves provider settings in a private local JSON file; environment and launch flags override them. The browser shows effective non-secret configuration and historical run metadata; old missing evidence remains unknown. Shared event history supports read-only terminal inspection through `/web` or `/inspect` without transferring execution ownership. Browser workspaces organize chats by actual project directory. Confirmed deletion removes a stopped conversation and its candidate branches, never project files. The trace tree presents recorded execution rather than private model reasoning.

Cloud hosting, OAuth, a plugin marketplace, an embedded shell/IDE, general workspace browsing, automatic rollback/crash recovery, and a benchmark dashboard are outside this iteration. Packaging is prepared locally; this work does not publish a release. Deterministic providers and disposable repositories validate the interface without paid model calls. Live correctness comparisons remain a separate evaluation project.

Implementation contract: [web interface plan](docs/web-interface-plan.md). User workflow and limitations: [web workflow](docs/web-workflow.md).
