# ADR-0111: Protecting, removing and copying respect the whole page tree

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer reported that protecting and deleting pages in a space did not work cleanly and asked for a review of the space administration, in particular that only space admins reach a space's settings, 2026-10-23)

## Context and problem statement

A review of the space administration found that a space's settings were already for space admins alone, on the server and in the UI. Protecting, removing and copying pages, however, did not treat a page's tree consistently:

- Protecting a page (ADR-0022) checked edit rights on that page only, yet every page below inherits the restriction. An editor could lock someone out of their own protected subpage, or lift a protection over pages they may not edit.
- Trashing or moving a page holding a subpage the actor may not see was refused with an error that named that subpage's key, and a page still referenced was refused with the keys of every referrer, hidden ones included. Both break D10.
- Restoring a trash batch (ADR-0021) checked the page the trash started from, not the pages trashed with it.
- A copy (ADR-0067) carried only the source's own restriction. A page protected through a page above it lost that protection when copied elsewhere. And since a write does not roll back, a copy whose restriction failed stood unprotected in the space.
- A space admin could revoke the last space admin role of a space, their own included.
- The UI offered trash, restore and purge where the server would refuse them.
- The broken-link report showed addresses from passages the reader may not read (ADR-0088).

## Decision drivers

- A restriction narrows for everything below it (D9), so changing it touches everything below.
- An item one may not read behaves as if it did not exist (D10), in error messages too.
- A refused command leaves nothing behind.
- The UI offers what the server allows.

## Considered options

1. Check the whole tree wherever a command affects it, and tell the UI what is allowed.
2. Keep the checks as they are and document the gaps.

## Decision outcome

Chosen option: **1**, because each gap lets a page be seen or changed by someone its protection is meant to keep out.

- **Protecting** a page needs edit rights on every live page below it, as moving and trashing do (ADR-0022). A space admin may always.
- **Errors** name a page below, or a referrer, only when the actor may read it. Otherwise they say that the tree holds pages the actor may not see, or how many referrers are hidden.
- **Restoring** needs edit rights on every page of the trash batch.
- **Copying:**
  - A page protected through a page above it is copied only to a place below that page.
  - The restriction a copy will carry is checked before anything is made.
  - The copy is restricted right after it is made, before labels and protected passages are copied.
- **Space admins:** a space admin cannot revoke or lower the last space admin role of a space. A group counts only while it has members. An administrator, who manages every space, can.
- **UI:** an item answers `may` with the removals the server allows (`trash`, `restore`, `purge`), and the page offers only those.
- **Broken-link report:** a link in a body block is listed only while the reader's veiled text still holds its address.

### Consequences

- **Positive:** a page's protection holds through copies and changes above it; refusals reveal nothing; the buttons match the rights.
- **Negative / trade-offs accepted:**
  - An editor can no longer protect, trash or move a page that holds a subpage closed to them. A space admin has to.
  - A copy of a page protected from above must go below the protecting page. A refused copy says so.
- **Follow-ups / risks to watch:** none; space deletion stays with administrators (ADR-0069).

## Pros and cons of the options

### Option 2: document the gaps

Strongest form: the gaps need an editor acting against a colleague, and the history records every change.

- Good: no behaviour changes for editors.
- Bad: the history records a leak, it does not undo it; a protected page's title and fields reached the whole space through a copy.

## Links

- amends [ADR-0022](0022-permission-model.md) (protecting needs the subtree), [ADR-0021](0021-deletion-only-when-unreferenced.md) (restoring and the referrers named), [ADR-0067](0067-copying-pages-with-their-child-pages.md) (copies keep inherited protection), [ADR-0048](0048-roles-are-listed-and-taken-back.md) (the last space admin)
- relates to [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages)
