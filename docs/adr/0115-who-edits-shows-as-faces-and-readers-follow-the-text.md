# ADR-0115: Who edits shows as faces, and readers follow the text

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that when several people edit a page or a task at once, who edits shows as round symbols with their initials, as in atlas, and that changes show live for everyone; they chose that readers see it too, 2026-10-23)

## Context and problem statement

A body is edited in a live session (ADR-0010). People editing at once see each other's typing, and a block someone holds is marked with their name. Who else was in the session showed only as a line of text above the body ("In this body now: …"). Someone only reading the page saw neither who edits nor the changes until they reloaded.

How do editors and readers see who edits now, and how do readers see the text change?

## Decision drivers

- Faces as elsewhere in the product: the profile picture, or the initials (ADR-0050).
- Readers must take no room from editors: a session holds at most `LiveClients` clients and the server at most `LiveSessions` sessions.
- A reader edits nothing, even when they may.
- A reader sees only what they may read: the session is open only to those who read every protected passage (ADR-0088), as for editors.

## Considered options

1. Readers *watch* the live session: they receive its events, are no participants and send no operations. The page shows the editors as faces beside the bell, and redraws a reader's text with every version saved.
2. Readers join as editors with a read-only editor and see every keystroke.
3. Readers poll the item.

## Decision outcome

Chosen option: **1**.

### Server
- `GET /api/items/{ref}/live?watch=1` joins as a watcher (`Hub.Watch`).
- A watcher receives every event but is no participant: presence lists editors only. Its operations are refused (403), even when it may edit.
- **Limits:** watchers count apart from editors, each up to `LiveClients`. A watcher does not open a new session while fewer than a quarter of `LiveSessions` is left.

### Page
- **Faces:** the editors other than oneself show as round faces (profile picture or initials) beside the bell, each named "… is editing". Editors see them in place of the old line of text, and readers too.
- **Readers' text:** for a reader, each version someone else saves redraws the text, without a reload. A selection made to comment or note is kept until it is let go.
- **Editors' text:** editors see each other's typing as before.

### Fixes found on the way
- **Waiting for an answer:** a client noted a lock or release only after the server answered the request. The server's event can come first, and then the client waited for it forever, showing "Saving…". It now notes the operation before it sends it.
- **Releasing a lost block:** the server now answers a release of a block the client no longer holds (its lock lapsed, or it was taken over) with who holds it.
- **Nothing left to send:** when a scheduled send found nothing left to send, the client did not clear the "Saving…" it had shown for it. It now does.

### Consequences

- **Positive:** everyone sees at a glance who works on a page; readers no longer read a stale text.
- **Negative / trade-offs accepted:**
  - Each open reading page holds a stream, as an editing page does.
  - Readers see a change when it is saved, that is when its editor leaves the block, not keystroke by keystroke.
- **Follow-ups / risks to watch:** none.

## Pros and cons of the options

### Option 2: readers as read-only editors

Strongest form: readers see every keystroke, exactly as editors do.

- Good: the most "live".
- Bad: the reading view's own tools (comments, notes, protecting passages) would have to move into the live editor; readers would count as participants.

### Option 3: polling

Strongest form: no long-lived connections.

- Good: simple on the server.
- Bad: delay and load grow with readers; no presence.

## Links

- amends [ADR-0010](0010-real-time-collaboration-with-block-locks.md) (live sessions)
- relates to [ADR-0046](0046-watching-and-notifications-from-the-log.md) (the bell), [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages)
