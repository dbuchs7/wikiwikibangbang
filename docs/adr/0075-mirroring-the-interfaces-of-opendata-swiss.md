# ADR-0075: Mirroring the interfaces of opendata.swiss into a catalog

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for all real APIs of opendata.swiss in a catalog space, linked to their entry in the system of record, first as a mirror and later as the master; with an owner — the publisher and a person here — and ideally a contact; formats as a configurable list; kept current both by a synchronisation and by agents, 2026-10-21)

## Context and problem statement

A catalog of interfaces (ADR-0074) is to hold every interface that opendata.swiss publishes. The portal runs CKAN with the Swiss DCAT-AP profile; its action API (`https://ckan.opendata.swiss/api/3/action`) lists data sets with their resources, each with a format. Thousands of data sets, of which some hundreds publish an API or a map service. Who brings them into the catalog, and how does the catalog stay in step without losing what is added here?

## Decision drivers

- Every interface, reproducibly, at no cost per run; a run that changes nothing writes nothing.
- The catalog adds what the source does not know — an owner here, a review, notes, often a contact — and a run must not take it away.
- An interface the source stops publishing must not vanish without trace.
- A failure of the source must not empty the catalog.
- Agents should be able to work on the same catalog, through MCP, without a second mechanism.
- Later the catalog becomes the master: the mirror must be able to end.

## Considered options

1. Agents only: an assistant reads the portal and writes through MCP.
2. A synchronisation in the server, on a schedule, with a dry run; agents work on the result.
3. A one-off import with the existing command (ADR-0023), repeated by hand.

## Decision outcome

Chosen option: **2**, because a deterministic synchronisation is the only one that reads thousands of data sets reproducibly and cheaply on a schedule; agents keep the tasks where judgment helps — a missing contact, a specification, a check — on the same items through MCP (`find_items` with `where`, `get_governance`; ADR-0074).

- **Source.** A run first asks the source which formats its data sets use (`package_search` with `rows=0` and the facet `res_format`), because its search compares formats exactly: `API` would not find `api`. The formats that count as an interface are a setting, by default `API, WMS, WFS, WCS, WMTS, SPARQL`; they are matched to the source's formats without regard to case and with common spellings (`REST`, `OGC:WMS`, `wms_srvc`, …), and the source's own spelling is searched: `package_search` with `fq=res_format:(…)`, optionally `AND organization:(…)`, in pages of 500, sorted by name. Downloads (CSV, JSON files) are left out. When no format of the list occurs in the source, the run writes nothing and names the source's most frequent formats. The administration lists the source's formats with the number of data sets for each, for the source and publishers as typed, to tick the ones that are interfaces.
- **Entries.** Each resource in such a format becomes one item of the type `interface` in the catalog's space: an interface is an endpoint, and a data set may offer several (a WMS and a WFS). The item's origin (ADR-0023) is the resource: system `opendata.swiss`, instance the API's address, external ID the resource ID; its URL is the data set's page on the portal, the entry in the system of record, linked from the item.
- **Type.** If the space's catalog has no type `interface`, the first run defines it: `source-id` (text, required, unique), `format`, `endpoint` (url), `specification` (url, recommended), `publisher` (text, required), `owner` (user, recommended), `contact` (text, recommended), `dataset` (url), `modified` (date), `reviewed` (date); a review every 365 days with `owner` as the owner; the states `active` and `withdrawn`. A type already defined is used if it has these fields with these kinds; otherwise the run stops and says which field is missing.
- **Owner.** The publisher is the owner in the source and is mirrored as `publisher`; the person here who answers for an entry is the `owner` field. New entries get the *steward* from the settings, if one is set, until someone else takes them over.
- **What a run writes.** The fields the source knows are mirrored: `source-id`, `format`, `endpoint`, `publisher`, `dataset`, `modified`, and the title. `contact` and `specification` are taken from the source when it names them and otherwise kept, so what an agent or a person adds stays. `owner`, `reviewed`, the body, comments, relations and labels are never touched. The items are not locked (no `Managed` tag): the owner and the review must be set here; a change to a mirrored field is overwritten by the next run, and the history shows it.
- **Withdrawal.** An entry the source no longer publishes is set to `withdrawn`, not deleted, and back to `active` when it returns. If the source answers fewer than half the entries that are active here, or none, nothing is withdrawn and the run says so: that is more likely a failure than a mass withdrawal.
- **Running.** On the schedule set under *Administration → Catalog sync* (every 24 hours by default), and on request: a *dry run* reports the counts and the first changes without writing, a run writes. Runs are audited (`catalog.sync`), as are changes to the settings (`catalog.sync.change`); the last ten runs are shown, held in memory.
- **The source is read with care.** Multilingual fields come as an object of languages, as text, or as text holding such an object; contact points as a list or one; documentation as a list, text, or a list encoded as text. All are read; a field that cannot be read is left empty, not the run failed. German is preferred, then English, French, Italian.
- **Becoming the master.** Switching the synchronisation off leaves a catalog of ordinary items; their origins keep the link to opendata.swiss.

### Consequences

- **Positive:** the catalog holds every interface the portal publishes, with its link to the system of record, current within a day; what is added here survives; withdrawn interfaces stay visible as such; agents and people work on the same items.
- **Negative / trade-offs accepted:** the field names are fixed, in English; the representation of the portal's fields was taken from its schema and documentation, not from a live answer, and is read tolerantly — the dry run is the check against the real source; each changed entry is one write (fsynced), so a first run of some hundred entries takes seconds.
- **Follow-ups / risks to watch:** the portal's API address and representation may change (ckanext-switzerland-ng); further sources, such as a wiki of another product or an internal registry, would follow the same pattern; when the catalog becomes the master, the type may gain an approval workflow.

## Pros and cons of the options

### Option 1: agents only

Strongest form: no code for a source; an agent copes with changes in the source by reading it.

- Good: flexible; an agent can enrich while it reads.
- Bad: thousands of tool calls per run, not reproducible, at a cost per run; a wrong reading changes the catalog silently.

### Option 2: synchronisation in the server, agents on the result

Strongest form: reproducible, scheduled, checkable by a dry run; agents where judgment helps.

- Good: cheap, idempotent, audited; agents use the same items.
- Bad: code that must follow the source's representation.

### Option 3: the import command by hand

Strongest form: reuses the existing importer.

- Good: little new code.
- Bad: the server must be stopped (ADR-0023); nobody runs it daily; it knows Atlassian sources, not CKAN.

## Links

- builds on ADR-0074 (catalog rules) and ADR-0023 (origin, upsert, release)
- relates to ADR-0040 and ADR-0057 (governance), ADR-0012 (MCP), ADR-0039 (link check)
- the guide for agents: [docs/guides/catalog-agents.md](../guides/catalog-agents.md)
