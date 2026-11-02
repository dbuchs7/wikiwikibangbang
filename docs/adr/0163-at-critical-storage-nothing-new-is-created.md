# ADR-0163: At critical storage nothing new is created

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-02
- **Deciders:** Project initiators (the maintainer's request of 2026-11-02: "at a storage threshold no new entries can be opened any more, so that everything stays stable for reading until more storage is added", ahead of a stress test that creates thousands of pages and tasks)

## Context and problem statement

ADR-0150 watches the file system of the data directory and protects it in two steps: from *critical* on, uploads are refused; at *full* (below 200 MB free), the wiki only reads, apart from what makes room. Between the two, every other command is still taken. A stress test, a careless agent or a connector that mirrors a large source can create thousands of pages and tasks in that window, and drive the storage from *critical* to *full* — where nobody can edit anything any more. The log only grows (ADR-0008, ADR-0025): each new item adds its creation, its key and the commands that follow it.

What may still be done between *critical* and *full*, so that reading and the work on what exists stay stable until room is added?

## Decision drivers

- **Stable for reading and for the work on what exists:** editing, transitions, comments, labels and the live editor go on.
- **Room can always be made:** the trash, purging and cleaning up stay possible, and signing in too (ADR-0150).
- **Hard to bypass:** one rule in one place covers the UI, the API, MCP, copies, connectors, linked tasks and imports alike, not a check per handler that a new route forgets.
- **Understood:** people learn why something is refused before they try, not only from an error.
- **Replay stays deterministic:** the rule refuses commands, never the replay of the log.

## Considered options

1. **Refuse everything that creates something new from *critical* on**, in the store, by the events a command records
2. **Only uploads**, as ADR-0150 decided (the status quo)
3. **Refuse every write from *critical* on**, as at *full*
4. **Quotas per space**

## Decision outcome

Chosen option: **1**.

- **What counts as new:** a command whose batch makes an item (a page, a task, a child page, a folder of the album, a copy of a page or a tree, an item made from a template, the task of an access request), an item an import or a synchronisation writes for the first time (`ItemImported` for an ID the store does not know: connectors, linked tasks, files mirrored as pages), a space, or a kanban (`KanbanSet` for a kanban the space does not have). A type definition is not an entry and stays possible; so does everything that changes, moves, comments on, labels, protects, trashes, restores or purges what exists.
- **Where:** `item.Store.recordBatch` asks the check the storage set (`Store.SetNothingNew`) whenever a batch creates (`Store.creates`); the storage's check answers `storage.ErrNothingNew`, an `ErrNoSpace`, from *critical* on, by the store's level (`DB.Space`). The whole batch is refused and nothing reaches the log. The replay never consults the check. The level is the running server's measurement; the offline commands (`import`, `restore`) measure nothing and are not held by it. A folder's type is not defined for a folder that is refused.
- **Answer:** HTTP 507 with "The storage is almost full: no new pages, tasks or rooms can be created until there is room again. Reading, editing existing entries and deleting stay possible." — through the API, and so through MCP's `create_item` too.
- **Access requests (ADR-0162):** a request makes its task in the same batch, so the request is refused as a whole. A request recorded without its task would leave the owner unaware of it and would have to be repaired later; refusing it says plainly that it can be made once there is room.
- **Comments stay allowed:** they are small changes of an existing item, held on it (ADR-0035), not entries of their own.
- **Accounts stay allowed:** an administrator's action, rare and tiny, kept in `accounts.wal`, not in the item log; an administrator may need a new account to make room.
- **Background writers:** a connector keeps existing pages in step and writes no new entry; it counts them (`paused` in its result), says so in one problem of the run, logs it once when the pause begins and once when it ends, and keeps the point a since-run asks from, so the next run with room reads and writes them. The Atlassian connector refuses at its first new space or item, and its run says why. The linked tasks of ADR-0151 keep their items in step and link no new task (`paused` in the round's report, not a problem); the log says so once per engine and once when it ends; a task the engine's feed told of meanwhile is linked by the comparison of the next round, as the feed has passed its fact.
- **Showing:** `GET /api/status` answers every signed-in person the level (`{"storage":"critical"}`) — no sizes, which stay administrators' information at `GET /api/storage`. While the level is *critical* or *full*, every signed-in person sees a slim banner under the top bar ("Storage almost full — new entries are paused; reading and editing continue", at *full* "Storage full — read only"), and the controls that create — the top bar's *+ New*, every link to the new-item form, the space's *New page*, *New child page*, the album's *New folder*, the kanban's *+* per column, the new kanban and new room forms — are disabled with a hint. Administrators keep the notice of ADR-0150 on the start page.
- **At *full*,** ADR-0150's rule stays: the wiki only reads, apart from what makes room.

### Consequences

- **Positive:** between *critical* and *full* the storage grows only by edits of what exists; a mass of new entries, from people, agents or synchronisations, cannot push the wiki into read-only. The rule sits where every command passes, so a new route or tool cannot forget it.
- **Negative / trade-offs accepted:**
  - From *critical* on, nobody may create a page, even a short one; an installation held at *critical* by a small file system has to lower the thresholds (ADR-0150).
  - Edits still grow the log, and a large body committed many times can still reach *full*; that is ADR-0150's floor.
  - A synchronisation stays incomplete while the pause lasts: its source's new entries and an engine's new tasks are missing here until there is room.
  - The UI learns the level when a view opens; a level that changes while a view stays open shows on the next one, and the server's answer covers the time between.
- **Follow-ups / risks to watch:**
  - quotas per space (option 4), should spaces need bounds of their own;
  - snapshots and compaction of the log (ADR-0008), the only way the log itself gets smaller.

## Pros and cons of the options

### Option 2: only uploads (the status quo)

Strongest form: uploads are by far the largest growth; texts are small, and people should be able to keep writing as long as possible. The floor of *full* already protects the log itself, and every further refusal is friction for work that would likely succeed.

- Good: the least friction; no new rule to learn.
- Bad: a mass of small creations — exactly what a stress test or a mirrored catalog does — grows the log by thousands of records, and the step from *critical* to *full* then locks all editing. Creation is also the growth that nothing urges: what exists still needs its edits, a new entry can wait.

### Option 3: refuse every write from *critical* on

Strongest form: the simplest rule there is — one level, one behaviour; it stops all growth at once and leaves the most room for the commands that make room.

- Good: the strongest protection; no classification of commands.
- Bad: *critical* is meant as the time to act (ADR-0150), not as an outage: people in the middle of their work could no longer finish a task, answer a comment or correct a page, though these grow the log only a little. It turns the two levels into one.

### Option 4: quotas per space

Strongest form: a bound per space, counted from what the wiki stores, stops one space's mass creation without touching the others, and is predictable for whoever runs a space.

- Good: fair between spaces; stops a runaway space early.
- Bad: it answers who may use how much, not whether the next write succeeds (ADR-0150); it needs decisions about who sets which bound and how a moved item counts. It complements this rule rather than replacing it.

## Links

- amends [ADR-0150](0150-the-wiki-watches-its-storage.md) (from critical on, nothing new is created, not only uploads)
- relates to ADR-0008 and ADR-0025 (the log only grows), ADR-0035 (comments), ADR-0060 (kanbans), ADR-0077 and ADR-0140 (connectors and since-runs), ADR-0090 (album folders), ADR-0151 (linked tasks), ADR-0162 (access requests become tasks)
