# ADR-0102: Owner and details beside the text; the sign-in card centred

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. After the left-aligned layout (ADR-0099), the reading mode (ADR-0101) and the empty sheet to write on (ADR-0095), the maintainer asked for two changes:
  - owner and details move to the right column;
  - the sign-in card stands in the middle again.

## Context and problem statement

**Owner and details.** ADR-0079 put the owner and details at the top of the main column, above the text, so that a catalog entry would read as one page. Since then, the text became an empty sheet in the proportions of A4 while editing (ADR-0095), and the columns beside it fold away (ADR-0100). With the details card above it, the sheet started half a screen down, and the editor's bar with it. The details are what a reader looks up, not what they read or write. The maintainer asked for them in the right column, with the comments and files.

**Sign-in.** ADR-0099 left-aligned every view, the sign-in card included. The sign-in view holds nothing but the card: there is no header edge for it to line up with, and a lone card at the left edge of a wide window looks misplaced.

## Decision drivers

- The text, read or written, starts at the top of the main column (ADR-0095).
- What belongs to a page stays together beside it (ADR-0051).
- Long values, such as addresses of catalog entries, stay readable in full.
- A view that holds one card shows it where the eye looks first.

## Considered options

1. **The details card leads the right column, for every type; single cards are centred.**
2. **Keep the details above the text (ADR-0079), folded to one line.**
3. **Details in the right column only for types without a text.**

## Decision outcome

Chosen option: **1**.

- **Details:** the card "Owner and details" (fields, the workflow's transitions, labels) is the first card of the right column, above the comments. Each field shows its name above its value; long values wrap instead of being cut off.
- **The main column** holds the text alone.
- **Folding:** the details fold away with the right column (ADR-0100).
- **Narrow screens:** below 1100 px the right column follows the text, the details as its first card.
- **Printing:** the printed page keeps the details above the text, as before.
- **Sign-in and other single cards** (`.narrow`: sign-in, password reset, the consent of an AI app) stand in the middle of the window. The column gives up its maximum width for them, so they are centred on wide screens too.
- This supersedes ADR-0079 and amends ADR-0099 for single cards.

### Consequences

- **Positive:** the text and the editor's bar start at the top; the sheet to write on is in view at once.
- **Positive:** the sign-in looks the same as before ADR-0099.
- **Negative / trade-offs accepted:** a catalog entry without text shows an empty main column and its substance at the right, the case ADR-0079 was made for. The right column is up to 440 px wide, and values wrap.
- **Negative / trade-offs accepted:** on narrow screens, the details come after the text.
- **Negative / trade-offs accepted:** where the browser does not support `:has()`, a single card is centred within the 1680 px column, not the window.

## Pros and cons of the options

### Option 2: details above the text, folded

Strongest form: the catalog keeps its one-page reading, and the text moves down by one line only.

- Bad: a folded card hides the owner, the field most often looked for, behind a click. The maintainer asked for the right column.

### Option 3: by type

Strongest form: each type gets the layout that fits it.

- Bad: the same card moves between pages, which ADR-0091 rules out (same structure everywhere). Whether a page has a text is a matter of content, not type.

## Links

- supersedes [ADR-0079](0079-owner-and-details-lead-the-main-column.md)
- amends [ADR-0099](0099-the-layout-is-left-aligned.md) (single cards are centred) and [ADR-0051](0051-pages-open-for-reading-and-are-edited-in-place.md) (the details are beside the text again)
- relates to ADR-0091 (rules every view follows), ADR-0095 (the empty sheet), ADR-0100 (columns that fold away)
