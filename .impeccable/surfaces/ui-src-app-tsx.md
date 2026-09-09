---
version: 1
slug: "ui-src-app-tsx"
primary_target: "ui/src/App.tsx"
related_targets: ["ui/src/components","ui/src/workspace.css","ui/src/workspace-responsive.css"]
---

# Browser workspace

Mode: Operate. Target: `ui/src/App.tsx`, its components, and workspace styles.

The approved scope and workflow remain in `docs/web-interface-plan.md`. This brief records the implemented surface and its inherited visual authority; it introduces no new product direction.

## Direction contract

THESIS: Complete repository tasks through readable conversation, with repair evidence alongside; a dashboard does not lead.

OWN-WORLD: Inherited warm neutral and olive surfaces, fine borders, compact controls, serif welcome, authored SVG icons.

STORY: Choose the workspace, send a task, read results, inspect evidence, continue.

FIRST VIEWPORT: Desktop sidebar (250px), flexible conversation, bottom composer; opening evidence uses a 230px sidebar and 370px inspector. Narrow screens use modal drawers.

FORM: Approved conversation-first structure from `docs/web-interface-plan.md`. No ranked concept list or seed key: the precise request inherited the established world; concept seeding was inapplicable.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Interaction and state

The signature interaction opens a conversation's repair notice directly onto its selected evidence above the decision list, moving focus to that detail. Context occurrences and candidate evidence retain recorded lineage and unknown states. Health is not correctness; missing dimensions are nonnumeric.

At 1199px and below, the inspector uses a native modal dialog; at 700px and below, sessions do too. Dialogs support Escape, labelled close controls, and focus return. The composer remains available below the conversation. Scrolling into older content suspends following until “Jump to latest.” Live completion announcements remain silent during historical replay.

Motion is restrained: no decorative animation is required, disclosure icons change orientation, and reduced-motion preferences suppress animation, transitions, and smooth scrolling.

## Finish record

Final reviewer disposition: ship; all six scored fixes resolved, as reported in the implementation handoff. The final documenter extracted the built system into root `DESIGN.md` and `.impeccable/design.json` after corrections. No shipping raster assets were introduced; the authored icon system is inline SVG.

Review captures: `ui/test-results/desktop-welcome.png`, `desktop-conversation.png`, `desktop-evidence.png`, `mobile-conversation.png`, and `mobile-inspector.png` in the same directory. These are fixture screenshots, not shipping imagery.

No unresolved design decisions remain in this handoff.
