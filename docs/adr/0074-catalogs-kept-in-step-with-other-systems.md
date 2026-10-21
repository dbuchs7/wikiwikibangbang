# ADR-0074: Catalogs kept in step with other systems

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for a space of interfaces, kept current by AI agents over MCP and by a synchronisation, first as a mirror of opendata.swiss and later as the master; every entry with an owner and ideally a contact, checked by the governance check, 2026-10-21)

## Context and problem statement

A catalog — of interfaces, applications, data sets — is a set of items of one type whose entries correspond to entries in other systems. It is filled by a synchronisation or by agents, read by people and automations, and must stay trustworthy as both write to it. Items already have typed fields, workflows, relations, a history and a governance check (ADR-0001, ADR-0040), and imports match items to their source by origin (ADR-0023). What is missing for a catalog that stays correct over many runs?

- An agent finds an entry by its identifier in the other system only by full-text search, which is fuzzy; repeated runs create duplicates.
- Nothing stops two items from claiming the same identifier.
- A web address is a text field: not checked, not opened, not part of the link check.
- The governance check knows required fields, not ones that should be set; and nothing asks for an entry to be checked again after a while.
- Findings go to whoever changed an item last, which for a synchronised entry is the synchronisation, not a person.

## Decision drivers

- Agents and a synchronisation must be able to keep an entry in step without duplicates, through the public API and MCP only.
- Rules belong to the type, as data (ADR-0001), so other catalogs use them too.
- A person, not the last writer, answers for an entry.
- Nothing that a source does not know may be lost on the next synchronisation.

## Considered options

1. Model the catalog with what exists, and leave identity and checks to the agents' instructions.
2. Add catalog rules to type definitions, enforced by the store and reported by the governance check.
3. A separate catalog module beside items, with its own model.

## Decision outcome

Chosen option: **2**, because the rules are few and general, and enforcing them in the store protects the catalog from every writer alike — person, agent or synchronisation.

- **url fields.** A field kind `url` holds an absolute http or https address of at most 2048 characters. The UI offers an address input and opens the address in a new tab; the link check (ADR-0039) checks url fields too, and an item's place in its system of record (`origin.url`, ADR-0023).
- **Unique fields.** A text, url or number field may be `unique`: no two live items of the type in one space hold the same value. Creating, setting, importing, restoring and moving are refused when they would break it; a type cannot be redefined so while values are held twice. A trashed item frees its value.
- **Finding by value.** `GET /api/items` and `/api/search` take `where=<field>=<value>`, repeated for several fields, compared as text (a date as `YYYY-MM-DD`). The MCP tool `find_items` takes it too, so an agent finds an entry by its identifier before it creates one.
- **Recommended fields.** A field may be `recommended`: the governance check lists items where it is empty, like an empty required field.
- **Reviews.** A type may have a `review` rule: `everyDays`, the date field that holds the last review, and optionally the user field of the `owner`. An item is due when that date is empty or older than `everyDays`; the governance check lists due items, and they reach the owner's bell (ADR-0057). Whoever may edit the item records a review from the report (`POST /api/items/{ref}/reviewed`, which sets the date to today). With an owner set, the owner — not the last writer — is responsible for every finding of the item.
- **Governance for agents.** The MCP tool `get_governance` returns a space's check, so an agent can fill a missing contact; recording a review stays a person's step in the UI.
- **Origin shown.** An item with an origin says where it is mirrored from and links to its entry in the system of record.
- The rules are part of a type definition and so of the event format: codec version 18.

### Consequences

- **Positive:** a catalog can be kept in step by repeated runs of a synchronisation or an agent without duplicates; its entries carry checked links; every entry has a person who answers for it and is asked to check it again.
- **Negative / trade-offs accepted:** the uniqueness check scans the items of the type in the space on every change (fast enough for tens of thousands of items; an index would follow if needed); `where` compares text and does not search ranges.
- **Follow-ups / risks to watch:** the synchronisation with opendata.swiss itself, which mirrors its interfaces into a catalog and keeps the fields only the catalog holds (owner, contact, review); the cut-over to the catalog as the master (`Release`, ADR-0023).

## Pros and cons of the options

### Option 1: what exists, plus instructions

Strongest form: no change to the model; agents are told to search before they create.

- Good: nothing to build.
- Bad: a fuzzy search and a forgotten instruction make duplicates; a synchronisation needs the same; nobody is asked to check entries again.

### Option 2: catalog rules on types

Strongest form: the store guards identity for every writer; the existing governance check and bell carry the reviews.

- Good: general, small, enforced where every write passes.
- Bad: a type definition grows; the event format changes.

### Option 3: a catalog module

Strongest form: a model made for catalogs, with versions and links of its own.

- Good: freedom to model catalogs exactly.
- Bad: a second model beside items, without their history, permissions, search, comments and governance — all of which a catalog needs.

## Links

- relates to ADR-0001 (types as data), ADR-0023 (origin and one-way sync), ADR-0039 (link check), ADR-0040 and ADR-0057 (governance and findings), ADR-0012 (MCP)
