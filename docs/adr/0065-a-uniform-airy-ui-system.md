# ADR-0065: A uniform, airy UI system

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked to overhaul the whole user interface to be intuitive, as simple as possible, uniform, with the same structure everywhere and attractive; they chose a uniform system without changing navigation or functions, in a modern and airy style, 2026-10-20)

## Context and problem statement

The UI grew view by view (ADR-0031). Each view chose its own frame: some put the heading in a row with actions, others not; crumbs were plain links; some tables stood in cards, others bare on the page; every button was a filled accent button, so the main action of a view did not stand out; item fields each had their own Save button; the administration was one long column of forms. How does the UI get one structure and one look without rewriting its views?

## Decision drivers

- Same structure everywhere: crumbs, heading with lead and actions, then cards.
- One clear main action per view; everything else quieter.
- No new dependency, no build step, no font download (ADR-0014, ADR-0031).
- The corporate design (ADR-0052) keeps working: it sets the tokens, not the components.
- The behaviour the tests and users rely on stays.

## Considered options

1. **A token-based system in the existing stylesheet, applied by rules that recognise the existing structure, plus a few structural changes in the views.**
2. **A component library or CSS framework.**
3. **Rewriting every view onto new helper components.**

## Decision outcome

Chosen option: **1**.

- **Tokens:** one palette (light and dark), spacing scale, radii (12 px surfaces, 8 px controls), two shadows, a focus ring, type scale (26 px headings, 14.5 px text).
- **Frame:** the crumb line, the view's heading and the paragraph after it as its lead are styled where they stand; tables outside cards get a card's frame; tabs are one underlined line.
- **Buttons:** three weights. *Primary* (filled) is a submit button or an action marked `primary`, *secondary* (outlined) is the default, *link* is text. Dangerous actions are outlined in red.
- **Fields:** one height, one radius, one focus ring; forms in cards keep a readable width; file pickers look like buttons.
- **Structural changes:** module icons in the drawer; the administration with its contents in a sticky column on the left, the section in view marked; item fields save as they change, with a mark beside them, instead of one Save button each; New task as the primary action of the Tasks page.

### Consequences

- **Positive:** every view reads the same way; the eye finds the main action at once.
- **Positive:** a new view gets the look by using the existing elements.
- **Negative / trade-offs accepted:** some rules recognise structure (`:has()`, `:not()` with lists), which needs a current browser; older ones fall back to the previous look, not a broken one.
- **Negative / trade-offs accepted:** a field saved on change cannot be edited "in draft"; every change is an event, as it was with its Save button.

## Pros and cons of the options

### Option 2: a framework

Strongest form: a tested, complete set of components.

- Bad: a dependency and probably a build step, a second vocabulary next to the views' own, and a heavier page.

### Option 3: rewrite the views

Strongest form: the cleanest result.

- Bad: thousands of lines changed at once, and every browser test touched, for little visible gain over option 1.

## Links

- relates to ADR-0031 (the UI), ADR-0052 (corporate design), ADR-0041 (navigation)
