# ADR-0095: Language and look in the header, an own start picture, an empty sheet to write on

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. The maintainer asked for:
  - a switch of the UI language (German and English) and of the light or dark look, with a light bulb, on the start page;
  - the start page's picture replaceable by an own one in the administration;
  - a larger text area while editing, like an empty sheet in the proportions of A4.

## Context and problem statement

The language was set only on the account page (ADR-0017), and the dark look followed only the system (ADR-0070). The start page always showed the product's picture. The editor's text area was a few lines high until text filled it.

How are these offered without new settings pages, and without breaking the design's own dark look?

## Decision drivers

- One place for both switches that is reached from every page, also before signing in.
- A signed-in user's language stays with the account (ADR-0017).
- The installation's own stylesheet may define its dark look with `@media (prefers-color-scheme: dark)` (ADR-0070); a chosen look must apply to it too.
- No new dependency, no build step (ADR-0014, ADR-0031).

## Considered options

1. **Both switches in the header, which the start page shows too.** The look is kept in the browser and applied by rewriting the media queries that ask for the system's scheme.
2. **Switches only on the start page.**
3. **A class on the root element and a second copy of every dark rule.**

## Decision outcome

Chosen option: **1**.

- **Language:** "DE | EN" in the header, the current language marked. A signed-in user's choice is saved with the account. Before signing in it is kept in the browser and becomes the account's language at the first sign-in. Languages the installation does not offer are not shown, and with one language the switch is hidden.
- **Look:** a light bulb in the header, lit in the light look. The choice is kept in this browser only. A choice that equals the system's scheme returns to following the system.
  - `theme.js` rewrites the media of the dark stylesheet's link, of the logo's picture source and of every `@media` rule that asks for the scheme, including the design's own stylesheet.
  - `theme-boot.js` sets the dark stylesheet's link before the page is drawn, so the chosen look does not flash.
  - The drawing editor follows the choice too.
  - When the installation turned the dark look off (ADR-0070), the bulb is not shown.
- **Start picture:** the design gains the file `welcome` (PNG, JPEG, GIF, WebP or SVG). It replaces the product's picture on the start page and keeps its own shape, at most as tall as the product's. It is set and removed under Administration → Design, like the logo.
- **Empty sheet:** while editing, the text area is at least as tall as A4 is in proportion to its width (1 : 1.414). Reading is unchanged.

### Consequences

- **Positive:** both switches are one click from every page, also on the sign-in page.
- **Positive:** an installation can show its own picture without an own stylesheet.
- **Negative / trade-offs accepted:**
  - The look is chosen per browser, not per account; another device starts from its system's scheme.
  - Rewriting media queries relies on stylesheets of this origin; a stylesheet from elsewhere would not follow, and none is linked.
  - The text area of a short text is taller than its content while editing, so the page scrolls further.

## Pros and cons of the options

### Option 2: only on the start page

Strongest form: it is exactly what was asked, and the header stays as it was.

- Bad: switching needs a detour to the start page from every other page, and the sign-in page has no start page.

### Option 3: a theme class and duplicated rules

Strongest form: the usual technique, with no rewriting at run time.

- Bad: every dark rule would exist twice, and the design's own stylesheet (ADR-0070) would not follow unless every installation wrote its rules twice.

## Links

- amended by [ADR-0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) (the language switch as letters)
- amends [ADR-0017](0017-internationalisation-via-message-catalogue.md) (where the language is chosen), [ADR-0070](0070-an-own-stylesheet-without-a-dark-look-turns-the-dark-scheme-off.md) (how the dark look is chosen) and [ADR-0052](0052-corporate-design-in-a-journal-of-its-own.md) (files of the design)
- relates to ADR-0065 and ADR-0091 (the UI's rules), ADR-0053 (drawings)
