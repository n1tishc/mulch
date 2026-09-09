---
version: 1
slug: "ui-src-app-tsx"
primary_target: "ui/src/App.tsx"
related_targets: ["ui/src/components","ui/src/workspace.css","ui/src/workspace-responsive.css","internal/cli/chat_terminal.go"]
---

# Browser workspace

Mode: Operate. Target: `ui/src/App.tsx`, its components, and workspace styles.

The approved scope and workflow remain in `docs/web-interface-plan.md`. This brief records the implemented surface and its inherited visual authority; it introduces no new product direction.

## Direction contract

THESIS: Complete repository tasks through readable conversation, with repair evidence alongside; a dashboard does not lead.

OWN-WORLD: Inherited warm neutral and olive surfaces, fine borders, compact controls, serif welcome, authored SVG icons.

STORY: Group conversations by project directory, send a task, follow its execution tree, inspect causal evidence, continue or delete saved history.

FIRST VIEWPORT: Desktop workspace-folder sidebar, flexible conversation and bottom composer; the inspector opens onto a turn tree with linked tools, scores and repairs. Narrow screens use modal drawers. Terminal retains a compact header, bounded transcript and stable prompt area.

FORM: Approved conversation-first structure from `docs/web-interface-plan.md`. No ranked concept list or seed key: the precise request inherited the established world; concept seeding was inapplicable.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Interaction and state

The signature interaction opens a conversation's repair notice directly onto its selected evidence above the decision list, moving focus to that detail. Context occurrences and candidate evidence retain recorded lineage and unknown states. Health is not correctness; missing dimensions are nonnumeric.

At 1199px and below, the inspector uses a native modal dialog; at 700px and below, sessions do too. Dialogs support Escape, labelled close controls, and focus return. The composer remains available below the conversation. Scrolling into older content suspends following until “Jump to latest.” Live completion announcements remain silent during historical replay.

Motion is restrained: no decorative animation is required, disclosure icons change orientation, and reduced-motion preferences suppress animation, transitions, and smooth scrolling.

Directory groups show actual project paths and conversation counts; users can add an existing directory and start a chat within it. Deleting a stopped conversation requires confirmation and removes its saved history and candidate branches, leaving project files intact.

The inspector defaults to the execution trace. Native turn disclosures expose recorded model calls, tools, scores, and repairs; the latest turn is expanded. Sequence-ID links connect repairs to their recorded score and context change, and candidate links open branch history. Older turns and long step lists load incrementally. Unrecorded links remain labelled unknown; turn grouping does not assert causal order across asynchronous scorers or reveal private model reasoning.

Terminal `/web` and `/inspect` open read-only inspection without transferring task ownership. The terminal uses a compact header, bounded transcript, and stable prompt area; provider setup belongs to `mulch config` and the browser shows non-secret effective settings.

## Finish record

Final reviewer disposition: ship for the local browser additions. The documenter merged the implemented directory navigation, history deletion, and execution trace into the existing design system; no rebrand or raster assets were introduced. Authored icons remain inline SVG.

Reviewed extension captures: `ui/test-results/desktop-trace.png` (1440 × 1000) and `ui/test-results/mobile-trace.png` (390 × 844). These fixture screenshots are review evidence, not shipping imagery. Earlier welcome, conversation, and evidence captures remain baseline records.

Terminal review was limited to source inspection, reported 80 × 24 PTY behavior, and layout tests. No native terminal visual verdict is claimed. This limitation remains in the handoff; no unresolved browser design decisions were reported.
