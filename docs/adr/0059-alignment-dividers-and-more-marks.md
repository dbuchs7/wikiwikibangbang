# ADR-0059: Text and pictures are aligned, bodies get dividers and three more marks, and the toolbar shows what applies

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for a fuller editor that handles the alignment of text and pictures, 2026-10-20)

## Context and problem statement

The body editor (ADR-0032) wrote headings, lists, quotes, code, tasks, panels, tables and images, with bold, italic, code, links and mentions. Pages that people build for others to read need more layout: centred titles, justified text, a picture beside its text, a line between sections, emphasis beyond bold and italic. How much of this belongs in the block model, which stays a structure (ADR-0001), not a page-layout language?

## Decision drivers

- Everything a reader sees is in the model, so search, export, history, live editing and agents see it too.
- No style attributes: the content security policy forbids them, and free styling erodes a common look.
- A small, closed set of values, validated like every block field.

## Considered options

1. **Closed fields and marks:** `Align` on blocks (text: center, right, justify; images: left, center, right with text flowing beside them), a `divider` block, and the marks `underline`, `strike` and `highlight`.
2. **Free styling** (colours, font sizes, CSS per block).
3. **Editor-only presentation**, not stored.

## Decision outcome

Chosen option: **1**, codec version 15.

- Paragraphs, headings, quotes and panels align center, right or justify; left is the default. Lists, tasks, code and tables are not aligned.
- An image aligns left or right, floating with the text beside it at its width (half the column by default), or center. On narrow screens it stops floating.
- `divider` is a horizontal rule that holds nothing.
- The marks underline, strike-through and highlight carry no target.
- The editor's toolbar is grouped: block type; marks; alignment; lists; link, mention and embed; insert. Buttons for marks and alignments show whether they apply at the selection, also after changes from others in a live session. Shortcuts: Ctrl+U, Ctrl+Shift+X, Ctrl+Shift+H, Ctrl+Shift+L/E/R/J.
- Alignment and marks are classes and elements, never style attributes; pasted text keeps an alignment it states.

### Consequences

- **Positive:** the look stays the installation's (ADR-0052); content stays structured and searchable.
- **Negative / trade-offs accepted:** no colours, sizes or columns; a layout beyond this set is not possible in a body.
- **Negative / trade-offs accepted:** splitting an aligned paragraph in the middle keeps the alignment in both halves; at the end of a paragraph the new one starts left.
- **Follow-ups / risks to watch:** importers (ADR-0023) may map alignment and horizontal rules from the source.

## Pros and cons of the options

### Option 2: free styling

Strongest form: any layout a person can imagine.

- Bad: style attributes are refused by the policy; styled content diverges from the corporate design and is hard to read in the dark scheme.

### Option 3: editor-only

Strongest form: no model change.

- Bad: what the author arranged would be lost on save and invisible to readers.

## Links

- relates to ADR-0001 (structured bodies), ADR-0032 (editor), ADR-0042 (block kinds), ADR-0052 (design), ADR-0056 (image size)
