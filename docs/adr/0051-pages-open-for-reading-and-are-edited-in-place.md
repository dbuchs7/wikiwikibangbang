# ADR-0051: Pages open for reading and are edited in place; what belongs to a page sits in a column beside it

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer found opening straight into the editor confusing and chose, from three drawn variants, editing in place, 2026-10-20)

## Context and problem statement

Opening a page or task put the reader straight into the live editor (ADR-0010): a toolbar, a live session with block locks, a caret. Most visits are to read. The sections of an item (fields, comments, files, relations, access, history) were stacked below the text in no particular order, so the page grew long and what mattered next to the text was far from it.

## Considered options

Three layouts were drawn for the maintainer:

1. **Read, and edit in place:** the page opens for reading; *Edit* turns the same text into the editor, with its toolbar above it; *Done* returns to reading. A column on the right holds the item's sections.
2. **Read, and edit beside a live preview:** two columns while editing, the preview on the left and the editor with the sections on the right.
3. **Tabs on the right:** a wide preview, and tabs for editing, details, comments, access and history.

## Decision outcome

Chosen option: **1**, the way people are used to editing wiki pages. Option 2 is the strongest alternative, as it shows exactly what readers will see; but the editor already shows the formatted text, so the preview would show it twice and halve the width for writing. Option 3 hides all sections but one.

- **Reading by default:** `#/item/KEY` shows the body read-only. Commenting on a passage works while reading (ADR-0047), and so do the fields, comments, files, relations and access in the side column for whoever may use them.
- **Editing:** `#/item/KEY?edit` edits title and body in place in the live session; *Done* sends what is not yet saved, gives up every block and shows the page for reading. A newly created item with a body opens for editing, as one creates it to write in it.
- **Side column, in this order:** owner and details (fields, workflow steps, labels), comments, files, relations, access, history, then moving and trashing. On narrow screens it follows the text.
- **Naming:** the fields' section is called *Owner and details* (*Eigner und Details*): pages carry only their owner there; tasks also their assignee, due date and priority.
- **Access at a glance:** open is the normal case: the access card shows an open lock on calm green, "Open: everyone with a role in the space". A restriction, for sensitive content (ADR-0022, ADR-0040), shows a closed lock on amber, and the title carries the closed lock too.

### Consequences

- Good: reading is calm; editing is a deliberate step, and leaving it saves.
- Good: what belongs to the text sits beside it.
- Bad: one more click before writing in an existing page.
- Bad: while reading, others' live edits are not shown until the page is opened again; the read view shows what was saved.
- Neutral: the history and its comparison of versions sit in the narrower column.

## Links

- amended by [ADR-0079](0079-owner-and-details-lead-the-main-column.md) (owner and details lead the main column)
- amended by [ADR-0102](0102-owner-and-details-beside-the-text-sign-in-centred.md) (owner and details beside the text again)
