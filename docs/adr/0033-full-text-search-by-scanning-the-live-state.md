# ADR-0033: Full-text search scans the live state; there is no search index yet

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (the maintainer chose search as the next feature, 2026-10-17)

## Context and problem statement

Items can only be found by browsing a space. A tool that unifies a wiki and an issue tracker is unusable without search. Three constraints shape it: search must never show an item the reader may not read (ADR-0022); content that was purged or redacted must be unrecoverable (ADR-0026), so no copy of it may survive in a search structure; and the state is already held in memory (ADR-0025).

## Decision drivers

- Results respect read permissions and restrictions exactly, through the same check as every other read.
- Erasure stays complete: purge and redaction leave nothing searchable behind.
- Correct before fast: no second copy of the state that can drift from it.
- Predictable matching for German and English text, including the Swiss spelling.
- The cost is measured, so the point where an index becomes necessary is known, not guessed.

## Considered options

1. **Scan the in-memory state** for every query
2. **An inverted index in memory**, maintained by `apply`
3. **An embedded search library** (e.g. Bleve) with an index on disk
4. **An external search engine** (OpenSearch, Meilisearch)

## Decision outcome

Chosen option: **1**, behind an interface that option 2 can replace.

- **Semantics:** the query is split into terms at every character that is neither a letter nor a digit; terms are folded (lower case, `ß` as `ss`) and de-duplicated. An item matches when **every** term occurs, as a substring, in its title, key, body text or a string property. Budgets: at most 256 bytes and 10 terms; anything else is a validation error.
- **Ranking:** the full key typed exactly (`PLAT-12`) ranks first; each term in the title adds more than a term in the body or properties; equal scores are ordered newest first. The snippet is the first matching body block, cut to 160 characters around the match.
- **Permissions and erasure:** `Session.Search` checks `canRead` on every candidate, like `Session.Find`. It reads only the live state, which loses an item's content when it is purged or redacted, so erasure needs no extra step and cannot be forgotten. Trashed items are excluded unless asked for.
- **API:** `GET /api/search?q=…` with the filters `space`, `type`, `state` and the usual page envelope (`limit`, `after`, `total`, `next`; ADR-0015). The UI has a search field in the navigation and a result list; snippets are inserted as text (ADR-0014).
- **Measured cost:** `BenchmarkSearch` — 10,000 items with about 1 KB of body each — takes about 30 ms per query on one core of a CI-class runner (roughly 3 ms per 1,000 such items), allocating about 1 MB. Folding writes into one reused buffer; the first version, which allocated a folded copy of every text, took 45 ms and 12 MB.

**Implementation is Partial:** no index (see follow-ups), no phrase or prefix-only search, no diacritic folding beyond `ß` (`ä` does not match `a`), no search in attachments (which do not exist yet). Comment text is searched like a property; when no body block matches, the snippet comes from the first matching comment, and the hit names it (ADR-0035).

### Consequences

- **Positive:** permissions and erasure are correct by construction; no new dependency, file or format; matching is simple to explain ("every word must appear").
- **Negative / trade-offs accepted:** cost grows linearly with the content. Reads and writes share one lock (ADR-0028), so a search delays every other request for its duration — about 30 ms at 10,000 pages. Substring matching finds `art` inside `start`, which favours recall over precision.
- **Follow-ups / risks to watch:** at about 50,000 items, or when search latency shows up in operation, replace the scan with an in-memory inverted index maintained in `apply` (option 2) — the API and its tests stay; a read lock that lets queries run in parallel (ADR-0008's open question) would ease the shared-lock cost before that; folding of diacritics needs Unicode normalisation (`golang.org/x/text`), which is a new dependency.

## Pros and cons of the options

### Scan the live state

- Good: no derived state, so nothing to keep consistent, rebuild or erase; permissions are applied to the real item.
- Bad: linear cost per query.

### Inverted index in memory

Strongest form: queries in microseconds independent of content size, proper ranking (term frequency), prefix search; maintained in `apply`, it is rebuilt on replay and dropped with the process, so it never reaches disk.

- Good: scales far beyond the scan.
- Bad: a second representation of every text in memory that every event kind must update correctly, including purge and redaction; a bug there either leaks erased content into results or loses hits. Worth it once the scan is too slow, not before.

### Embedded library with an index on disk

Strongest form: mature analysis (stemming, language detection, fuzzy matching) at once.

- Good: search quality.
- Bad: an index file holds plaintext of encrypted content and survives crypto-shredding unless every purge also rewrites it — it undermines ADR-0026; a large dependency.

### External search engine

Strongest form: the industry standard for search at scale.

- Good: best quality and scale.
- Bad: a second service to run, contradicting the single binary (ADR-0009, ADR-0024); plaintext leaves the process; permissions must be mirrored.

## Links

- respects [ADR-0022](0022-permission-model.md), [ADR-0026](0026-crypto-shredding-of-item-content.md) and [ADR-0014](0014-closed-renderer-for-user-content.md); uses the page envelope of [ADR-0015](0015-resource-budgets-and-paged-listings.md) and the item API of [ADR-0028](0028-item-api.md); builds on [ADR-0025](0025-event-log-file-and-in-memory-state.md)
