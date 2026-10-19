# ADR-0046: Watching items and spaces; notifications are read from the event log

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer required that pages can be subscribed to, 2026-10-19)
- **Open question:** should notifications also be sent by e-mail, and how often (each change, a daily digest)? That needs an SMTP server, credentials and rules against floods; this record builds the in-app part both need.
- **Question checked:** 2026-10

## Context and problem statement

People must learn what changed on the pages they care about without opening them. A wiki offers this as *watching*: a page, or a whole space, and a list of what others changed there.

Every change is already an event in the log, with its author and time (ADR-0008). What is missing is who watches what, and how far each person has read.

## Decision drivers

- One source of truth: a notification must not say something the log does not, or miss what it does.
- A notification shows only what its reader may read now (ADR-0022): a page restricted after a change must not leak its title through an old notification.
- Watching is not content: it must not appear in an item's history or survive as someone else's information about the watcher.
- No per-notification storage that grows with every change times every watcher.

## Considered options

1. **Watches as events of the item store; notifications computed from the log for each reader; a read mark per account**
2. **A notification inbox per user**, written when a change happens
3. **Watches in the accounts**, notifications computed as in 1

## Decision outcome

Chosen option: **1**.

- **Watches:** `Watched` and `Unwatched` events, each naming a principal and an item or a space (codec version 9). They touch no item, so no item's history shows them. Watching an item needs the right to read it; watching a space needs a role in it.
- **Automatic watching:** whoever creates an item through their session (the UI, the API, an API token, the MCP adapter) watches it, recorded in the same batch as the creation, so an item never exists without its creator's watch. One can stop watching it like any other. An import does not watch: its author is the importing administrator, who would otherwise watch every imported page. Editing does not start a watch either: a single fix of a typo would subscribe the editor to the page for good, and the creator, who answers for the page, is already told.
- **Notifications:** for each reader, the events after their watch began, of the kinds that change what people see (created, renamed, edited, a field or the state changed, moved, trashed, restored, commented, labels changed, a file attached, imported), by someone else, on an item they watch, an item of a space they watch, or a new item directly below an item they watch. Only items they may read now count, and the item's current key and title are shown. Nothing is stored per notification; the log is read newest first, and the reading stops at the oldest watch of the reader.
- **Read mark:** the accounts journal records, per user, up to which event they have seen their notifications (`NotificationsSeen`); it only moves forward. Opening the list marks everything shown as seen.
- **API:** `PUT`/`DELETE /api/items/{ref}/watch` and `/api/spaces/{space}/watch`; `GET /api/me/watches`; `GET /api/notifications` (newest first, paged with `before`, with an unread count up to 100); `POST /api/notifications/seen`.
- **UI:** a *Watch* / *Stop watching* button on every item and space; *Notifications* in the navigation, with the unread count; a list of notifications, newest first, unread ones marked, and of what one watches.

**Implementation is Partial:** no e-mail (the open question), no automatic watching of pages one edits, no notification of mentions.

### Consequences

- Good: notifications cannot disagree with the history; a restriction or a purge hides them at once.
- Good: watching costs one event; the log does not grow with the number of watchers.
- Bad: listing notifications reads the log back to the oldest watch; for someone who watches a busy space for years, that is a long scan. A cursor per reader (option 2's inbox, as an index) can come when it matters.
- Bad: an event on an item one watches through both the item and its space is listed once, but a change and a later rename show the item's current title in both.

## Pros and cons of the options

### 2. An inbox per user

- Good: listing is a lookup.
- Bad: every change writes one entry per watcher; entries can disagree with later restrictions and erasure, which must then find and remove them.

### 3. Watches in the accounts

- Good: per-user data stays with the user.
- Bad: the item store, which decides readability and holds the log, would have to ask another journal for every event; the two journals would need to be read consistently.

## Links

- ADR-0008 (the log), ADR-0022 (permissions), ADR-0026 (erasure), ADR-0027 (accounts), ADR-0035 (comments)
