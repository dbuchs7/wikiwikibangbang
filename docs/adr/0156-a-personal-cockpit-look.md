# ADR-0156: A personal cockpit look

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer asked for the gamified theme of atlas in Orbithek and chose a personal choice over a theme for everyone, 2026-11-01)

## Context and problem statement

atlas has a gamified skin, the starship cockpit (atlas ADR-0509): a deep-space palette with cyan HUD accents, a drifting starfield, faint scanlines and a few soft lights, chosen by each person as a fourth appearance beside light, dark and system. The maintainer wants the same look in Orbithek, which now speaks of a library in orbit (ADR-0153, ADR-0154).

Orbithek has two kinds of looks: the light or dark look, which each person chooses with the bulb in the header and which follows the system otherwise (ADR-0070, ADR-0095), and the ready-made themes, which an administrator sets for the whole installation (ADR-0085). Where does the cockpit belong?

## Decision drivers

- **Fun is a personal choice:** a playful look should not be imposed on everyone, nor turn the organisation's design aside.
- **Reuse, not a second theming engine:** the dark look already paints every surface from tokens; the cockpit should ride it.
- **Accessibility:** motion stops under `prefers-reduced-motion`; contrast stays readable.
- **Buildless:** plain CSS and JavaScript (ADR-0031).

## Considered options

1. **A third personal look, `cockpit`, beside light and dark**, chosen on the account page and kept in the browser.
2. **A ready-made theme for everyone**, set by an administrator.
3. **Both.**

## Decision outcome

Chosen option: **1, "a third personal look"**, because it is what atlas does and what the maintainer chose, and because the dark look's tokens carry it with one palette block.

- **Choice:** the account page offers *Look*: as the system, light, dark, or cockpit (just for fun). The choice is kept in the browser under the same key as the bulb's (`ot.theme`); the system never selects the cockpit. The page offers it only when the dark look is offered (ADR-0070), since it builds on the dark stylesheet.
- **Mechanism:** the cockpit counts as dark: `theme-boot.js` turns the dark stylesheet on before the first paint and sets `data-look="cockpit"` on the root; `theme.js` forces every rule that asks for the system's scheme to dark, as for a chosen dark look. The bulb in the header treats the cockpit as dark: a press leads to light.
- **Palette:** a `:root[data-look="cockpit"]` block in `style.css` answers the dark look's tokens in atlas's deep-space, cyan-HUD key; it outranks the dark stylesheet's `:root` by specificity. It also outranks the design's accent and header colours (ADR-0052): someone who chose the cockpit sees the cockpit.
- **Decoration:** a nebula behind the page, a drifting starfield, faint scanlines with a glow from the top, a glass header, a pulsing glow on the logo's planet, glowing buttons and module symbols, an online light on one's avatar and telemetry lights on the start page's counts. All of it is CSS, inert (`pointer-events: none`) and behind the content. Under `prefers-reduced-motion` nothing moves; the palette and glow stay.

### Consequences

- **Positive:** the look of atlas in Orbithek with no new infrastructure: one palette block, decoration and a choice on the account page.
- **Negative / trade-offs accepted:** the cockpit is per browser, not per account; a person chooses it again on another device. It hides the organisation's accent and header colours for whoever chose it. The decoration is bespoke CSS to maintain.
- **Follow-ups / risks to watch:** views that paint colours of their own (diagram canvases, the drawing editor) look as in the dark look, not cockpit-tuned. If people ask for it on every device, the choice can move to the account like the language.

## Pros and cons of the options

### Option 1: a personal look (chosen)

Strongest form: playfulness stays with whoever wants it; everyone else keeps the organisation's look.

- Good: mirrors atlas; small; no server change.
- Bad: kept per browser.

### Option 2: a theme for everyone

Strongest form: it fits Orbithek's existing ready-made themes, which already include playful ones (comic, sketch).

- Good: set once, on every device.
- Bad: imposes a playful look on all; atlas deliberately rejected that for the same skin.

### Option 3: both

Strongest form: an installation that wants to be a starship for all can be.

- Good: covers both wishes.
- Bad: two ways to the same look to keep in step; nobody asked for the second.

## Links

- after atlas ADR-0509 (the cockpit skin), which it mirrors
- builds on ADR-0070 (dark scheme) and ADR-0095 (look switch in the header)
- relates to ADR-0085 (ready-made themes), ADR-0052 (corporate design), ADR-0031 (buildless UI)
