# ADR-0162: An access request becomes a task for the page's owner

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-02
- **Deciders:** Project initiators. The maintainer reported (2026-11-02), on the card *Protected passages* of a protected page showing "atlas-shop asks to read it." with Grant and Decline: "The release (access grant) of protected passages on the right side is not optimal. Rebuild it so that a task is created in Tasks, assigned to the owner of the page." They chose (2026-11-02):
  1. **Assignee:** the page's owner — the item's user field named `owner`, as the ready-made page type has it — if set; the owner thereby also decides about the page's protections (grants and declines). If the owner field is empty, the task goes to the one who protected the content.
  2. **Where:** a real task item in the same room as the page, as a child of the page. If there is no type that can hold a task (a type with a workflow and a user field `assignee`), the ready-made task type is provided the way the type templates provide it, and the task is then made.
- **Open question:** when the owner field of a page changes while a request waits, should its open task follow to the new owner, and its restriction with it? This record leaves the task where it is (see *Consequences*).
- **Question checked:** 2026-11

## Context and problem statement

ADR-0161 made a request to read a protected passage or protected blocks (ADR-0088, ADR-0160) a view: those who decide — the one who protected it and the room's administrators — found it under *Tasks → Access requests*, computed from the `SecretRequested` events, and answered it there or in the page's card. Nothing was stored for it.

In use, the maintainer found this not good enough: answering sat in a list of its own beside the work, and in the right-hand card of the page. The one who answers for a page — its owner — was not involved at all, and a request could not be planned, discussed, put on a board or found with the other open work. They asked for a real task, assigned to the page's owner.

How does a request to read protected content become a task, made for one who may only read the room, without the task telling the room what was asked for, and without the task and the request drifting apart?

## Decision drivers

- **One model (D1):** a task is an item like any other; the request stays the events on the protected item, and the task carries the link to it.
- **Validated changes (D3):** the task is made by validated commands; its state changes only by its workflow's transitions, the decision's as well as a person's.
- **No type-specific code (D6):** the type of the task, its assignee and the owner are found through type definitions — "a type with a workflow and a user field named assignee", "the item's user field named owner" — never by a type's name.
- **Unreadable is non-existent (D10), access only narrows (D9):** the task reveals that someone asked for a protected part of a page; only those it concerns read it. It never holds the protected content.
- **Readers ask too:** asking must work for those who only read the room, with no right to create anything in it.
- **The persisted format is a contract (ADR-0025):** linking a request to its task changes an event; the codec version rises and older events are still read.

## Considered options

1. **A task item per request, made by the server in the request's batch, below the page, assigned to the owner or the one who protected it, restricted to those it concerns; deciding finishes it.**
2. **Keep the list of ADR-0161** (a view in Tasks, nothing stored), and only move the buttons off the page's card.
3. **A task per request, made by the requester's browser** through the ordinary item API.

## Decision outcome

Chosen option: **1**. It supersedes ADR-0161's choice of a list in which nothing is stored, and amends ADR-0088 on who decides.

### The task

