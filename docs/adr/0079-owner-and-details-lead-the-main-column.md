# ADR-0079: Owner and details lead the main column of an item's page

- **Status:** Superseded by ADR-0102
- **Implementation:** Superseded
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked that owner and details sit in the main column instead of at the right, 2026-10-21)

## Context and problem statement

ADR-0051 put everything that belongs to a page beside it, in a column at the right: owner and details, comments, files, relations, access, history. For catalog entries (ADR-0074, ADR-0075) the details *are* the content: endpoint, publisher, owner, contact, data set. Their text is often empty. Such a page showed an empty main column and its substance in a narrow side column, with long addresses cut off. Where do an item's fields belong?

## Decision drivers

- What an item is about is read first and in full width.
- One layout for every type (D6), not one per type.
- The side column keeps what accompanies an item, not what it is.

## Considered options

1. **Owner and details at the top of the main column, above the text, for every type.**
2. **Only for types without a body or with many fields.**
3. **Keep them at the right.**

## Decision outcome

Chosen option: **1**.

- The details card opens the main column. It holds the fields, the workflow's transitions and the labels.
- Each field shows its name and value side by side. The fields sit in as many columns as the width allows.
- The side column keeps comments, files, relations, access, history and the actions.
- This amends ADR-0051 for the fields only.

### Consequences

- **Positive:** catalog entries and tasks read as one page; long values such as addresses are readable.
- **Negative / trade-offs accepted:** on a page with many fields, the text starts further down. On narrow screens the details come before the text, as before.

## Pros and cons of the options

### Option 2: only for some types

Strongest form: a long article keeps its text at the top; only records with fields move them up.

- Bad: the same page would look different depending on its type and on how many fields it has. Where something sits should not depend on a count.

### Option 3: keep them at the right

Strongest form: ADR-0051's reasoning holds for prose pages, which are the majority.

- Bad: for the catalog the maintainer works with, the right column hid the content and left the main column empty.

## Links

- amends [ADR-0051](0051-pages-open-for-reading-and-are-edited-in-place.md)
- relates to ADR-0074 and ADR-0075 (catalog entries)
- superseded by [ADR-0102](0102-owner-and-details-beside-the-text-sign-in-centred.md) (owner and details beside the text)
