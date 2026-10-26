# ADR-0120: A change feed, one's own roles, and watching with read tokens

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested for an external catalogue that copies service descriptions from pages, 2026-10-26); proposed for the maintainer's acceptance
- **Open question:** an item watched by its own watch keeps producing anonymous entries after the caller lost the right to read it, until the caller ends the watch; is that timing signal acceptable, or should a watch end by itself when its item becomes unreadable (which apply cannot do without an event of its own)?
- **Question checked:** 2026-10

## Context and problem statement

An external catalogue copies service descriptions from pages into a shop of its own. It must never show a page its reader may not see, nor a protected passage (ADR-0088). It reads with a technical account that holds the role `viewer` only, through an API token of scope `read` (ADR-0027). Two things are missing for it:

1. It must learn at once when a page it copied, or an ancestor of it, changes in any way: content, fields, state, a restriction set or lifted (ADR-0022), a data protection hold (ADR-0058), a protected passage set, removed or erased (ADR-0088, ADR-0089), a move, the trash, a purge or a redaction (ADR-0021, ADR-0026). Notifications (ADR-0046) do not serve: they leave out access changes and protected passages by design, they leave out one's own changes, and they count only items readable now, so the very restriction that hides a copied page is the one change never told.
2. It must prove that its account holds no role other than `viewer`. `GET /api/me` says whether the caller is an administrator or holds the erasure role, but not which space roles it holds, and the space role listings are for space administrators (ADR-0048).

Besides, a token of scope `read` may send only GET and HEAD (ADR-0027), so the catalogue's account cannot watch the pages it copied.

## Decision drivers

- Unreadable is non-existent (D10, ADR-0022): nothing the feed answers may reveal an item its caller may not read, not even that the item exists.
- One source of truth: the feed must not say something the log does not, or miss what it does (as ADR-0046).
- No storage per entry, and a cursor that survives restarts and backups.
- A `read` token stays unable to change anything anyone else reads.
- Every bound on what a caller makes the server hold is a budget (ADR-0015).

## Considered options

1. **A feed read from the log with its sequence numbers as cursor; anonymous entries for unreadable items; the watch routes open to read tokens; one's own grants as a read**
2. **Widen notifications** to every kind of event and keep their paging
3. **Webhooks**: the server posts each change to an address the catalogue registers
4. **Grant the catalogue's account a role in the access listings** (`GET /api/spaces/{space}/roles`) so it can check itself

## Decision outcome

Chosen option: **1**.

