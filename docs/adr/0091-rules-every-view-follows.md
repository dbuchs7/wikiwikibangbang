# ADR-0091: Rules every view follows

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators. The maintainer asked to overhaul the whole user interface, apply uniform rules following common usability practice, and record them in an ADR if needed. They chose to keep navigation and functions as they are (2026-10-22).

## Context and problem statement

ADR-0065 gave the UI one set of tokens, one frame and three button weights. Views added since have each made small choices of their own. A review of every view (desktop and phone, light and dark) found the following:

- **File fields:** they show the browser's button in the browser's language ("Choose File" on a German page).
- **Destructive actions:** some are outlined buttons, others plain links or filled buttons, some without confirmation.
- **Notices:** an informing notice used the accent colour. Where a corporate design makes the accent red, it reads as an error.
- **Empty states:** "nothing found" takes several forms: muted text, plain text, a card.
- **Spacing:** headings follow a table without space.
- **Headings:** one view has no readable heading.
- **Space links:** the other views of a space are a dotted line of links.
- **Phones:**
  - the logo is cut at the top;
  - fields squeeze beside their labels;
  - rows of lists run off the screen;
  - the label field is half the height of the others.
- **Album:** it shows the word "null".

Rules that are not written down drift. Which rules does every view follow, so the next view follows them too?

## Decision drivers

- **Established practice:**
  - consistency and standards, visibility of state, error prevention, recognition rather than recall (the usability heuristics after Nielsen);
  - accessibility after WCAG 2.2 AA: contrast, target size, focus visible.
- **Unchanged scope:** navigation, functions and the behaviour the tests rely on stay (as in ADR-0065).
- **The corporate design keeps working (ADR-0052, ADR-0085):** rules use the tokens, so any accent colour still reads correctly.
- **No new dependency or build step (ADR-0031).**

## Considered options

1. **Written rules, kept in the shared stylesheet and in a few helpers the views use.**
2. **A component library of our own that every view is rewritten onto.**
3. **Fix the findings one by one, without rules.**

## Decision outcome

Chosen option: **1**. Every view follows these rules.

### Frame

- **Order:** a view begins with its crumbs, then one `h1` that names what is shown, then its lead.
- **Actions:** the view's actions stand to the right of the `h1`, at most one of them *primary*.
- **Other views:** a view's related views, such as the boards and checks of a space, are a row of quiet tabs (`.subnav`) under the heading, not running text.

### Rhythm

- **Sections:** sections are cards or tables in a card's frame, 18 px apart.
- **Headings:** a heading after a table, card, list or empty state keeps 24 px above it.

### Actions

- **Weights:** ADR-0065's three weights stay: *primary*, *secondary* and *link*.
- **Destructive actions:** they are red, always.
  - In a form or a view's head they are an outlined red button (`danger`).
  - In rows and lists they are red text (`link danger`).
- **Confirmation:** what cannot be undone asks first, naming what goes.

### Fields

- **Height:** every control has one height. The label field of an item is a field like the others.
- **Files:** a file field is a button in the page's language with the chosen names beside it (`dressFilePicker`). The browser's own control speaks the browser's language and is not shown.
- **Phones:** below 640 px, a field stands under its label, not beside it.

### Messages

Colour carries meaning, and only that meaning:

- **Accent:** what can be pressed.
- **Neutral notice:** information.
- **Amber (`notice warning`):** a warning, such as a list cut short.
- **Green (`notice success`):** something done, such as saved or sent.
- **Red:** an error or a destructive action.

An error says what went wrong where it happened. A notice never takes the accent, so a red accent never reads as an error.

### Empty states

- **Wording:** a list or section with nothing to show says so in one sentence, with the same helper (`empty`).
- **Frame:** standing in a view rather than in a card, it gets a quiet dashed frame.

### Tables

- **Title column:** it keeps at least 12 rem.
- **Chips:** they do not break inside.
- **Width:** a table wider than the screen scrolls inside its card.
- **Phones:** in the administration, rows wrap.

### Touch and phones

- **Touch targets:** on a touch screen, everything pressable is at least 40 px high. WCAG 2.5.8 asks for 24 px; 40 px is what a finger hits reliably.
- **Logo:** the logo has room in the top bar.

### Where the rules live

The rules are in the last section of `style.css`, marked ADR-0091, and in the helpers `empty` and `dressFilePicker` in `app.js`. A new view uses them and adds no look of its own.

### Consequences

- **Positive:**
  - The same thing looks and behaves the same everywhere.
  - Meaning does not depend on the accent colour a design chooses.
  - Phones and touch screens work without zooming.
- **Positive:** the rules are written down, so a review can point to them.
- **Negative / trade-offs accepted:** some rules recognise structure (`:has()`, `:is()`, `:not()` with lists), as in ADR-0065, and need a current browser.
- **Negative / trade-offs accepted:** the file field is wrapped in a label after it is placed in the page (a `MutationObserver`). The field stays the same element, with its listeners.
- **Negative / trade-offs accepted:** type names, field names and states are data that people define (ADR-0001). They show as defined, in the language they were defined in, and the rules do not translate them.
- **Follow-ups:**
  - lists as cards on phones instead of scrolling tables;
  - an arrangement of the item page for narrow screens.

  The maintainer left both out of this round.

## Pros and cons of the options

### Option 2: a component library

Strongest form: rules are enforced by construction; a view cannot get them wrong.

- Bad: every view is rewritten, and every browser test touched, for the same visible result. ADR-0065 rejected the same option for the same reason.

### Option 3: fix the findings only

Strongest form: the smallest change.

- Bad: the findings came from rules nobody wrote down. Without the rules, the next view brings them back.

## Links

- amends [ADR-0065](0065-a-uniform-airy-ui-system.md) (the UI system)
- relates to ADR-0031 (the UI), ADR-0052 and ADR-0085 (corporate design and themes), ADR-0017 (translation)
