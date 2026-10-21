# ADR-0081: The server stays fast with tens of thousands of items

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer reported that the server had become much slower since the catalog import of 10 690 entries, and asked to check how the system scales with much content, 2026-10-21)

## Context and problem statement

The in-memory store answers every read by walking all items (ADR-0008, ADR-0033). This held for a few hundred items. A connector (ADR-0077) brought an installation to 10 690 catalog entries, and every page became slow.

A scale test with that many entries, `go test -tags scale -run TestScale ./internal/server/`, measured where the time went:

| Request | Before | Share of the cause |
|---|---|---|
| The bell, asked by every page (`/api/notifications?limit=1`) | 850 ms; 16 s with a generated page per entry (ADR-0080) | the governance findings: every item's text searched with the sensitive data patterns on every request |
| One page of a listing (`/api/items?limit=100`) | 75 ms | every matching item copied with its body, comments and attachments, for a page of 100 |
| The task list | read the first 2 000 items of any type, 1.5 s | catalog entries crowded out tasks; tasks after the first 2 000 items were missing |
| Space tree (`/api/spaces/{space}/tree`) | 0.6 to 0.9 s | every child copied with its content; read permission checked twice per item |
| Search for a common term | 1.1 to 2 s | rating every hit built joined word lists per block |

On top of that, `DB.Read` took the store's exclusive lock. One slow read held up every other request, including the other requests of the same page.

The measurements come from a development machine; a NAS is several times slower. How does the server stay usable at this size without giving up its model of one in-memory store?

## Decision drivers

- The bell and listings are on every page; they must not grow with the content.
- Results stay exactly as before: no finding, task or hit may go missing for speed.
- What a read remembers must never hold content that was erased or redacted (ADR-0026).
- `applyToState` stays deterministic and free of side effects; remembered results are derived state outside it.

## Considered options

1. **Remember derived results and avoid needless work, in the existing store.**
2. **A separate index or database for listings, findings and search.**
3. **Accept it and advise smaller catalogs.**

## Decision outcome

Chosen option: **1**.

- **Reads run side by side.** `DB.Read` takes a read lock and commands a write lock. What a read remembers is guarded by its own lock. A test asks every reading endpoint at once from several clients while commands run, under the race detector.
- **The sensitive data scan is remembered per item.** The store remembers which texts of an item hold findings, together with the item's last touching event when it was scanned. An item is scanned again only after it changed.
  - The memo holds where findings are, never text. The findings themselves are worked out again from the current text when a report needs them, so nothing of a redacted or purged item stays.
  - The server fills the memo in the background after it starts, in parts of 200 items, each part a read of its own, so commands are not held up.
- **The patterns are tried only where they can match.** Each detector has a cheap test that is true for every text its pattern can match:
  - a keyword and `:` or `=` for passwords;
  - the literal starts of tokens;
  - two capitals and two digits for an IBAN;
  - 13 digits for a card number;
  - the key header for private keys.

  A test with random texts checks that the cheap tests never turn away a match.
- **The bell works out only what it shows.** It computes the findings the person answers for, and the holds. A type's due fields are found once per report rather than once per item. Each person's findings are kept until anything in the store changes or the hour turns.
- **Listings copy only the page.** `Session.FindPage` filters and sorts the matching items without copying them and copies only the page asked for. Children in a tree are listed as summaries without content, and whether a node has readable children is asked only for the nodes listed.
- **The task list reads the task types only**, one listing per type, instead of all items up to 2 000.
- **Rating search hits** builds each block's word string in one pass.

### Results

Measured with 10 690 entries of 30 paragraphs each, warmed up:

| Request | Before | After |
|---|---|---|
| The bell | 850 ms; 16 s with generated pages | 5 ms; 50 to 125 ms after a change to the store |
| A page of a listing | 75 ms | 10 to 18 ms |
| Listing all items, 107 pages | 7 s | about 2 s |
| Space tree | 0.6 to 0.9 s | 50 to 60 ms |
| Search, common term | 1.4 to 2 s | 1.1 to 1.3 s; a rarer term 0.3 to 0.4 s |

### Consequences

- **Positive:** pages no longer slow down with the size of a catalog. A slow request no longer holds up the others.
- **Negative / trade-offs accepted:** a report asked for before the background scan finished still scans. With 10 690 generated pages the whole scan took 2.7 s on the development machine.
- **Negative / trade-offs accepted:** the bell's findings may lag an hour behind a deadline that passes without any change to the store.
- **Negative / trade-offs accepted:** search and the governance report still walk every readable item. They stay in the order of a second at this size, and grow with it.
- **Negative / trade-offs accepted:** a connector's first run writes one fsynced command per entry: 1.5 to 2 minutes for 10 690 entries with pages here, longer on a NAS.
- **Follow-ups:** an index for search and for listings by space and type, should catalogs grow by another order of magnitude; batching a connector's writes.

## Pros and cons of the options

### Option 2: a separate index or database

Strongest form: listings, search and findings in an index scale far beyond what walking memory can, and are the usual answer.

- Bad: a second store to keep in step with the log, to back up and to restore, for a size the existing store handles once needless work is gone. It may follow when search grows; nothing here stands in its way.

### Option 3: smaller catalogs

Strongest form: a catalog of every CSV file of a portal is not a catalog of interfaces, and the formats setting can keep it small.

- Bad: the wiki would then fail at a size a single team reaches with ordinary use; the slowness was in the code, not in the content.

## Links

- relates to ADR-0008 (in-memory store, one writer), ADR-0033 (search), ADR-0040 and ADR-0057 (governance findings), ADR-0049 (task list), ADR-0077 and ADR-0080 (connectors)
