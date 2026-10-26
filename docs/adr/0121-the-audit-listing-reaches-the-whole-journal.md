# ADR-0121: The audit listing reaches the whole journal

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked that entries older than the in-memory window be listed from the journal, 2026-10-26)

## Context and problem statement

The audit trail keeps every entry for ten years in `audit.wal` (ADR-0029), but only the newest 10 000 are in memory, and `GET /api/audit` listed only those. Failed logins and failed token requests are audited too. On an exposed installation, many failed attempts can push an older entry out of that window within hours, for example the `design.change` entry that shows who gave the installation the official look (ADR-0120).

The entry then stays in the file and in backups, but the administration can no longer find it. A complaint about the official look must be answerable from the product for as long as the entry is kept.

## Decision drivers

- An entry stays findable for as long as it is kept.
- The memory used stays bounded, however large the journal grows.
- Recording an entry is never held up by a listing.

## Considered options

1. Read older entries from the journal when the in-memory window does not fill a page.
2. Keep rare entries, such as design and role changes, in memory apart from frequent ones, such as failed logins.
3. Keep every entry in memory.

## Decision outcome

Chosen option: **1**, because it covers every event, not only those chosen in advance, and changes nothing for listings the window can serve.

- A listing first takes from the in-memory window, as before.
- When the window holds fewer matching entries than the page asks for, and older entries exist, the journal is read from its start up to the oldest entry in memory. The newest matching entries found there complete the page.
- Paging with `before` continues into the journal in the same way, so the filter by event, actor or words (ADR-0098) reaches back to the oldest kept entry.
- The journal is read record by record (`wal.Each`). Only as many entries as the page asks for are held.
- The read runs without the trail's lock, so entries are recorded meanwhile. It reads only entries older than those in memory, which nothing changes except expiry. Expiry replaces the file atomically, and a read in progress keeps the file it opened.

### Consequences

- **Positive:** an entry such as the choice of the official look is listed for its whole retention, whatever came after it.
- **Negative / trade-offs accepted:**
  - A page that reaches past the window reads the journal from its start: time in proportion to the journal's size, for each such page. Only administrators list the trail, and only filters that find little in the window, or paging far back, reach the journal.
  - Where the operating system does not let a file open for reading be replaced, an expiry that runs during such a read can fail; it runs again the next day.
- **Follow-ups / risks to watch:**
  - If listings far back become frequent on large journals, an index of sequence numbers to file offsets would let a page start near its entries.
  - Tamper evidence, still open in ADR-0029: the listing shows what is in the file, not that it is unchanged.

## Pros and cons of the options

### Option 1: read older entries from the journal

Strongest form: the file already holds every entry; reading it when needed makes the window a cache, not a limit.

- Good: complete; no choice of which events matter; memory bounded.
- Bad: a slow page when it reaches far back on a large journal.

### Option 2: keep rare entries apart

Strongest form: the entries a complaint or an investigation needs are few, and keeping them apart makes them instant to list without reading any file.

- Good: fast; small.
- Bad: it needs a list of what counts as rare, which an attacker can still fill if any of those events can be caused at will; entries outside the list stay unreachable.

### Option 3: keep every entry in memory

Strongest form: the simplest listing, with no file reading at all.

- Good: simple and fast.
- Bad: memory grows without bound with failed logins, which anyone can cause; that is the reason for the window in ADR-0029.

## Links

- amends [ADR-0029](0029-persisted-security-audit-trail.md) (the listing is no longer limited to the in-memory window)
- relates to [ADR-0098](0098-every-list-can-be-filtered.md) (the trail's filter) and [ADR-0120](0120-the-swiss-theme-follows-the-confederations-design-system.md) (the entry that records the official look)
