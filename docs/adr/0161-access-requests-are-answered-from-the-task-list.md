# ADR-0161: Access requests are answered from the task list

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-02
- **Deciders:** Project initiators. The maintainer asked: "When I come across a protected area, one click on the protected element should create a task for the release (access grant) addressed to the owner. Build it in, well explained for the user." They chose (2026-11-02): the request appears for those who decide as an entry of its own in *Tasks* and on the start page, with *Grant* and *Decline* right there; it is not a stored page or item but a view of the open requests the viewer may decide, which leaves the list when answered; and it works for readers without the right to edit in the room.
- **Open question:** should a request carry a short message from the one who asks ("Why do you need it?")? It is content, so it would be sealed with the item's key, and the event that asks would change its persisted form (a new codec version); this record does without it.
- **Question checked:** 2026-11

## Context and problem statement

Protected passages (ADR-0088) and protected blocks (ADR-0160) show a reader who may not read them a placeholder that names who decides and offers to ask. Asking records `SecretRequested` on the item; those who decide — the one who protected it and the space's administrators — get a notification, and answer in the *Protected passages* card of the item's page.

Two gaps showed in use. A veiled block inside an embedded item had no button at all: the page knew only its own protections, so the embed's bar could neither name who decides nor ask. And a request lived only as a notification: once read, it was found again only by opening the right item. Those who decide had no list of the requests that wait on them.

How does one click on a protected element ask for access, and where do those who decide find and answer what waits on them?

## Decision drivers

- **One model (D1):** a request already exists as events on its item; nothing about it may live in a second place that can drift from them.
- **Unreadable is non-existent (D10):** a list of requests names only items the viewer reads, and only requests the viewer decides.
- **Readers ask too:** asking must work for those who only read the room; it needs no edit right and makes nothing in the room.
- **Explained:** whoever asks learns what is protected, who decides and what happens next; whoever decides learns who asks for what, without the list showing the protected content.
- **No format change** unless it buys something the existing events cannot give.

## Considered options

1. **A view of the waiting requests in Tasks and on the start page, computed from the items, answered in place; a dialog on every veiled element.**
2. **A real task item per request,** made in the room, assigned to the one who protected the content, finished by granting or declining.
3. **Notifications only** (the status quo).

## Decision outcome

Chosen option: **1**.

### The dialog

- Every veiled element is a way to ask: a veiled passage is a button with an accessible name, the bar of a veiled block holds one, and a click on the bar itself does the same. It works for the item's own protections and for those of every item it embeds, whose bars ask at the embedded item; the page keeps the protections it shows, its own and its embeds', by ID.
- The dialog says what is protected — "Protected passage n" or "Protected blocks n" of the item, numbered as the item's card numbers them — that only the people chosen by whoever protected it read it, and who decides: the one who protected it, by name, and the administrators of the room, by the room. It does not list the room's administrators by name, as a room's grants are shown to its administrators only.
- *Ask for access* sends the existing request. The dialog then explains what follows: those who decide get a task in their Tasks list and a notification; the one who asked is notified of the answer, and the content then appears where the bar was.
- Clicking again while the request waits says since when it waits, and for whom. A request cannot be withdrawn, as no event withdraws one; the dialog offers none.
- The dialog is keyboard accessible (focus on its button, Escape closes it) and works on touch.

### The list

- `GET /api/me/access-requests` answers the waiting requests the caller decides, across rooms: for each, the item (ID, key, title, room), the protection (its ID, its number among the item's protections, whether it protects blocks), who asked and when. It reads them through `Session.AccessRequests`, which looks only at live items the caller reads and at protections the caller decides; oldest first, paged within the listing budget `PageItems` (ADR-0015), with a cursor (the event that asked) and the total.
- **When** a request was made is the time of its `SecretRequested` event, kept with the request in the state (live and on replay alike). Nothing is added to the persisted format.
- *Tasks* has a tab *Access requests* with their count; on the other tabs a card shows them while any wait. Each entry names who asks, links the item, says which protection and when, and offers *Grant* and *Decline*, which call the existing answer of ADR-0088. An answered request leaves the list because it no longer waits — nothing is marked done. The list never shows the protected content. An empty tab says that nothing waits.
- The start page counts them on the Tasks tile and in a notice that leads to the tab, and the *My tasks* button goes there while any wait.
- The MCP adapter offers the list as a read, `list_access_requests`; granting and declining stay unoffered, as they change who reads (ADR-0012, ADR-0088).

### No message with a request

A free-text note ("Why do you need it?") was considered. It is content (ADR-0026): it would have to be sealed with the item's key and erased with it, so `SecretRequested` — today an event without content — would become one with sealed content, a change of the persisted form that needs a new codec version and its tests. The note would also be a second channel of conversation beside comments. Neither is needed for the maintainer's request, so a request carries no message; the open question above records it.

### Consequences

- **Positive:** a request is answered where open work is, across rooms, by one click; the list cannot disagree with the items, since it is read from them; a veiled block in an embed asks like any other.
- **Positive:** no new event, no format change, no new stored object, no new permission path: who sees an entry is who decides the protection and reads the item.
- **Negative / trade-offs accepted:** the list is computed by walking the items on each request; for a very large wiki that is a scan, as the bell's findings are (ADR-0081). A derived index is the follow-up if it shows.
- **Negative / trade-offs accepted:** the one who asks cannot say why, nor withdraw the request; those who decide cannot ask back except by a comment.
- **Follow-ups:** a message with a request (open question); withdrawing a request; a reminder for requests that wait long.

## Pros and cons of the options

### Option 2: a real task item per request

Strongest form: a task is what the maintainer asked for, and a task item gets everything tasks have — an assignee, a due date, a place on kanbans, comments for asking back, history — with no new list in the UI.

- Bad: it is a second home for the request (D1). The item and the events on the protected item could drift: a task finished without an answer, an answer given in the item's card that leaves the task open, a task left behind when the protection is taken off or erased.
- Bad: rights do not fit. The one who asks may only read the room and could not make the item; the item would have to be made by the server on their behalf, in a room and type the system picks, and restricted so its title does not tell the room what was asked for.
- Bad: it fills kanbans, boards and the task list of everyone in the room with access requests, and couples finishing a task to granting access.

### Option 3: notifications only

Strongest form: nothing new; those who decide are already told, and the item's card already answers.

- Bad: a notification read is gone from sight; there is no list of what still waits, and the veiled block of an embed offers nothing.

## Links

- amends [ADR-0088](0088-protected-passages-of-a-body.md) (asking from every veiled element, the list of waiting requests)
- amends [ADR-0160](0160-protected-blocks-and-pictures-of-a-page.md) (veiled blocks of embedded items ask too)
- relates to ADR-0036 (a board is a view, not a stored object), ADR-0049 (the task inbox), ADR-0061 (account requests decided under the tasks), ADR-0046 (notifications), ADR-0015 (paged listings), ADR-0012 (MCP adapter), ADR-0022 (permissions)
