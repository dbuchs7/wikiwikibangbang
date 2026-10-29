# ADR-0151: User tasks of a process engine become linked tasks, and the engine decides who completes them

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-28
- **Deciders:** Project initiators (requested 2026-10-28: user tasks of an atlas engine become tasks of the wiki with labels an administrator chooses, a page shows a table of tasks selected by a filter, and a card moved to done on a kanban completes the task, showing its form first where input is needed. The requester decided: the engine alone decides whether a person may work a task; each person connects their account to the engine once, by OAuth, with the mail address only a suggestion; groups serve only to decide who sees a linked task, never rights in a space; the coupling must work whatever people and groups come from — local accounts, single sign-on with any provider, a directory, the engine itself; and a linked task shows how current it is. The engine's side was built in the same step: pblumer/atlas ADR-0510, Tasks for a connected system); proposed for the maintainer's acceptance
- **Open question:** three points (see "Open questions"): whether administrators, whom restrictions do not bind (ADR-0022), may see every linked task; whether the form checked in the browser is enough while the engine checks only who completes, not what is submitted; and whether rules or a list of named processes decide which tasks are linked.
- **Question checked:** 2026-10

## Context and problem statement

ADR-0049 built the task inbox on the item model and left open whether the wiki should also show the user tasks of an atlas engine; it named what that needs: an engine address, a credential per person and a mapping of accounts. ADR-0148 named the same need as its third use — tasks listed by a filter, as cards or as a list, opened in a dialog — and deferred it to a record of its own. This is that record.

The requester asked for four things:

1. **A task appears.** When a user task becomes active in the engine, a task of the wiki is made, with labels an administrator chooses.
2. **A table in a page.** A page lists tasks selected by a filter, as the engine's own task app does.
3. **A board.** Tasks are moved on a kanban; moving one to done completes it in the engine, and where the task needs input, its form opens first.
4. **People are coupled.** The wiki's accounts and the engine's are matched, possibly through an external identity provider.

What the engine offers, as of pblumer/atlas ADR-0510, Tasks for a connected system (built 2026-10-28, proposed there):

- **Tasks.** `GET /api/v1/tasks` lists the open user tasks the caller may see, newest first; an operator sees all, a person sees the tasks addressed to them, offered to a group they belong to, or to nobody. `GET /api/v1/tasks/{key}` reads one; a task that ended answers 404. A task names its process, element, name, candidate groups, assignee (a username), form, priority and due date. `POST …/claim`, `…/unclaim` and `…/complete` (with `{"variables": …}`) work a task; completion records the caller as `completedBy` and is refused to a caller who may not work the task.
- **Forms and values.** `GET /api/v1/forms/{id}` answers a form-js schema; `GET /api/v1/instances/{key}/variables` the values a form opens with, scoped to the caller.
- **Facts.** The event feed (`GET /api/v1/events?after=`, scope `events`, CloudEvents) carries `atlas.task.created`, `.assigned`, `.unassigned`, `.completed` and `.cancelled` with the task's key, instance, element, process, candidate groups, due date and whether somebody holds it — never who.
- **A scope to list them.** A token of scope `tasklist` reads the list and one task and nothing else; an administrator's token lists every open task.
- **A grant to work them.** A person approves a client for the resource `<engine>/tasks`: authorization code with PKCE, access tokens for two hours, refresh tokens rotated on use, clients registered by the engine's administrator with their redirect addresses. The grant reaches the person's tasks, the three commands, the form, the values and `GET /api/v1/auth/me` — which answers the person's id, username, mail address and **groups** — and nothing else. Disabling or deleting the account revokes its grants.

What the wiki has: kanbans of a type and a label whose moves are transitions (ADR-0060, ADR-0076, ADR-0139); the task list by label in a text (ADR-0114); the query language (ADR-0136); form-js vendored for showing forms (ADR-0144) and sending start forms (ADR-0148); engines named by an administrator with a credential from the environment (ADR-0129); connectors that write items of a type on a schedule of hours, their items managed so that people cannot change them (ADR-0077, ADR-0127, ADR-0128); protection of items for people and groups (ADR-0078); single sign-on with one provider (ADR-0030).

The question: **how do the engine's user tasks become tasks people find, move and complete in the wiki, while the engine — and only the engine — decides who sees them and who works them?**

## Decision drivers

- **The engine decides** who sees a task and who works it (the requester). The wiki never works a task with a credential of its own.
- **Content is items** (D1): what is on a board, in a table or in the inbox is an item; a second kind of task object would be a parallel domain.
- **Any source of people and groups.** Nothing may depend on an identity provider both systems share, on a directory, or on a mail address.
- **Access only narrows, and unreadable is non-existent** (D9, D10). Search, queries, notifications and the change feed must hide a linked task from whoever the engine would not show it to, without code of their own (D6).
- **State changes through transitions** (D3); **a task that ends is never deleted** (D7, D8).
- **Credentials stay on the server** (ADR-0129), and a person's credential is content-grade secret: never in plaintext on disk, never logged or returned.
- **A copy shows its age** (the requester): every copy can be stale; the reader must see when it is.
- **Every new bound is a budget** (ADR-0015).

## Considered options

Where tasks live:

1. **Linked items**: each task is an item, made and closed by a synchronisation.
2. **A live view**: tables and boards read the engine whenever they are shown; no item.

How the wiki learns what is open:

3. **A modelled step**: each process writes its own wiki task through the engine's wiki Worker Type (atlas ADR-0498).
4. **Comparing**: the wiki reads the engine's list of open tasks and compares it with its linked items.
5. **Following the facts**: the wiki reads the engine's feed of task facts after a cursor.

How a task is worked:

6. **The installation's credential** works every task, the wiki deciding who may.
7. **Each person's own grant**, the engine deciding.
8. **The wiki names the person** to the engine with a trusted credential.

Who sees a linked task:

9. **Groups copied** from the engine or a directory into the wiki's groups, and items protected for them.
10. **Each person's own task list**, read with their grant, and items protected for the people who see them.

## Decision outcome

Chosen options: **1, 4 with 5, 7 and 10**, because a task on a board must be an item (D1), the feed tells what changed while the comparison tells what is, and reading each person's tasks with their own grant lets the engine decide twice — what a person sees and what a person may do — whatever the people and groups come from.

### Task links: which tasks, where, with which labels

- **Rules per engine**, under *Administration → Process engines → Task links*, kept in `site.wal` with the engine. Each rule has an ID, a filter, a target and a mapping; the first rule whose filter matches a task links it, and **a task no rule matches is not linked**.
- **Filter:** processes by BPMN ID, task elements by BPMN ID, candidate groups; an empty list matches all.
- **Target:** a space and a type with a workflow. The rule names the type's state for a completed task and for a cancelled one (they may be the same finished state, ADR-0082); saving checks that every other state has one transition to each, and an administrator may add a missing one as on a kanban (ADR-0076).
- **Labels:** fixed labels and templates of the task's values — `{process}`, `{element}`, `{group}` — normalised as labels are (ADR-0044). Kanbans and boards by label (ADR-0087, ADR-0139) then gather the linked tasks without anything new.
- **Fields:** the due date into a date field of the type, the person who holds the task into a user field, both optional.

### Linked items

- A linked item's **origin** is the engine — system `engine`, instance the engine's ID, external ID the task's key — and it carries the mark **`Linked`** naming the engine, with the fields the engine owns (`LinkedFields`). **Codec version 36** adds both; versions 1 to 35 are still read. The task list's query (below) comes with a codec version of its own.
- It is **not managed** (ADR-0128): people comment on it, label it, relate it, put it on kanbans and in a kanban's own columns, and move it between open states.
- **The engine owns** its title (the task's name) and the mapped fields: the synchronisation writes them as the actor `engine:<id>`, and a person's change to them is refused with a message naming the engine.
- **A finished state is reached only through the engine:** a transition of a linked item into the rule's completed state is the completion below; into its cancelled state, only the synchronisation makes it.
- **A task that ends** moves its item to the rule's state, as the synchronisation's transition (D3). The item stays, with its comments and relations (D7, D8).
- **Who reads it is not changed by people:** a person may not set or lift a linked item's own restriction, move it — or a page holding it — to another space, where its readers have no role, or put it in the trash while its task is open. A restriction on a page above it only narrows further (D9).

### Learning what is open

- **Credentials** from the environment, beside the engine's existing one: `WWBB_ENGINE_<ID>_TASKLIST_TOKEN` (on atlas a token of scope `tasklist` minted by an administrator, so it lists every open task) and, optionally, `WWBB_ENGINE_<ID>_EVENTS_TOKEN` (scope `events`).
- **Following the facts:** with an events token, a runner per engine reads the feed after its cursor every `TaskSyncEvery` (default 60 seconds) and applies the task facts: `created` links a task a rule matches, reading it once with the list token; `assigned` and `unassigned` read it again for the person; `completed` and `cancelled` move the item to the rule's state. The cursor is kept with the engine's settings, written at most once a minute.
- **Comparing:** every round without an events token, and with one after a start, after the feed answers that the cursor is older than what it keeps (410), after a change of rules, and every `TaskCompareEvery` (default 15 minutes), the runner reads the engine's whole list of open tasks with the list token, in pages, and compares: a matching open task without an item is linked, an open linked item whose task is gone is moved to the rule's completed state — a task that ended unseen cannot be told completed from cancelled.
- **Applied idempotently:** a fact about a task already settled changes nothing, so the feed may be read again from an older cursor.

### Connecting a person

- **Profile → Connected engines → Connect** starts the engine's authorization for the resource `<engine>/tasks` with PKCE; the wiki is registered at the engine as an OAuth client by its administrator, and its client ID and secret come from `WWBB_ENGINE_<ID>_CLIENT_ID` and `WWBB_ENGINE_<ID>_CLIENT_SECRET`. The engine's redirect returns to `GET /api/engines/{id}/connect/callback`.
- **Who the person is at the engine** is read with the new grant from `GET /api/v1/auth/me`: the engine's user ID — what the link keeps — and the username, display name, mail address and groups, shown on the connection. **The mail address is a suggestion:** when the engine's differs from the wiki account's, the page says so and the person confirms. Nothing is matched by mail; the person's own sign-in at the engine is the proof.
- **The refresh token** is kept sealed with a key of its own, never logged, returned or exported, and erased with the link; access tokens live in memory only.
- **A link ends** when the person disconnects, when the wiki account is disabled or deleted, or when the engine refuses a renewal — which it does once the engine's account is disabled or deleted. The linked tasks it showed that person are hidden at the next round. Connecting, disconnecting and a link ended by the engine are audited (ADR-0029).

### Who sees a linked task

- **Every `TaskVisibilityEvery`** (default 2 minutes), for each connected person, the runner reads the engine's task list **with that person's grant** — exactly the tasks the engine shows them, by assignee, candidate groups and roles, whatever the groups come from.
- **Each linked item is restricted** for reading (ADR-0078) to the connected people who see its task, written by the synchronisation. A new item is restricted to nobody until its first round. Search, queries, notifications, boards and the change feed hide it from everyone else as they hide any restricted item (D6, D9, D10).
- **People still need a role** in the target space: protection narrows, it never grants (ADR-0022). The administrator who sets a rule gives the people of the process a reader's role there.
- **Groups are not copied.** The engine's answer already applies them; one's own groups from `auth/me` are shown on the connection, and are what a group-based visibility would use if reading each person's list proves too costly (a follow-up, measured first).
- **Who has not connected sees no linked task:** they could not work one either.

### Working a task

- **Take on and give back** on a linked item call the engine's claim and unclaim with the person's grant; the person field follows from the engine's answer.
- **Completing:** moving a linked card into the rule's completed state — on a kanban, a board, the item's page or the task list — or *Complete* on the item opens the **task dialog**. The server reads, with the person's grant, the task, its form and the values it opens with, and the dialog draws the form with form-js as vendored (ADR-0144). **Send** checks the form as form-js does, and the server completes the task at the engine with the person's grant. Only when the engine accepts does the wiki make the transition, as the synchronisation's actor, the history naming the person. When the engine refuses — the task is not the person's, it ended, a value was refused — the card returns and the dialog shows the engine's reason; when the engine is unreachable, nothing changes.
- **A task without a form** asks for a confirmation and completes without values.
- **A form the vendored viewer cannot draw** — one using the engine's own components, such as a directory lookup or a mail view — is not sent from the wiki: the dialog says so and links to the task in the engine's own task app.
- **The wiki's edit right on the item is not asked:** whoever reads the card and is connected may try, and the engine decides (the requester's rule). Without a connection, the dialog offers to connect.
- **Not an MCP tool:** completing is deciding, and an agent works tasks at the engine with a grant of its own (ADR-0012).

### A table of tasks in a text

- The `tasklist` block (ADR-0114) takes a **query** (ADR-0136) instead of a label, in the block field `Query`: `label = atlas AND assignee = currentUser() AND due < now() ORDER BY due`. The label form stays for blocks made before.
- For linked tasks the table adds the due date and the person, and each row opens the item, where the task is worked.

### How current a linked task is

- Each linked item, each board and each table that shows linked tasks says **when its engine was last read**; past `TaskSyncStale` (default 5 minutes) it shows a warning that the tasks may be out of date.
- **Metrics** (ADR-0016): the time of the last successful round per engine, the failed rounds, and the number of connected people.

### Routes

- Administrators: `GET` and `PUT /api/engines/{id}/task-links`; `POST /api/engines/{id}/task-links/run`, with a dry run that reports what it would link and close.
- People: `GET /api/me/engines`; `GET /api/engines/{id}/connect` and `…/connect/callback`; `DELETE /api/me/engines/{id}`.
- A linked item: `GET /api/items/{ref}/engine-task` (the task, its form and its values, as the person), `POST /api/items/{ref}/engine-task/claim`, `…/unclaim`, `…/complete`.

### As built so far

Step 1 (pull request after #192): the model, the synchronisation by comparing, and the administration's API.

- `internal/item`: `Linked` and `LinkedFields` on items and on `ItemImported` (codec version 36); `LinkTask` makes a new linked item, its restriction to the synchronisation's actor and its labels in one batch, and updates an existing one's title and owned fields only; `EndTask` makes the one transition to a finished state; `TaskReach` checks a rule's type; sessions refuse what only the engine decides.
- `internal/engine`: `OpenTasks` reads `GET /api/v1/tasks` page by page with `WWBB_ENGINE_<ID>_TASKLIST_TOKEN`, up to budget `EngineOpenTasks` (20 000); a list cut by the budget ends nothing.
- `internal/site`: `TaskLinks` per engine, at most 20 rules, removed with their engine.
- `internal/tasklink`: a round per engine every minute while its links are switched on, and on request, dry or not; the last report is kept in memory. Steps of public wizards are never linked.
- `internal/server`: the administrators' routes above; settings and rounds on request are audited (`engine.tasklinks.change`, `engine.tasklinks.run`). The item's JSON names `linked` and `linkedFields`.

Step 2: the administration's page.

- `internal/web`: the engine's page under Administration shows its task links (`assets/tasklinks.js`): switched on or off, the rules — a rule's type is one with finished states, its due date field one of that type's date fields, its completed and cancelled states among those finished states —, whether the credential's variable is set, a try that changes nothing, a round now, and the last round's report with its problems. What it offers and how it reads a rule is in `assets/taskparts.js`, tested without a browser.

Step 3: connecting a person.

- `internal/engineconn`: a person's connection to an engine — the engine's ID of the person, username, name, mail address and groups — in a journal of its own, `connections.wal`. The refresh token is sealed with a key of the connection's own, kept in `keys.wal` under `engineconn:<id>`, and the key is destroyed when the connection ends, so neither the journal nor a copy of it gives the token again; a start destroys the keys of connections that are gone and rewrites the journal when renewals have made it long. The journal is **not backed up**: a refresh token is its person's credential and is replaced on every use, so a backup's token would be spent already, and a restored or copied server must not act as people at an engine; after a restore, people connect again.
- `internal/engineauth`: the engine's metadata of its tasks resource and of its authorization server (RFC 9728, RFC 8414) tell the endpoints and the engine's own spelling of the resource, which it compares exactly; endpoints on another server are refused. The authorization code flow with PKCE (S256) asks for that resource and comes back to `<WWBB_PUBLIC_URL>/api/engines/<id>/connect/callback`; the client's ID and secret, from `WWBB_ENGINE_<ID>_CLIENT_ID` and `_CLIENT_SECRET`, are sent in the form, the only way atlas takes them. Access tokens are held in memory; a renewal replaces the refresh token, so renewals of one connection run one after another. Only the engine refusing the grant (`invalid_grant`) ends a connection; an engine that is busy or away leaves it. Disconnecting revokes the grant at the engine where its metadata names a revocation endpoint (RFC 7009). At most budget `EngineConnectsPending` (10 000) connections are begun and not finished.
- `internal/engine`: `Me` reads `GET /api/v1/auth/me` with the person's access token.
- `internal/server`: the people's routes above. The engine's redirect back is a cross-site navigation, with which the session cookie (SameSite=Strict) is not sent, so the callback is public: the state, begun by the signed-in person and held by the server, names the person, and a cookie of its own (SameSite=Lax) binds it to the browser that began it; it is used once. Connecting begins only in a browser session, not with an API token. An engine removed or given another address ends its connections: the grants belong to the engine at its address. Connecting, disconnecting and a connection ended without its person are audited (`engine.connect`, `engine.disconnect`, `engine.connection.end`). The task links' answer tells administrators the variables, whether they are set, the redirect address to register at the engine and how many people are connected.
- `internal/web`: *Account → Connected engines* (`assets/engineconn.js`) connects, shows who one is at the engine and, when the engine's mail address differs from one's own, says so and offers to disconnect — the confirmation the decision names is that notice; nothing is matched by mail. The engine's page under Administration shows the registration.
- Accounts of the wiki are not disabled or deleted today, so nothing ends a connection for that yet.

What the engine side lacks, found while building (pblumer/atlas follow-ups): its consent page signs a person in with a password only, so a person who signs in to atlas through single sign-on must already have an atlas session when connecting; it offers no revocation endpoint, so a disconnected grant stays listed under *AI access* until the person withdraws it there, though its refresh token is destroyed here; and its token endpoint shares one throttle per address with the public forms, so after a restart of the wiki the first renewals of many people queue there.

Step 4: who sees a linked task.

- `internal/tasklink`: after a round of an engine's links, every `VisibilityEvery` (2 minutes), and at once when the round linked a task or a person connected or disconnected, each connected person's own task list is read with their grant (`engine.OpenTasksAs`, up to budget `EngineOpenTasks`). Each open linked item is then restricted, for reading and editing alike, to the synchronisation's actor and the connected people whose list holds its task, written as that actor; an item whose readers did not change writes nothing. A person whose list could not be read — the engine busy or away, or the list cut by the budget — keeps what they saw; an access token the engine refuses is given back, so the next round renews it and learns whether the grant still stands; a person no longer connected sees nothing. An ended task's item keeps its readers. Editing goes with reading because a linked item is not managed: who sees the task comments on it, labels it and moves it between open states, as far as their role in the space allows.
- The round's report says how many connected people's lists were read and not read, and on how many items the readers changed; a dry round reads no person's list.
- The protection narrows and never grants (D9): a connected person without a role in the target space still does not read the item.

Step 5: the task dialog.

- `internal/engine`: a person's work with the access token of their grant — `TaskAs`, `FormAs`, `ValuesAs` (the variables of the task's scope; none where the grant does not reach them), `Claim`, `Unclaim` and `Complete`; the engine's refusals keep its reason: `ErrNotAllowed` for 403 (held by another, a public form's step), `ErrRejected` for 400 and 422 (a value refused, as atlas checks directory fields).
- `internal/server`: the linked item's routes above. The item is found as the caller reads it (D10); the wiki's edit right is not asked. Without a connection, `GET` says so and the commands answer 409 with `connect`. A task the engine no longer answers is 409, and a round is asked for. Once the engine completes the task, the item moves to the rule's completed state by its one transition, **recorded as the person** — the history names who completed it, rather than the synchronisation's actor this record first named — and a round follows.
- `internal/web` (`assets/taskdialog.js`): the item's page shows the task as the engine shows it to oneself — who holds it, *Take on*, *Give back*, *Complete…*, and the engine's own app. Moving a linked card to a finished state — on a board or kanban, in the item's or a card's state bar, in the task list — opens the task dialog instead of the transition: the form drawn by form-js with the values it opens with, checked as form-js checks it, sent to the engine as oneself; a task without a form asks to confirm. A move to any other finished state than the rule's completed one is refused there: only the engine cancels. A form with parts only the engine draws — atlas's e-mail view, a field that searches a directory (`atlasDataSource`), or any type the vendored viewer does not know — is not sent from here: the dialog names them and links to the engine's app.
- Not an MCP tool (`internal/mcp/tools.go` says so).

