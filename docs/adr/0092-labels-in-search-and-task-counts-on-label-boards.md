# ADR-0092: Labels in search, and task counts on the boards by label

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators (the maintainer asked that the number beside each board by label counts only the tasks carrying the label, that labels can be searched from the search at the top, and that labels and items are suggested there while typing, 2026-10-22)

## Context and problem statement

The Kanbans page lists a board for every label (ADR-0087). The number beside each label counted every readable item with it, pages included, as ADR-0087 accepted. A board by label shows only tasks, so the number promised cards that were not there, and a label of pages alone led to an empty board.

The search at the top (ADR-0033) looked in titles, keys, text, properties and comments, but not in labels. Someone looking for "release" did not find the items labelled `release` unless the word also stood in them.

## Decision drivers

- A number beside a link says what the link will show.
- One search box finds what one knows of an item; a label is part of that.
- Labels stay item labels; no new kind of search syntax.

## Considered options

1. **Count tasks on the Kanbans page; search labels as words of an item, and offer the matching labels on the results page.**
2. **A search syntax such as `label:release`.**
3. **Keep counting every item, and say so in the text.**

## Decision outcome

Chosen option: **1**.

### Boards by label

- **Counting:** the Kanbans page counts, per label, the readable tasks: the items whose type has a workflow. These are the items a board by label shows.
- **Which labels show:** labels that only pages carry are no longer listed there.
- **Explanation:** the page says what the number counts.
- **API:** `GET /api/labels?tasks=1` gives these counts. Without it the counts stay those of every item.

### Search

- **Matching:** an item's labels are searched like its properties, so a word of a label finds the item. A word of a label adds to the score as a property does, below the title.
- **Labels on the results page:** the labels holding the word are listed above the hits, as links to their pages.
- **Labels on each hit:** each hit shows its labels.
- **Suggestions while typing:** under the search at the top, two groups are offered.
  - **Labels:** those holding what is typed, at most four, each with how many items carry it.
  - **Items:** the five that best answer the search, ranked as on the results page, each with its key, title and type.
  - Choosing a label opens the label's items; choosing an item opens it.
  - Arrow keys move through the list, Enter opens the chosen label, and Escape closes the list.
  - Without a choice, Enter searches as before.
  - The list is a listbox the search field controls, so screen readers announce it.

### Consequences

- **Positive:** the number beside a board is the number of its cards; the search box finds by label without a syntax to learn.
- **Negative / trade-offs accepted:** a common label can bring many hits for a short word. They rank below hits in the title, and the label's own link leads straight to its items.
- **Negative / trade-offs accepted:** each pause in typing runs a search, as submitting one does. It waits 200 ms after the last key and asks for five hits; the search scans the live state (ADR-0033, ADR-0081), so this costs one search per pause.
- **Negative / trade-offs accepted:** picture labels (ADR-0090) are not item labels and are not searched here; the album searches them.

## Pros and cons of the options

### Option 2: a search syntax

Strongest form: precise; no hits by accident.

- Bad: a syntax has to be known before it helps, and the results page offers the label's own page anyway.

### Option 3: keep counting every item

Strongest form: no change.

- Bad: the maintainer found the number misleading, which it is: it counts what the board does not show.

## Links

- amends [ADR-0087](0087-kanban-pictures-and-boards-by-label.md) (boards by label) and [ADR-0033](0033-full-text-search-by-scanning-the-live-state.md) (search)
- relates to ADR-0044 (labels), ADR-0072 and ADR-0083 (ranking), ADR-0090 (picture labels)
