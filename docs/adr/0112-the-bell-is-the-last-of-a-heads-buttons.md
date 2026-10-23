# ADR-0112: The bell is the last of a head's buttons, and the tree makes no pages

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked to move "New page" out of the left column into a button beside the bell, to put "New subpage" and "Export" left of the bell too, and to keep the bell always rightmost, 2026-10-23)

## Context and problem statement

A space's page and an item's page each have buttons in their head (ADR-0091). The bell to watch (ADR-0046) stood first on an item and in the middle on a space. The page tree in the left column (ADR-0041) also had a "+ New page" link. On a space it did what the head's "New page" button does. On an item it did what "New subpage" does. Where do the head's buttons stand, and where does one make a page?

## Decision drivers

- The same thing in the same place on every view.
- One way to make a page from a view, not two.
- The tree is navigation.

## Considered options

1. The head's buttons, then the bell last; the tree without a "New page" link.
2. The bell first, then the buttons.
3. Keep both ways to make a page.

## Decision outcome

Chosen option: **1**, as the maintainer asked.

- **A space's head:** "New page", the CSV export, then the bell.
- **An item's head:** "Done" (for an item without a text, while editing), "New subpage", "Export", then the bell.
- **The page tree:** it has no "New page" link any more. The head's buttons make pages.

### Consequences

- **Positive:** the bell is found in one place; making a page is offered once, where the other actions are.
- **Negative / trade-offs accepted:** on a long item page the tree no longer offers a new page at the top of the space; one goes to the space, or uses "New" in the top bar.
- **Follow-ups / risks to watch:** none.

## Pros and cons of the options

### Option 2: the bell first

Strongest form: the bell is the one control every reader has, so it would stand in the same spot whatever else is offered.

- Good: steady position for readers.
- Bad: the maintainer asked for it rightmost; the actions an editor uses most would follow it.

### Option 3: keep both

Strongest form: the tree's link is near where one looks for pages.

- Good: one click from the tree.
- Bad: two controls for one action, one of them easily read as a page of the tree.

## Links

- relates to [ADR-0041](0041-page-tree-and-sibling-order.md) (the page tree), [ADR-0046](0046-watching-and-notifications-from-the-log.md) (the bell), [ADR-0091](0091-rules-every-view-follows.md) (actions in the head)
