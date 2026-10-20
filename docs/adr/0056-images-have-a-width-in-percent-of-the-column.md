# ADR-0056: Images have a width in percent of the text column

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that inserted pictures can be resized, 2026-10-20)

## Context and problem statement

An image block (ADR-0045) showed its picture at its own size, at most the width of the column. A large screenshot filled the page, a small icon stayed tiny. How is a picture's size kept, so it looks the same to every reader on every screen?

## Considered options

1. **A width in percent of the column**, on the image block, 10 to 100; none for the picture's own size.
2. **A width in pixels.**
3. **Scaling the file itself** on upload.

## Decision outcome

Chosen option: **1**, the block field `Width` (codec version 13).

- While editing, the image offers a size (own size, 25, 50, 75, 100 %) and a handle at its corner that resizes in steps of 5 %. Reading shows the picture at the stored width.
- The width is part of the body, sealed with it, and changes like any block: in the live session, in the history and the comparison of versions.
- Agents writing a body over MCP may set it too.

### Consequences

- **Positive:** a share of the column keeps the layout on narrow and wide screens alike (the column grows with the window since the layout uses its full width).
- **Positive:** the file is unchanged, so a drawing stays editable and a copy keeps its content (ADR-0055).
- **Negative / trade-offs accepted:** a picture can be enlarged beyond its own size and look blurred; the editor does not prevent it.
- **Negative / trade-offs accepted:** the browser still loads the whole file, also for a small width.

## Pros and cons of the options

### Option 2: pixels

Strongest form: exactly the size one chose, independent of the column.

- Bad: too large on a phone, lost on a wide screen.

### Option 3: scaling the file

Strongest form: smaller files for small pictures.

- Bad: loses detail for good, breaks the scene in a drawing's PNG and the content check of copies.

## Links

- relates to ADR-0045 (attachments), ADR-0053 (drawings), ADR-0055 (copies)
