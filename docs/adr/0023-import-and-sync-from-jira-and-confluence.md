# ADR-0023: Import from Jira and Confluence through one engine — snapshot import and one-way sync, never two masters

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17
- **Open question:** How much history (changelogs, page versions) must an import carry to be accepted by users? (Which source comes first was answered by the maintainer on 2026-10-19: Atlassian Cloud through its REST APIs, Confluence first.)
- **Question checked:** 2026-10

## Context and problem statement

wikiwikibangbang replaces Confluence and Jira. Almost every organisation that could adopt it already holds years of pages and issues there. If the data cannot come along, the product is not adopted, however good the model is. Adoption also rarely happens in one step: teams move space by space, while others keep working in the old tools for months.

The question is: **how is data taken over from Jira and Confluence — once, continuously, or both — and what must the data model provide so that it is easy and safe?**

Reasoning from what a migration actually consists of:

1. **Reading** the source — APIs or export files, each with its own formats, paging and limits.
2. **Mapping** the source's concepts onto items — issue types onto types, custom fields onto fields, workflows onto workflows, page trees onto containment, Confluence storage format onto block bodies, Jira issue macros onto embeds.
3. **Writing** the result — through the same validated commands as everything else, so that invariants hold for imported data too.
4. **Repeating** — because the first run is never the last: mappings are corrected, conversions improve, and during a transition the source keeps changing.

Steps 2 and 4 decide whether a migration is easy. Step 4 in particular requires that every imported item knows *where it came from*, so that a second run updates instead of duplicating.

The unified model helps here: types are data (ADR-0001), so a Jira project's issue types, fields and workflows become type definitions without code; and pages and issues being the same kind of thing means a Confluence page that embeds a Jira issue becomes a page that embeds an item — the cross-tool link becomes native.

## Decision drivers

- Re-runnable and idempotent: running an import twice changes nothing; running it after a source change applies only the change.
- No silent loss: whatever cannot be mapped is reported, and the original is kept so it can be converted later.
- Imported data obeys the same invariants and validation as everything else (D1–D10).
- Gradual adoption: teams can move one space or project at a time while others continue in the source.
- One clear master per space at any time; no conflicting edits between two systems.
- Links keep working: Jira keys, Confluence page links and cross-links between them resolve after the move.
- Permissions are never widened by an import.

## Considered options

1. **Generic CSV import only** — users export to CSV and map columns
2. **One-time direct import** from APIs or export files
3. **One import engine with two modes: snapshot import and incremental one-way sync**, with imported items carrying provenance and synced items read-only until cut-over
4. **Bidirectional synchronisation** between the source and wikiwikibangbang
5. **Rely on third-party migration tools**

## Decision outcome

Chosen option: **3.**

### One engine, three stages

```text
source reader  ──►  canonical source model  ──►  mapping  ──►  import session  ──►  events
(API / export)      (issues, pages, users,       (config,      (validated
                     fields, links, versions)     versioned)    commands)
```

- **Readers** are per source and format: Jira and Confluence REST APIs (Cloud and Data Center/Server), and offline exports (Confluence space export, Jira backup or CSV). Each reader produces the same **canonical source model**, so mapping and writing do not care where the data came from.
- **Mapping** is **configuration, not code**: a versioned file per import that maps projects to spaces, issue types to types, custom fields to fields, statuses and transitions to workflows, users to principals, macros to blocks. A default mapping is derived automatically from the source's own configuration; people adjust it.
- **Writing** goes through an **import session** that uses the normal validated commands (ADR-0022, D10). The import never bypasses validation; where source data violates a rule (e.g. a required field empty), the item is reported, not forced.

### Two modes

- **Snapshot import:** one complete run for a space or project — read everything, map, write. Repeatable: a re-run updates what changed and creates what is new.
- **Incremental one-way sync:** after a snapshot, the engine polls the source for changes (modified-since queries or change feeds) and applies them. The source stays the **master**; items under sync are **read-only** in wikiwikibangbang and marked as such, so no edit can be lost or conflict. This is the transition period.
- **Cut-over** per space: the sync is stopped, the read-only mark removed, and wikiwikibangbang becomes the master. Cut-over is explicit, recorded and irreversible for that space (switching back means a new import in the other direction, which this record does not cover).

