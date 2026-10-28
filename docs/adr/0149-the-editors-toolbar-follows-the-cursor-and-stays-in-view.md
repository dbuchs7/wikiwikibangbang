# ADR-0149: The editor's toolbar follows the cursor and stays in view

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-28
- **Deciders:** Project initiators (the maintainer reported that a change of block kind took several tries, that headings of the second and third level looked alike, and that on long pages the toolbar had to be fetched by scrolling, 2026-10-28)

## Context and problem statement

ADR-0059 made the toolbar show what applies at the selection: marks and alignment are pressed while they apply. The choice of block kind (paragraph, headings, lists, quote, code, panels) was left out. It kept the kind chosen last, so after choosing *Heading 2* in one paragraph, choosing *Heading 2* in the next did nothing: a choice that already shows the option it is given does not change, and the browser reports no change. People had to choose another kind first and then the one they wanted.

Two further things got in the way of editing. In the text, a heading of the second level was barely larger than one of the third, and the third barely larger than a paragraph, so a text's structure was hard to see. And the toolbar sat above the text: on a long page, every change of format meant scrolling up to the toolbar and down again.

How do the toolbar and the headings serve a long text?

## Decision drivers

- **A choice applies the first time,** as in every word processor.
- **The structure of a text is visible at a glance.**
- **The tools are where one writes,** on a long page too, without covering the text.
- **No new controls:** the same toolbar, the same keys.

## Considered options

1. **The block kind follows the cursor; headings set apart by size, weight and space; the toolbar sticks below the top bar while editing**
2. **A floating toolbar at the selection,** as some editors show over selected text
3. **Keyboard shortcuts only** for the block kinds, leaving the toolbar as it was

## Decision outcome

Chosen option: **1**.

- **Block kind:** the choice shows the kind of the block the selection starts in, with every change of the selection, one's own or another's in a live session (ADR-0059). A heading shows its level and a panel its tone. A block the choice does not list, such as a picture or a table, leaves it empty. Choosing a kind therefore always changes the choice and applies at once.
- **Headings:** in the text, the first level is 1.75 times the size of the text, bold, with a thin rule below it. The second level is 1.4 times the size. The third is 1.15 times the size and in the muted colour. Each has more space above than below, so it belongs to what follows. The first block of a text has no space above. The sizes are relative to the text, so a theme that changes the text's size keeps the steps.
- **Toolbar in view:** while a text is edited, its toolbar sticks just below the top bar as the page scrolls, for as long as the text is on screen. Its offset is the top bar's measured height, as the top bar takes two rows on narrow screens. The toolbar of a text being read, and the editors of comments and dialogs, stay as they are.

### Consequences

- **Positive:** a change of block kind applies the first time; a text's sections stand out; the tools are at hand anywhere in a long text.
- **Negative / trade-offs accepted:**
  - The toolbar covers the top of the visible part of the text, one or two rows depending on the width. The cursor's line can scroll under it; the browser scrolls the cursor into view when one types.
  - Headings take more room than before; existing texts look longer.
- **Follow-ups / risks to watch:**
  - stored marks (bold before typing) in a live session with many others typing, should a mark chosen with nothing selected still get lost.

## Pros and cons of the options

### Option 2: a floating toolbar at the selection

Strongest form: the tools appear exactly where one works, and the page has no bar at all.

- Good: no scrolling, no bar covering text.
- Bad: the block kind, inserting pictures, diagrams and tables, and the text's own actions need room a bubble does not have; two toolbars would then be needed, with a selection to make before any tool shows. It hides the state of the text when nothing is selected.

### Option 3: keyboard shortcuts only

Strongest form: no visual change; experienced writers already use keys.

- Bad: the reported failure is in the toolbar, and most people use it; shortcuts do not make the headings distinct or the toolbar reachable.

## Links

- amends [ADR-0059](0059-alignment-dividers-and-more-marks.md) (the toolbar shows the block kind too; headings set apart; the toolbar stays in view)
- relates to ADR-0032 (editor), ADR-0042 (block kinds), ADR-0091 (rules every view follows), ADR-0095 (the text as a sheet while editing)
