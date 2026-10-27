# ADR-0062: Spaces carry an icon and show as tiles or a list

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that the start page show the spaces as tiles, switchable to a list, and that the tiles can carry an icon or logo, 2026-10-20)

## Context and problem statement

The start page shows the spaces as tiles with their key, name and the reader's role. With many spaces, a list is easier to scan; tiles are easier to recognise with a picture. Where does a space's icon live, who sets it, and where is the reader's choice of layout kept?

## Decision drivers

- An icon is presentation, not content: it has no history, no review, no relations (D1).
- Space administrators decide on their space; nobody else.
- Who cannot see a space must not learn of it through its icon.
- A reader's layout preference concerns that reader only.

## Considered options

1. **The icon is an asset of the site settings, named by the space's key; the layout is kept in the browser.**
2. **The icon is an event of the item store (`SpaceIconSet`), with a codec version.**
3. **The layout is a setting of the account.**

## Decision outcome

Chosen option: **1**.

- **Storage:** the site journal (ADR-0052) holds the icon as asset `space-<KEY>`. Space keys never change, so the name is stable.
- **Who:** space administrators, and administrators, set and remove it on the space's Members and roles page; the change is audited as `design.change` with the space.
- **Files:** PNG, JPEG, GIF, WebP or SVG, recognised by their bytes, at most `WWBB_BUDGET_BRANDING_BYTES`. It is served like a logo: as an image only, sandboxed, `nosniff`; only to callers with a role in the space, others get 404.
- **Listing:** `/api/spaces` names each space's icon by a content tag, so browsers cache it and fetch it again when it changes.
- **Layout:** tiles or list is the reader's choice, kept in the browser's local storage; without storage it lasts for the page.

### Consequences

- **Positive:** no codec change; an icon costs one site event.
- **Negative / trade-offs accepted:** the icon is not part of a space's history, and a replaced icon is kept in the site journal like every replaced logo.
- **Negative / trade-offs accepted:** the layout does not follow a reader to another browser.

## Pros and cons of the options

### Option 2: an item store event

Strongest form: the space and its look live in one place and are exported together.

- Bad: a codec version and golden files for presentation, and binary data in the item log, which is the content's history.

### Option 3: the layout on the account

Strongest form: the choice follows the reader everywhere.

- Bad: an account event for a view preference; the UI already keeps such choices in the browser.

## Links

- amended by [ADR-0145](0145-favourite-spaces-come-first-on-the-start-page.md) (favourite spaces first, as tiles and in the list)
- relates to ADR-0052 (corporate design), ADR-0041 (page tree)
