---
name: Mulch
description: Warm neutral and olive controls for a local coding workspace.
colors:
  olive: "#4b5e36"
  paper: "#fafaf7"
  panel: "#f0f1e9"
  line: "#dfe1d5"
  ink: "#282c24"
  muted: "#656a5c"
  tint: "#e8eddd"
  danger: "#9d3c2e"
typography:
  body:
    fontFamily: 'ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: "14px"
    lineHeight: 1.75
  title:
    fontSize: "16px"
    fontWeight: 550
    lineHeight: 1.5
  label:
    fontSize: "12px"
    fontWeight: 600
  code:
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "12px"
    lineHeight: 1.65
rounded:
  badge: "4px"
  code: "6px"
  control: "7px"
  tool: "8px"
  surface: "12px"
spacing:
  compact: "8px"
  control: "12px"
  content: "14px"
  section: "20px"
  roomy: "24px"
components:
  button-primary:
    backgroundColor: "{colors.olive}"
    textColor: "white"
    rounded: "{rounded.control}"
    padding: "9px 12px"
  button-secondary:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "9px 12px"
  button-quiet:
    textColor: "{colors.muted}"
    rounded: "{rounded.control}"
    padding: "6px 9px"
  button-stop:
    backgroundColor: "#f5e6df"
    textColor: "{colors.danger}"
    rounded: "{rounded.control}"
    padding: "9px 12px"
  field:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "11px"
  badge:
    backgroundColor: "{colors.tint}"
    rounded: "{rounded.badge}"
    padding: "3px 6px"
  session-navigation:
    rounded: "{rounded.control}"
    padding: "12px 14px"
  tool-container:
    backgroundColor: "#f5f6f0"
    rounded: "{rounded.tool}"
  composer:
    backgroundColor: "white"
    rounded: "{rounded.surface}"
    padding: "14px"
  repair-notice:
    backgroundColor: "#ecf0e0"
    padding: "14px"
---

# Design System: Mulch

## Overview

**Creative North Star: "Mulch's established local workspace"**

Mulch inherits a warm neutral and olive identity: pale work surfaces, dark readable text, and restrained controls. The serif welcome and italic brand mark provide the established expressive detail; system sans-serif carries the operating interface.

Depth comes mainly from surface tones and fine borders. Components remain compact, with open reading space around conversation text. This records the implemented workspace, without introducing a new metaphor, rebrand, or imagery.

**Key Characteristics:**

- Warm neutral surfaces with one olive action accent.
- System interface text, serif welcome, monospace evidence.
- Fine borders, gently rounded controls, restrained elevation.

## Colors

Restrained olive actions sit on warm, slightly green neutral surfaces. The frontmatter records the reused CSS custom properties without renaming their source roles.

### Primary

- **Olive:** primary actions, links, focus outlines, and the brand mark.
- **Pale olive tint:** hover and selected treatments; badges and active evidence tabs.

### Neutral

- **Warm paper:** workspace ground and secondary controls.
- **Soft panel:** sidebar ground.
- **Fine sage line:** separators and field borders.
- **Dark botanical ink:** primary text.
- **Muted gray olive:** supporting text and secondary actions.

### Semantic

- **Brick danger:** cancellation controls and error-associated text; pale warm fills distinguish these states from ordinary actions.

**The Text With Color Rule.** Status and selection retain readable labels; color supplies reinforcement.

## Typography

The body stack is the system sans-serif recorded above. Conversation prose uses the body role; section titles and message labels step down into a compact interface hierarchy. Evidence and code use the mono role, with smaller inspector previews.

The approved welcome uses Georgia with Times New Roman and serif fallbacks, regular weight, tight tracking, and a larger scale (36px desktop, 30px narrow). The italic serif brand mark is an inherited identity asset. These specific treatments belong to the welcome and brand; they do not establish a generic display-font rule for future surfaces.

## Layout

Use a viewport-height workspace with independently scrolling reading and navigation regions. The desktop shell allocates a sidebar and flexible main column; evidence is optional. The current route's exact composition and behavior live in its surface brief.

The layout changes at the existing maximum-width breakpoints (1199px and 700px). Evidence becomes a modal drawer below the first; sessions become a modal drawer below the second. Narrow layouts reduce outer padding while preserving usable controls and wrapped content. Keep the responsive stylesheet after base styles so those overrides win.

## Elevation & Depth

Tonal separation and thin borders carry most depth. The composer has a subtle resting shadow; dialogs and the floating latest-message action have stronger structural shadows. Exact shadow values are recorded in the sidecar, where the schema supports them. This is not a shadow-free system.

**The Structural Depth Rule.** Elevation distinguishes floating controls and modal surfaces from the reading plane.

## Shapes

Controls have gently rounded corners; badges and code containers use smaller curves. Tool disclosures use a slightly wider radius, while the composer and dialogs share a larger surface radius. Repair notices and decision rows use square edges and line boundaries. Preserve that distinction rather than making every region a rounded card.

## Components

### Buttons

Compact, explicit controls. Primary buttons use olive and white, darkening on hover. Secondary buttons use paper with a line border. Quiet buttons use muted text and tighter padding; stop controls use danger text on a pale warm fill. Buttons share the control radius, a pale olive hover where not overridden, and reduced opacity with a blocked cursor when disabled.

Keyboard focus is an olive outline (2px) offset from the control (3px). Authored inline SVG icons use currentColor, rounded strokes (1.6px), and a consistent box (16px); decorative icons are hidden from assistive technology.

### Chips

Small pale olive badges carry short recorded-state labels. The selected candidate badge is evidence context, not an action or quality endorsement.

### Cards / Containers

Tool calls are bordered disclosures with a monospace summary and readable status. User messages have a pale tonal enclosure, while assistant prose remains on the main reading plane. Code previews scroll or wrap within bounded containers.

### Inputs / Fields

Fields use paper, a fine border, and the control radius. The white composer has a larger border enclosure and olive focus-within border. Its textarea grows within a bounded height; the surrounding composer carries its focus treatment. Error and setup notices remain separate, labelled text regions.

### Navigation

Session rows pair a truncated title with supporting metadata. The active row deepens the pale olive fill and strengthens its title weight. Evidence tabs use a tinted selected background, olive text, and aria-selected. Modal session and inspector drawers use native dialog behavior, Escape dismissal, labelled close controls, and focus return.

### Repair evidence

A square bordered notice links conversation context to inspectable evidence. The selected repair detail appears before the decision list and receives focus. Missing dimensions show a nonnumeric dash and dashed placeholder; numeric meters appear only when a value exists. See the surface brief for this route's complete interaction contract.

## Do's and Don'ts

### Do:

- **Do** preserve the inherited neutral and olive identity.
- **Do** use explicit text alongside color for status and selection.
- **Do** retain visible keyboard focus and reduced-motion behavior.

### Don't:

- **Don't** introduce a new brand or decorative imagery into this workspace.
- **Don't** present missing evidence as a numeric zero.
- **Don't** turn context health or candidate selection into a correctness grade.

Sources: [workspace styles](ui/src/workspace.css), [responsive styles](ui/src/workspace-responsive.css), and [components](ui/src/components/). Recorded after the final implementation review on 2026-09-08. Product constraints remain in [PRODUCT.md](PRODUCT.md).