### What the data model must provide

- **Provenance on every imported item:** source system, instance, external ID, external key and URL, last imported source version. It makes imports idempotent (match by external ID, never by title), makes sync possible, and lets old URLs redirect to the new item.
- **Key preservation:** an imported Jira issue keeps its key when the target space has the same prefix and the key is free (`PLAT-123` stays `PLAT-123`); the space's key counter continues above the highest imported number. People keep the keys they know; other keys become former keys that still resolve (D2).
- **A sync marker:** items managed by a running sync are read-only for everyone except the sync itself (`ErrForbidden`, "managed by sync from …").
- **Raw source retention:** the original representation (issue JSON, page storage format) is kept with each imported item, outside its body. When a converter improves, items can be re-converted from the original without going back to the source, which may by then be switched off.
- **Imported history as data, not as live commands:** changelogs and page versions, where imported, become events marked as imported, with the original time and mapped author. They show in history; they do not run workflow transitions or validation of the past.

### Conversion rules

- **Two passes.** Pass one creates all items (so every external ID has an item); pass two writes bodies, relations and links, which can then resolve to items regardless of order.
- **Confluence storage format** is converted into the block document (ADR-0001, ADR-0014). Known elements become blocks; links to imported pages become mentions; **Jira issue macros become embeds** of the imported issue; unknown macros become a visible "unsupported content" block that keeps the original, never raw HTML.
- **Jira:** issue types → types; custom fields → fields of matching kind (unmappable kinds reported); statuses and transitions → workflows; sub-tasks → children (containment); epic and other hierarchy and issue links → typed relations; comments, attachments, labels, watchers → their universal counterparts.
- **Confluence:** spaces → spaces; page tree → containment; page restrictions → restrictions (ADR-0022); comments, attachments, labels → universal counterparts.
- **Users** are mapped to principals by a configurable rule (e.g. e-mail); unmapped users become placeholder principals that can be merged later, so authorship is never lost.
- **Permissions** are mapped conservatively: when a source permission cannot be represented exactly, the import chooses the **more restrictive** option and reports it. An import never makes content visible to more people than in the source.

### Dry run and report

Every run can be a **dry run** that writes nothing and produces a report: counts per type, unmapped fields and statuses, unsupported macros, unmapped users, permission approximations, items refused by validation. A real run produces the same report. The report is the tool people use to fix the mapping before the real run.

### Consequences

- **Positive:** migration can be rehearsed, corrected and repeated; teams can move gradually without conflicting edits; cross-tool links become native embeds; nothing unmappable is silently dropped; keys and links people know keep working.
- **Negative / trade-offs accepted:**
  - Readers for several APIs and export formats are substantial work and depend on interfaces the project does not control; they will need maintenance when Atlassian changes them.
  - Synced items cannot be edited in wikiwikibangbang during the transition — a deliberate limitation to avoid two masters.
  - Mapping Jira workflows and custom fields can never be complete for every installation (scripted fields, app-specific data); the report makes gaps visible but does not close them.
  - Provenance and raw source add storage per imported item.
- **Follow-ups / risks to watch:**
  - The data model changes listed above (provenance, key preservation, sync marker, raw retention, imported history) — to be prototyped in `internal/item` before acceptance.
  - A corpus of anonymised sample exports for golden-file tests of the converters.
  - Assumption to verify: Atlassian Cloud may not expose users' e-mail addresses, depending on their privacy settings, which affects user mapping. Not verified; the Confluence Cloud reader does not rely on them (see below).
  - Assumption to verify: the support horizon of Atlassian's Data Center products changes the urgency of export-file readers. Not verified.
  - Crypto-shredding (ADR-0008) also applies to retained raw source.

