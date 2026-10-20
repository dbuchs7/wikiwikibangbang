# ADR-0067: Copying pages with their child pages, and moving them by a form

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that pages with their child pages can be copied and moved, not only dragged, 2026-10-20)

## Context and problem statement

Pages form a tree (ADR-0041): child pages are created below a page, and pages are reordered and moved by dragging in the tree or by the up and down buttons. Moving to another space or far up the tree by dragging is awkward, and a page cannot be copied at all, though a page with its child pages is a natural template. What is a copy, and how is a page moved without dragging?

## Decision drivers

- A copy is new content: a new item with its own identity (D2) and history; nothing links it to its source except where a file came from (ADR-0055).
- A copy is never more open than its source.
- A copy reads the source with the caller's rights and creates with them; it shows nothing the caller may not read.
- A copy of a page into its own subtree must end.

## Considered options

1. **A server-side copy of the page and, if asked, its readable child pages; moving by a form over the existing move.**
2. **A copy in the browser, through the existing create, upload and body routes.**
3. **A copy that shares the source's files.**

## Decision outcome

Chosen option: **1**.

- **Copy:** `POST /api/items/{ref}/copy` with space, parent, title and `children`. Each page copied becomes a new item of the same type under the same title (the first under the given one), with its properties, labels and access restriction, its files and its body. Comments, history, watches and the workflow state are not copied; a copy starts in its type's initial state.
- **Files** are copied as new files of the copy, each naming its source (ADR-0055); images in the body are pointed at the copies.
- **Subtree:** the readable child pages are listed before anything is made, so copying a page below itself copies the subtree as it was; at most `WWBB_BUDGET_PAGE_ITEMS` pages per copy.
- **Rights:** the caller must read the source and create at the target; a viewer may copy into a space where they edit.
- **Move:** a form names the target space and parent page and uses the existing move, which carries the subtree and refuses a move into the page's own subtree. Dragging in the tree stays.
- **UI:** *Copy…* and *Move…* in the item's side column.

### Consequences

- **Positive:** templates of page trees, and moves across spaces without dragging.
- **Negative / trade-offs accepted:** a copy is not atomic across pages: when one page fails (a reference the caller may not read), the pages made so far stay, and the answer says how many.
- **Negative / trade-offs accepted:** references in the body to other pages of the copied subtree still point at the originals.

## Pros and cons of the options

### Option 2: copy in the browser

Strongest form: no new route.

- Bad: dozens of requests for a tree, every file downloaded and uploaded again, and a half-made copy when the browser closes.

### Option 3: shared files

Strongest form: no duplicated bytes.

- Bad: files are encrypted per item and erased with it (ADR-0026, ADR-0045); sharing would tie the copy's files to the source's life.

## Links

- relates to ADR-0041 (page tree), ADR-0045 (files), ADR-0055 (copied files carry their source)