Step 6: following the facts, and how current a linked task is.

- `internal/engine`: `TaskFacts` reads a page of the feed (`GET /api/v1/events?after=`) with `WWBB_ENGINE_<ID>_EVENTS_TOKEN` and answers its task facts, passing over the other events; a cursor the feed no longer holds (410) is `ErrCursorGone`. `OpenTask` reads one open task with the task list credential.
- `internal/tasklink`: with an events credential, a round follows the feed after its cursor, up to 20 pages: `created` reads the task once and links it as a comparison would (a public form's step never), `assigned` and `unassigned` have the people's lists read again, `completed` and `cancelled` move an open item to the rule's completed or **cancelled** state. Facts about an item already ended change nothing, so the feed may be read again. The feed is read before a comparison, so a cancelled task is cancelled before the comparison would close it as completed. A round compares as well after a start — first moving the cursor to the feed's end without applying what the comparison tells —, when the feed no longer holds the cursor, after the rules changed, and every `CompareEvery` (15 minutes); without an events credential every round compares, as before. Removing an engine or changing its address forgets its cursor.
- **The cursors are kept in a small file of the data directory**, `taskcursors.json`, replaced at once when one moved, rather than with the engine's settings in `site.wal`, which a cursor moving every minute would make grow without end. The file is not backed up: a restored instance compares and follows each feed from its end.
- How current: `GET /api/engines/{id}/tasks-read`, for anyone signed in, answers when the engine's tasks were last read in full and whether that is longer ago than `Stale` (5 minutes); the task panel, every board and kanban with linked cards, and the engine's page under Administration say so, with a warning once stale.
- Metrics (ADR-0016): `wwbb_tasklinks_last_read_timestamp_seconds`, `wwbb_tasklinks_failed_rounds` and `wwbb_engine_connections`, each by engine.

Not built yet: the task list's query.

### Consequences

- **Positive:**
  - The engine's tasks are tasks of the wiki: on kanbans, in the inbox, in tables of a page, searchable and commented, without a second task object.
  - The engine decides every time, twice: what each person sees and what each person may do. No account, group or mail address is matched by the wiki.
  - People and groups may come from anywhere; the coupling does not care.
  - A person who leaves the engine leaves the linked tasks with their next renewal.
- **Negative / trade-offs accepted:**
  - Every visibility round reads one list per connected person; for many people the round must run less often, and a change of who holds a task shows with that delay.
  - Readers who have not connected see no linked task, even when the engine would show them one.
  - The wiki keeps a refresh token per connected person: a new kind of secret at rest.
  - A task that ended while the feed was not read is closed as completed, whatever it was.
  - Forms with the engine's own components are worked in the engine's app.
  - Administrators of the wiki see every linked task (open question 1).
- **Follow-ups / risks to watch:**
  - A group-based visibility from one's own groups, if reading each person's list is too costly.
  - The engine checking submissions against their forms (open question 2).
  - Who completed a task in the engine, if the engine adds it to its facts.

## Pros and cons of the options

### Option 2: a live view

Strongest form: the engine stays the only copy; nothing is ever stale; visibility is the engine's per reader at every read; nothing about tasks is stored in the wiki, which ADR-0129 preferred for processes.

- Good: no synchronisation, no staleness, no copy of anything.
- Bad: kanbans hold items, labels are on items, the inbox, search and notifications read items; a board of things that are not items is a parallel domain (D1) and a second board component. The requester's labels and kanban moves have nothing to attach to.

### Option 3: a modelled step

Strongest form: it exists — the engine's wiki Worker Type opens a wiki task from a process step — and each process decides explicitly what to publish and with which labels.

- Good: nothing to build in the wiki.
- Bad: every model must be changed, and a model that forgets leaves its tasks out; the step does not know the task's key, so the wiki task is not linked to the user task and completing it does not complete the user task.

### Option 4 alone: comparing only

Strongest form: one mechanism, no cursor, always correct after each round.

- Good: simple and self-healing.
- Bad: each round reads every open task; it cannot tell completed from cancelled. Kept as the baseline and the repair; the feed carries the rounds in between.

### Option 6: the installation's credential

Strongest form: no connection per person, no token store; every reader of a card can complete it at once.

- Good: simplest for people and the server.
- Bad: the engine would record the credential, not the person, and the wiki's rights would replace the engine's: anyone who may edit a space could approve their own request. The requester excluded it.

### Option 8: the wiki names the person

Strongest form: no grant per person; one trusted credential and a name; the engine checks the named person's rights.

- Good: the engine still decides on rights; no refresh tokens in the wiki.
- Bad: the engine would decide on an identity the wiki asserts and the engine cannot check, and the wiki's accounts would have to be matched to the engine's — by name or mail, which the requester excluded as proof. The engine offers no such delegation.

### Option 9: groups copied

Strongest form: groups are a familiar way to protect items (ADR-0078), one protection per group instead of lists of people, and group members would be visible in the wiki.

- Good: few protections, cheap to apply.
- Bad: the groups may come from anywhere — the engine, a directory, a provider — and the engine's group lists are its administrators'; copying them needs every member matched to a wiki account, which brings back matching by name or mail; copied groups in the wiki would grant rights in spaces, which the requester excluded; and the wiki would re-implement the engine's rule of who sees a task, which drifts.

## Open questions

1. **Administrators see every linked task.** A wiki administrator, and an administrator of the target space, is not bound by restrictions (ADR-0022), so linked tasks are visible to them whatever the engine would show. Accept, as for every restricted item, or keep linked tasks from administrators who are not connected?
2. **What is submitted is checked in the browser.** The engine checks who completes a task, and a directory field's value; required fields and value shapes are checked by form-js in the browser, here as in the engine's own app, with another copy of form-js. Is that enough, or must the engine check submissions against the form before linked tasks are completed here?
3. **Rules or named processes.** Rules link every task a filter matches, and a rule with an empty filter links all of an engine's tasks. Should linking be limited to processes an administrator names one by one?

## Links

- answers the open question of [ADR-0049](0049-a-task-inbox-after-atlas.md) (a task inbox) and the third use of [ADR-0148](0148-start-forms-and-the-forms-of-a-process.md) (user tasks)
- relates to [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (engines), [ADR-0144](0144-forms-and-decisions-from-a-process-engine.md) (forms), [ADR-0060](0060-kanbans-are-topics-whose-cards-carry-their-label.md), [ADR-0076](0076-kanban-columns-card-fields-and-free-moves.md) and [ADR-0139](0139-the-boards-of-a-type-and-a-label-show-the-cards-of-a-kanban.md) (kanbans and boards), [ADR-0114](0114-tasks-in-a-text.md) (tasks in a text), [ADR-0136](0136-a-query-language-selects-items-by-their-fields.md) (queries), [ADR-0078](0078-protecting-items-for-people-and-groups.md) (protection), [ADR-0082](0082-a-state-no-transition-leaves-is-finished.md) (finished states), [ADR-0128](0128-the-atlassian-import-as-a-connector.md) (managed items), [ADR-0030](0030-openid-connect-single-sign-on.md) (single sign-on), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0016](0016-metrics-and-backup.md) (metrics)
- the engine's side: pblumer/atlas ADR-0510, Tasks for a connected system (task facts on the feed, the `tasklist` scope, grants for `…/tasks`, one's own groups in `auth/me`)