## Prototype (2026-10-17)

The data-model requirements are prototyped in `internal/item` (`import.go`: `Origin`, `Upsert`, `FindByOrigin`, key preservation, `Managed` and `Release`; raw source removed by purge and redaction; an `unsupported` block kind). The engine is prototyped in `internal/migrate`: the canonical source model (`Snapshot`), `Derive` for the default mapping, `ConvertStorage` for the Confluence storage format, and `Run` with dry run, two passes, idempotent re-runs, one-way sync marking and a report. Readers for real APIs and export files are **not** built; the `Snapshot` is the seam they plug into (open question).

The acceptance test imports a Jira project (custom fields of five kinds and one unmappable one, a workflow, a sub-task, an issue link) and a Confluence space (page tree, Jira macro, page link, info macro, read restriction) into one space; the Jira macro becomes an embed of the imported issue. Re-running changes nothing; changed source objects are updated; refused objects and everything unmappable are reported. Three mutations of the engine (non-idempotent bodies, widened restrictions, macros not embedded) are caught.

Found while prototyping:

1. **Import sets state directly.** D3 says state changes only through transitions; an imported issue must arrive in its current source status. The prototype sets it directly, validated against the type's states, and AGENTS.md records this as the one exception to D3.
2. **A shared space shares its key counter.** When a Jira project and a Confluence space map to the same space, pages and native items take keys after the highest imported issue key. During a sync, an issue created later in Jira can find its key already taken. It is then imported under a new key, its source key stays in the origin, and the report says so. Whether to map Jira projects and Confluence spaces to separate spaces during a sync, or to accept renamed keys, needs a decision.
3. **Confluence links name their target by title.** Page links in storage format carry space and title, not an ID, so resolution is by title within the snapshot; a page renamed between runs breaks links until the next full run.
4. **Lenient parsing is right for storage format.** Unclosed inline tags are recovered without loss, as in HTML; a body cut off inside a tag keeps the text before it and is reported. `<br>` becomes a line break.
5. **Issue types without a workflow** import without a state; an existing type with the derived name is used as it is, and mismatches surface per item.
6. **Removed in the source** is reported, never deleted (D8): a person decides to trash.
7. **The dry run copies the store by replaying its log.** Correct, but its cost grows with the log; snapshots from ADR-0008 will be needed.
8. Not yet built: readers, imported history (changelogs, page versions), attachments, comments, labels.

## Confluence Cloud reader (2026-10-19)

The maintainer chose Atlassian Cloud through its REST APIs as the first source. `internal/migrate/confluence_cloud.go` reads spaces and pages with their storage format through API v2 (`/wiki/api/v2/spaces`, `/wiki/api/v2/pages`, following the `_links.next` cursor) and page restrictions through API v1 (`/wiki/rest/api/content/{id}/restriction/byOperation/{read|update}`), and produces the Confluence part of a `Snapshot`. The command `wikiwikibangbang import -from atlassian-cloud` runs it against a stopped server's data directory, with the account's e-mail address and API token from the environment.

1. **Restrictions fail closed.** A restriction that cannot be read (an answer other than 200, after retrying rate limits) or has an unexpected form restricts the page to a placeholder no one holds, and the report says so. A group in a restriction becomes a placeholder too: its members lose access until groups are mapped. An import never shows a page to more people than the source did.
2. **Accounts are mapped by a list.** The reader sees account IDs only; the command maps them to local accounts by a file of `accountId username` lines. A username without a local account stops the import, since a placeholder would lock its owner out silently. Unmapped accounts become placeholders, as before.
3. **The token stays on the site.** Only paths of the site are followed; an absolute or scheme-relative next link is refused, and plain http is accepted only for the local machine.
4. **Pages under a folder or another non-page parent** are placed at the top of the space and reported.
5. **Not verified against a live site.** The endpoints and field names follow Atlassian's published models as mirrored in an open-source client library, since Atlassian's documentation could not be reached while building. The tests use a fake site; the first import against a real site is the check, and an unexpected answer restricts and reports rather than opens.
6. Still not built: comments, attachments, labels, page history, and mapping groups.

