# ADR-0100: A tidy top bar on every width, and columns that fold away

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. The maintainer asked for three changes:
  - the columns beside a page (the page tree on the left; comments, files and the rest on the right) to fold away, for more room for the content;
  - the top bar not to fall into disorder on narrow screens;
  - the language switch to show its letters alone, without a frame.

## Context and problem statement

**Top bar.** It was one flex row that wrapped wherever an element ended. Between about 640 and 1100 px the search stayed on the first line while New, the bell and the switches dropped to a second line, at different places depending on the width.

**Columns.** On wide screens a page stands between two columns: the page tree (ADR-0041) and the item's side column (ADR-0079). The tree could close its list but kept its width. The side column could not be put away. Long texts and tables had less room than the screen offered.

**Language switch.** "DE | EN" (ADR-0095) was drawn as a framed segmented control, heavier than the icons beside it.

## Decision drivers

- The same top bar on every width: a fixed arrangement, never a wrap by chance.
- Room for the content when wanted, without losing the columns for good.
- No new dependency, no build step (ADR-0014, ADR-0031).

## Considered options

1. **Two fixed rows below 900 px; a fold button on each column, kept per browser; the language switch as letters.**
2. **A hamburger menu that holds the whole navigation on narrow screens.**
3. **Columns that resize by dragging.**

## Decision outcome

Chosen option: **1**.

- **Top bar:**
  - **From 900 px:** one row. The search grows and shrinks, and nothing wraps.
  - **Below 900 px:** two rows. The first holds the module switcher, home, back and forward, the logo, the bell, the language, the bulb and the account. The second holds the search and New.
  - **Below 480 px:** back and forward give way.
  - **Below 420 px:** home gives way too, since the logo leads home, and the logo is smaller.
  - The bar holds together down to 320 px.
- **Folding:**
  - The page tree has a button at its top right, and an item's side column one at its top left.
  - A folded column leaves only its button, a 32 px strip, and the content takes the rest.
  - The choice is kept in this browser for every page (`localStorage`), like the light or dark look.
  - Where the columns already stand one above the other (the tree below 800 px, the side column below 1100 px), nothing is folded and the buttons are hidden.
- **Language switch:** the letters "DE EN" alone, without a frame or separators. The current language is in the accent colour and bold.

### Consequences

- **Positive:** the top bar reads the same at every width; a page can use the whole screen.
- **Negative / trade-offs accepted:**
  - On a phone, back and forward are only the browser's own.
  - A folded column hides its content, notifications of new comments included, until it is shown again.

## Pros and cons of the options

### Option 2: a hamburger menu

Strongest form: the most room on a phone.

- Bad: search and New, used most, would sit one tap further away, and the bar would differ in kind between phone and desktop.

### Option 3: resizing by dragging

Strongest form: any width one likes.

- Bad: more code, fiddly on touch screens, and a width chosen by dragging is hard to get back to.

## Links

- amended by [ADR-0101](0101-a-reading-mode.md) (a reading mode)
- amends [ADR-0095](0095-language-and-look-in-the-header-and-an-own-start-picture.md) (the language switch) and [ADR-0041](0041-page-tree-and-sibling-order.md) (the page tree beside the content)
- relates to ADR-0065, ADR-0091 and ADR-0099 (the UI's frame), ADR-0079 (the item's side column)
