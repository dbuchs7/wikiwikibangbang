# ADR-0050: Profile pictures are kept in the accounts journal; the header has an account menu

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for an account menu behind one symbol and for profile pictures, with the initials where there is none, 2026-10-19)

## Context and problem statement

The header named the account and offered *Log out* and *Administration* as links; on a phone they took a row of their own. People also want to recognise each other by a picture. Where does a picture live, and who may see it?

## Decision drivers

- A picture is personal data of the account, not content of a space: it belongs with the account, not with an item, and is not crypto-shredded with one (ADR-0026).
- It must survive restarts and backups without a new kind of storage.
- No file a user uploads may run as a page of this site (ADR-0014).
- A phone photo is megabytes; a picture shown at 32 to 64 pixels needs kilobytes.

## Considered options

1. **An event of the accounts journal** carrying the picture (`AvatarSet`), scaled down by the browser before it is sent.
2. **A file beside the journal** (like attachments, ADR-0045), with its own healing and backup paths.
3. **An external service** (e.g. a hash-based picture service).

## Decision outcome

Chosen option: **1**. Option 2 is right for files of any size, but a picture scaled to 256 pixels is a few kilobytes, and an event needs no new storage path, healing or backup handling: it is in `accounts.wal`, replayed and backed up like every account change. Option 3 sends who uses this server to a third party.

- **Storage:** `AvatarSet` (user, picture, media type); an `AvatarSet` without a picture removes it. Older builds do not know the kind and refuse the journal, as with earlier additions to it.
- **Budget:** `AvatarBytes`, 256 KiB by default (ADR-0015); the browser cuts the middle square of the chosen image and scales it to 256 pixels as PNG, so the budget is not reached in practice.
- **API:** `PUT /api/me/avatar` with the picture as `application/octet-stream` (not sendable by a cross-site form, ADR-0027); its format is read from its bytes and must be PNG, JPEG, GIF or WebP, never SVG. `DELETE /api/me/avatar`. `GET /api/users/{id}/avatar` for every signed-in caller, with `nosniff`, a sandboxing CSP and an ETag. `GET /api/me` names the picture's content (`avatar`), so the UI asks for a new one when it changes.
- **UI:** the header ends with the notification bell and the account button: the picture, or the initials of the username when there is none. It opens a menu with the username and system roles, *Account and password*, *AI assistants (MCP)*, *Administration* for administrators, and *Log out*. The menu follows the ARIA menu pattern: arrow keys, Home and End move, Escape closes and returns to the button. The account page sets and removes the picture.

### Consequences

- Good: the header is search, *New*, bell and account on every width; on a phone it fits one row below the logo.
- Good: no new storage path; backups carry the pictures.
- Bad: *Log out* and *Administration* are one click further away. Sessions end by themselves; administrators reach the administration also through the module switcher.
- Bad: every change of a picture grows the accounts journal by the picture; replacing it daily for years would be noticeable. A compaction of the accounts journal would remove superseded pictures; there is none yet.
- Neutral: the pictures show in the header, on the account page, beside the authors of comments and beside each change in an item's history; `GET /api/users/names` names the pictures of the users it names (`avatars`), so a list asks for them in the request it makes anyway. Initials are drawn by CSS from an attribute, so they are no part of the name's text.