## Jira Cloud reader (2026-10-19)

`internal/migrate/jira_cloud.go` reads projects through REST API v3: the field list (`/rest/api/3/field`), each project and the statuses of its issue types (`/rest/api/3/project/{key}/statuses`), and its issues through the token-paged search (`POST /rest/api/3/search/jql`). The import command reads Confluence spaces and Jira projects into one snapshot, so page links to issues resolve in the same run.

1. **Workflows without transitions.** Reading Jira's transitions needs the workflow scheme and workflow APIs; the reader takes the statuses only and lets every state be reached from every other, reported once per type. The initial state is the first status of the category "new". Reading the real transitions is a follow-up.
2. **Fields from use.** A type's fields are the custom fields, and a short list of system fields (assignee, reporter, priority, dates, resolution …), that its issues hold a value for; an option field's values are those its issues use. Arrays (labels, components, versions) are reported as unmappable; their values stay in the raw source.
3. **Rich text becomes plain text.** Descriptions and rich-text fields in the Atlassian Document Format become paragraphs; lists, tables and task items keep their text, links keep their address, attachments become a placeholder and are reported.
4. **Links once.** Every issue link appears on both issues; the reader imports it from its outward side only.
5. **Parents outside the import.** An issue whose parent (an epic in another project) is not read is placed at the top of its space and reported, instead of being refused.
6. **"Removed in the source" looks only at what a run read.** A run that reads only some projects or spaces no longer reports the items of the others as removed.
7. Not verified against a live site, as for the Confluence reader; the models of the same open-source client library were the reference.

## Pros and cons of the options

### 1. Generic CSV import only

Strongest form: minimal engineering; CSV exports exist for Jira; users control the mapping in a spreadsheet.

- Good: cheap; universal.
- Bad: loses structure — page trees, bodies, macros, links, attachments, history, permissions; no re-runs, no sync; Confluence content does not fit CSV at all.

### 2. One-time direct import

Strongest form: a "big bang" is simple to reason about — one run, then the old tool is switched off; no sync machinery.

- Good: far less complex than sync.
- Bad: organisations rarely move all teams at once; without provenance, a corrected second run duplicates everything; the source keeps changing between rehearsal and go-live.

### 3. One engine, snapshot import and one-way sync

- Good: rehearsable, idempotent, gradual adoption, one master per space, native cross-links.
- Bad: largest up-front investment in readers and provenance; synced items read-only during transition.

### 4. Bidirectional synchronisation

Strongest form: teams can work in whichever tool they prefer, for as long as they like; adoption has no deadline and no friction.

- Good: no forced switch.
- Bad: two masters produce conflicts that need rules for every field, body and workflow; Jira workflows and wikiwikibangbang workflows cannot be kept equivalent; edits made in one system can be rejected by validation in the other; writing back requires write access to the source and a mapping in both directions — a permanent integration product rather than a migration path.

### 5. Third-party migration tools

Strongest form: specialised vendors already handle Atlassian's formats and edge cases; no engineering effort for the project.

- Good: zero initial effort.
- Bad: no such tool targets a new product; they would need wikiwikibangbang-specific output; the project's adoption would depend on others' roadmaps and licences.

## Links

- builds on [ADR-0001](0001-unified-item-data-model.md) (types as data, keys, relations), [ADR-0014](0014-closed-renderer-for-user-content.md) (no raw HTML), [ADR-0020](0020-embedding-and-containment-are-independent.md) (embeds), [ADR-0022](0022-permission-model.md) (permissions, sessions)
- relates to [ADR-0008](0008-event-sourced-persistence.md) (imported events, crypto-shredding), [ADR-0011](0011-http-api-route-table-and-access-classes.md) (redirects from old URLs)
- ADR-0001 named "import" as a follow-up record; this is it
