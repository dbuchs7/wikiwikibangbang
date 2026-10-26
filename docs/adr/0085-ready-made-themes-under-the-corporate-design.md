# ADR-0085: Ready-made themes under the corporate design

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for ready-made themes in different colours and shapes, among them one in comic style and one as a hand sketch, 2026-10-21)

## Context and problem statement

The corporate design (ADR-0052) sets a name, a logo, four colours, a font and an own stylesheet. Changing the look beyond colours requires writing CSS. Administrators who want a different look, such as rounder or squarer shapes, a dark look, a comic or a hand-drawn one, have nothing to start from.

How does an installation get a distinctly different look without anyone writing CSS?

## Decision drivers

- An administrator picks a look in one step, and can still set the logo, colours and font over it.
- The product keeps the themes up to date with its own markup; an installation does not keep a stale copy.
- Nothing is fetched from elsewhere: fonts ship with the binary (ADR-0052).
- The dark scheme stays on only where the look defines one (ADR-0070).

## Considered options

1. **Themes as stylesheets in the binary, chosen by name in the design.**
2. **Themes as templates that fill the design's own stylesheet.**
3. **No themes; document example stylesheets.**

## Decision outcome

Chosen option: **1**.

- **Themes:** eight are offered, each a stylesheet in `internal/site/themes`:
  - Ocean, Forest, Sunset and Berry: colours and rounder shapes, light and dark;
  - Swiss: square corners and clear lines, light and dark;
  - Night: always dark;
  - Comic: ink outlines, hard shadows and pop colours, light only;
  - Sketch: pencil on ruled paper with hand-drawn boxes, light only.
- **Order:** `/branding.css` holds the theme first, then the design's colours and font, then the own stylesheet. What an administrator sets applies over the theme.
- **Dark scheme:** a theme without `@media (prefers-color-scheme: dark)` rules turns the UI's dark scheme off, as an own stylesheet without them does (ADR-0070).
- **Fonts:** Comic uses Comic Neue, shipped in `assets/fonts`. Sketch uses Virgil, already shipped with the drawing editor (ADR-0053). Both are under the SIL Open Font License 1.1.
- **Choosing:** the design card lists the themes with a colour preview. The design keeps the theme's name (`theme`); an unknown name is refused.

### Consequences

- **Positive:** a different look is one click. A theme follows the product's markup, as it ships with it.
- **Negative / trade-offs accepted:** the contrast check of ADR-0052 knows only the design's own colours, not a theme's. The themes were checked by eye, not measured.
- **Negative / trade-offs accepted:** the themes restyle the product's components through its class names. A change to the markup can leave a theme partly applied until it is adjusted.
- **Negative / trade-offs accepted:** the start page's illustration keeps the product's style in every theme.

## Pros and cons of the options

### Option 2: templates for the own stylesheet

Strongest form: no new field, and an administrator can edit the theme freely.

- Bad: every installation keeps a copy that no longer follows the product's markup, and the theme is lost the moment the own stylesheet is replaced.

### Option 3: example stylesheets

Strongest form: nothing to maintain in the product.

- Bad: it leaves the look to those who write CSS, which is what was asked to avoid.

## Links

- amends [ADR-0052](0052-corporate-design-in-a-journal-of-its-own.md) (a theme under the design) and [ADR-0070](0070-an-own-stylesheet-without-a-dark-look-turns-the-dark-scheme-off.md) (a theme turns the dark scheme off the same way)
- amended by [ADR-0120](0120-the-swiss-theme-follows-the-confederations-design-system.md) (the Swiss theme follows the Confederation's design system)