- **Made with the request.** `RequestSecret` records `SecretRequested` and the events that make the task in one batch: `ItemCreated` (in the page's room, the page as parent), `BodyCommitted`, `LabelsSet`, `RestrictionSet` and `Watched` for the assignee and the one who asks. A failed write makes neither. One who may only view the room asks as before: the server makes the task on their behalf, as a trusted command inside their session's command, which needs no create right.
- **Attributed to the mechanism.** The task's events have the actor `access-request:<requester>`, as the synchronisation of an engine acts as `engine:<id>` (ADR-0151): the history says it was made by the access-request mechanism, for that person. `SecretRequested` keeps the requester as its actor. The UI names the actor "access request for bert".
- **Linked.** `SecretRequested` carries `task`, the ID of the task made with it. The state keeps, for each request, its task, and for each task, its request, derived from the log alike live and on replay. This is the only format change: **codec version 39**; versions 1 to 38 are still read, and their requests have no task.
- **Type.** The type is chosen by its definition: it has a body, a workflow, a user field named `assignee`, no other required field, and a transition that finishes an item from the initial state (the workflow's last state preferred, ADR-0082). The ready-made types (`page`, `task`, `decision`) are now kept by the server (`item.TypeTemplates`, `GET /api/types/templates`) and offered to administrators from there, so there is one definition; the first of them that can hold the task is *the* template. Its type is used if it is defined and can hold the task. Otherwise the other type that can and is most like the template (the most of its fields by name and kind and of its states, then by name) is used. With none, and the template's name free (never defined, or removed, ADR-0094), the template is defined in the same batch, by the mechanism, as its next version. With none and the name taken by a type that cannot hold it, the request is made **without a task** and is answered from the page's card, as before. Types are installation-wide, so "the room has a task type" means the installation has one.
- **Assignee.** The item's owner — the value of its user field named `owner` — if set, if the owner reads the item (role in the room and no restriction excluding them, ADR-0022) and if the owner is not the one who asks. Otherwise the one who protected the content (`Secret.By`), even if they can no longer read the item; the room's administrators read and answer the task then.
- **Content.** The title says "Access request: bert for «Salaries»", in German "Freigabe-Anfrage: bert für «Gehälter»". The text says who asked, on which date, which protection by number and kind ("protected blocks 1") with a mention of the page, to whom it is assigned and why, that the content is not shown, and how to answer. It never holds the protected content: titles hold no protection (ADR-0088), and the number and kind are what the page's card shows everyone. The words are in the language the assignee chose, else the requester's, else English, from the server's message catalogue (ADR-0017); they are content, written once, not translated later.
- **Label** `access-request`, so boards (ADR-0036, ADR-0060, ADR-0139) and the task list filter the tasks.
- **Who reads it.** The task is restricted, for reading and editing, to the assignee, the one who asks, the one who protected the content and the owner if they read the item; the room's administrators read it as they read everything (ADR-0022). It is set in the same batch, so the task is never readable by others in between. It only narrows (D9): the page's own restrictions, inherited along containment, still apply; no one gains access to the page or the room through it. Everyone else in the room learns nothing of it: not in lists, search, the tree, the starmap, relations or notifications (D10). The room's administrators are not named on it: their grants are shown to administrators only (ADR-0161).

### Who decides

ADR-0088's deciders — the one who protected the content and the room's administrators — keep every power over the protection. **The item's owner answers requests** (grant, decline), for others' requests only, and only while they read the item. The owner does not change the readers, take the protection off, erase it, or answer their own request: the owner field is set by whoever edits the item, so a wider power would let any editor read any protection on a page they own. For the same reason, while an item holds protections its owner field is set only by those who read every protection or administer the room — the rule that already guards the item's text (ADR-0088). The owner is told of requests as the deciders are.

### Deciding

- **From the task.** The task page shows a card *Access request*: who asks, since when, which protection of which page (a link), and, to those who answer it while it waits, **Grant** and **Decline**, which call the answer of ADR-0088 (`POST …/secrets/{id}/requests/{principal}`). An answered request's card says so.
- **Wherever it is answered** — the task, the page's card for a request without a task, the API — the answer's batch also records a comment on the task by the one who answers ("Granted by anna.", "Declined by anna.") and finishes the task by the transition that finishes it from its state. Changing the readers so that the one who asked reads it counts as granting; taking the protection off or erasing it ends the task with a comment that nothing is left to decide. A task no longer live (in the trash) is left as it is.
- **Closing the task without deciding declines the request.** A transition of the task to a finished state while its request waits records `SecretDeclined` and a comment in the same batch, and the one who asked is notified as of any decline. Only those who answer the request may do it; anyone else who may edit the task (an asker who edits the room) is refused while it waits. Other transitions are free. A task reopened after its request was answered is an ordinary task.
- **Again.** Only one request per person and protection waits at a time, so there is at most one open task for it; asking again while it waits does nothing. After a decline, asking again makes a new request and a new task.

### The page and Tasks

- **The page's card** no longer holds Grant and Decline for a request with a task: it says "bert asked on 02.11.2026 → task VAULT-7", linking the task, to those who answer it (the task key only where they read the task). A request without a live task — made before this change, made where no type could hold a task, or whose task is in the trash — keeps Grant and Decline in the card.
- **The one who asked** sees "You asked on …; the task is assigned to anna: task VAULT-7" where they read the task, and the dialog that sent the request names the task and its assignee.
- **Tasks.** The tab *Access requests*, its card and the start page's counts of ADR-0161 are removed: the tasks are in the ordinary task list (*Assigned to me*, filtered by the label) and on boards, and the start page counts them as tasks. `GET /api/me/access-requests`, `Session.AccessRequests` and the MCP tool `list_access_requests` are removed; agents find the tasks as any task (`query_items`, `find_items` by label).
- **Notifications.** No assignment notification exists in the product (ADR-0046); the assignee — always one who answers — is told of the request by the existing request notification, which now goes to the owner too, and watches the task, so they hear of the asker's comments, and the asker of theirs. The one who asked is told of the answer as before.

### Requests made before

Open requests made before this change have no task. They are **not** given one at startup: a migration that writes events on opening would have to be explicit, idempotent and run once, and would make tasks on behalf of people who asked under other rules, possibly days ago. They stay answerable in the page's card with Grant and Decline, and the request notification already told those who decide. This is the simplest correct choice; apply stays deterministic and free of side effects.

### Consequences

- **Positive:** a request is work like other work: assigned, on boards, commented, in history, found with the other open tasks; the page's owner answers for their page.
- **Positive:** the task and the request cannot drift: every way a request ends finishes its task in the same batch, and finishing the task while it waits ends the request.
- **Positive:** readers ask without any create right; the room learns nothing of the request.
- **Negative / trade-offs accepted:** a stored object per request, and a format change (codec 39). Kanbans and boards of the room show the tasks to those who read them.
- **Negative / trade-offs accepted:** the task's restriction is a snapshot of who it concerned when it was made. A new owner, or a new administrator, does not join it unless the restriction is changed by hand; administrators read it anyway.
- **Negative / trade-offs accepted:** in an installation whose only type of the template's name cannot hold a task, requests keep being answered from the page's card; an administrator adds the missing fields to make tasks.
- **Negative / trade-offs accepted:** the task's words are written in one language once, as content.
- **Follow-ups:** move an open task to a new owner (open question); a message with a request, now naturally a comment on the task (ADR-0161's open question); an assignment notification for any user field.

## Pros and cons of the options

### Option 2: keep the list of ADR-0161

Strongest form — it was the previous choice, and it holds: the list cannot disagree with the requests because it is read from them; nothing new is stored, no format changes, and no item is made on someone's behalf with all the questions of type, place and visibility that raises. Boards and the room's task lists stay free of access requests, and a request leaves the list exactly when it is answered. Moving the buttons off the page's card would have answered the screenshot.

- Bad: it does not do what the maintainer asked: the page's owner gets nothing, and a request is still not a task — it cannot be assigned, planned, commented or found with the other work.
- Bad: the list is computed by walking the items on every request, and a second list of open work beside the task list stays.

### Option 3: the browser makes the task

Strongest form: no server change beyond the link; the UI already makes tasks.

- Bad: the one who asks may only read the room and cannot create the item; the request and the task would be two commands that can fail apart; the restriction would be set after the task is readable.

## Links

- supersedes [ADR-0161](0161-access-requests-are-answered-from-the-task-list.md) (its list of waiting requests, in which nothing is stored; its dialog stays)
- amends [ADR-0088](0088-protected-passages-of-a-body.md) (the item's owner answers requests; the owner of an item with protections is set by those who read them all)
- relates to ADR-0160 (protected blocks), ADR-0022 and ADR-0078 (restrictions), ADR-0036, ADR-0060 and ADR-0076 (boards and kanbans), ADR-0046 (notifications), ADR-0082 (finished states), ADR-0094 (removed types), ADR-0151 (attribution of what a mechanism makes), ADR-0025 (the persisted format), ADR-0012 (MCP adapter), ADR-0017 (languages)