- **Change feed:** `GET /api/changes?after=<seq>&limit=<n>&wait=<seconds>` answers `{"changes":[{"seq":…,"item":"<id or empty>","at":"<RFC 3339, UTC>"}],"last":…}`. It lists, oldest first, every event of the log after `after`, of any kind and by anyone, the caller included, that touches what the caller watched at the time of the event: the watched item; an item that is now an ancestor of a watched item; an item of a watched space; and, for an event on a whole space (trash, restore, purge, rename), that space or a space holding a watched item. Events that change the caller's own roles (a role of theirs or of a group they belong to, their group memberships, their system roles) and the state of a space they hold a role in are always listed. Watching and unwatching are not changes: they belong to no item (ADR-0046) and would tell who else watches.
- **The cursor:** `seq` is the event's sequence number in the log (ADR-0025), so a cursor survives restarts and backups. `last` is the newest event the server considered: the head of the log, or the last entry's when `limit` cut the page; a caller continues with `after=last` and moves on even when nothing it watches changed. Without `after` the answer has no changes and `last` is the head, to start from now. Nothing is stored per entry; watches that have ended are found from the watch events of the log, which the store indexes per principal in memory as it applies them.
- **The item of an entry:** the item's ID when the caller may read it now, otherwise the empty string. An entry for an unreadable item names nothing: no ID, key, title or content, only that something the caller watches changed; the caller then checks again everything it holds. Such an entry appears only through a watch of that very item, or for an event on an ancestor of a watched item the caller cannot read either: the caller chose to watch it while it could read it, and already knows of it. An item seen only through a space watch is listed only when readable now, and an ancestor's event only when the ancestor is readable or the watched item is not: otherwise a space watch would report the activity of items the caller never could read, and an unreadable draft above a readable page (ADR-0058) the activity of that draft — both against D10.
- **Long poll:** with `wait`, a request with no entry is held until an entry arrives or the wait ends, and ends early when the request is cancelled. The store's lock is not held while waiting: every command that adds to the log closes a channel (`storage.DB.Changed`) that waiting requests take before they read.
- **Budgets:** `ChangesPage` (largest page, 1000; `limit` is 100 when absent), `ChangesWait` (longest wait, 30 seconds; it must stay below the server's write timeout of 60 seconds) and `ChangesWaiting` (requests held waiting at once, 1000; beyond it a request is answered at once).
- **Own roles:** `GET /api/me/roles` answers `{"system":[…],"spaces":[{"space":"<key>","role":"viewer|editor|space-admin","via":"" or "@group"}]}`: the caller's system roles, and every space role granted to the caller directly or to a group they belong to, one entry per grant (not only the effective role), ordered by space and then by `via`. It tells nothing about anyone else. Spaces in the trash are left out: they are out of everyone's sight and grant nothing (ADR-0069); a restore is an entry of the feed for whoever holds a role there.
- **Read tokens watch:** a token of scope `read` may `PUT` and `DELETE` `/api/items/{ref}/watch` and `/api/spaces/{space}/watch`, for its own principal. It is the one exception to "read allows only GET and HEAD" (ADR-0027): a watch changes no item and no item's history (ADR-0046) and nothing anyone else reads. The route table marks these routes (`ReadScope`, `x-read-scope` in the OpenAPI description), and the gate refuses every other change to such a token. Watching is not audited: like a read, it is not security-relevant, and no other watch is.
- **Ending a watch of an unreadable item:** `DELETE /api/items/{id}/watch` ends one's own watch of an item one may no longer read, by its ID; one knows of one's own watch, so this reveals nothing. Without such a watch, or by key, the item stays unknown (404).
- **MCP:** the adapter offers the reads, `get_own_roles` and `list_changes` (without `wait`: a tool call answers at once). Watching is not offered (ADR-0012).

**Implementation is Partial:** the API, its budgets and its tests are built; the web UI shows neither the feed nor one's own roles. It becomes Landed only once the decision is accepted.

### Consequences

- **Positive:** a copier learns of every change that may make a copy stale, access changes included, within one request, and never learns what it may not read.
- **Positive:** the feed cannot disagree with the log; it costs no storage.
- **Negative / trade-offs accepted:** a feed read from the start (`after=0`) scans the whole log in memory, as notifications do; a cursor kept close to the head scans only what is new.
- **Negative / trade-offs accepted:** the open question: a watched item that became unreadable keeps producing anonymous entries until its watch ends; the caller should end it when it finds the item gone.
- **Negative / trade-offs accepted:** a space watch does not tell that a readable item was moved out of the space (the item's current space counts), and an ancestor is an ancestor now, not at the time of the event; a move of the watched item itself is always told.
- **Follow-ups / risks to watch:** a view of the feed in the UI if people ask for it; whether watches should end when their item becomes unreadable.

## Pros and cons of the options

### 1. A feed from the log, anonymous entries for unreadable items

Strongest form: the log already holds every change with a gapless sequence number; reading it forward from a cursor is exact, durable and free of storage, and the permission check decides per entry at the time of answering.

- Good: one source of truth; the cursor survives restarts; entries for unreadable items say only "check again".
- Bad: an anonymous entry is still a signal; it is confined to what the caller watched itself.

### 2. Widen notifications

Strongest form: notifications already compute what a watcher is told from the log; adding the missing kinds is a small change, and the bell would show access changes too.

- Good: one mechanism instead of two.
- Bad: notifications are made for people: they leave out one's own changes, show the current key and title, page newest first and drop unreadable items, which is right for people (ADR-0046) and wrong for a copier; widening them would change what people see.

### 3. Webhooks

Strongest form: the catalogue is told without asking, at once, and holds no connection open.

- Good: no polling.
- Bad: the server would call out to addresses (ADR-0039 allows only public ones), must retry and remember what was delivered — storage per entry — and a read token would register an outgoing call, which is more than watching.

### 4. Access listings for the account

Strongest form: the space role listings exist (ADR-0048); giving the account the right to read them reuses them.

- Good: no new route.
- Bad: the account would need the role of space administrator, the very role it must prove it lacks, and would learn everyone's roles.

## Links

- relates to [ADR-0022](0022-permission-model.md) (D9, D10: unreadable is non-existent), [ADR-0027](0027-local-accounts-sessions-and-api-tokens.md) (token scopes; this record adds the watch exception to scope `read`), [ADR-0046](0046-watching-and-notifications-from-the-log.md) (watches and notifications from the log), [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages), [ADR-0058](0058-data-protection-review-holds-open-items-as-drafts.md) (holds)
- [ADR-0012](0012-mcp-adapter-over-http-api.md) (MCP), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0025](0025-event-log-file-and-in-memory-state.md) (sequence numbers), [ADR-0048](0048-roles-are-listed-and-taken-back.md) (role listings), [ADR-0069](0069-deleting-a-space-in-two-steps.md) (spaces in the trash)
