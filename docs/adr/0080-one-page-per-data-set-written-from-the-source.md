# ADR-0080: One catalog page per data set, written from everything the source says

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for one page per interface description that shows, normalised, every field the connector receives; chose that the source owns the page and that notes are comments, and one page per data set, 2026-10-21)

## Context and problem statement

ADR-0075 made one catalog entry per resource in an interface format. The entry carried a handful of fields, and its text was left to the catalog. A data set offering the same data as a WMS, a WFS and an API became three near-identical entries, each with its own owner and review. Their pages were empty apart from the fields, while the source holds much more: a description, keywords, themes, licence, period, frequency, contact, related documents, other resources. How should a catalog entry show all of that, and what is one entry?

## Decision drivers

- Everything the source says of a data set is readable in the wiki, normalised and in one place.
- One owner and one review per thing that is governed.
- Nothing added here is lost by a run: owner, review, contact, comments.
- The page is written in the language the catalog is read in, as far as the source has it.

## Considered options

1. **One entry per data set with interfaces. Its page is written from the source on every run; notes are comments.**
2. **As 1, but the page written only when the entry is created**, then left to the catalog.
3. **As 1, but the source's part in a read-only section of its own**, with the page's text left free.

## Decision outcome

Chosen option: **1**.

- **Entry.** A data set with at least one resource in an interface format is one item of the type `interface`.
  - `source-id` and the origin's external ID are the data set's ID.
  - `format` lists the formats of its interfaces, e.g. `WMS, WFS`.
  - `endpoint` is the first interface's address, and `specification` the first documentation.
  - `modified` is the latest date of the data set and its interfaces.
- **Page.** Every run writes the page anew, and only commits it when it changed. In order, the page holds:
  - a note that the page comes from the source and that notes belong in comments;
  - the description;
  - per interface: its title, description, format, media type, licence, terms of use, languages, size, dates, ID, and links to address, access, download, documentation and access services;
  - the data set's details: publisher, contact, keywords, themes, licence, period, update frequency, dates, languages, area, identifier, ID, and links to the data set in the source, its landing page, documentation and related resources;
  - its other resources, as links;
  - every further field of the answer that is not read by name and is not CKAN's own bookkeeping, as a table.

  Values are made one line and at most 2000 characters each. Only `http(s)` addresses become links.
- **Language.** A CKAN connector names the language of its pages: `de` (default), `fr`, `it` or `en`. The source's texts are taken in that language where it has them. The page's own words come from a translation table, English as the source, with German added. French and Italian pages use the English words until they are translated.
- **Kept.** Owner, review, a contact or specification added when the source has none, comments (of the whole entry, and of a passage; one whose passage changed says so, ADR-0047), relations, labels and access are not touched by a run. Text written into the page by hand is replaced by the next run; the history keeps it.
- **From one entry per interface to one per data set.** An entry of the earlier kind is withdrawn when the source still publishes its resource as part of a data set's entry. This applies whatever the share of the source's answer, and the run names the entry that took its place. Its comments and history stay with it.
- **Not tasks.** The task list (ADR-0049) lists the type `task`, and types with a workflow and a field named assignee, due or deadline. A catalog entry has a workflow and an owner, but is not a task.

### Consequences

- **Positive:** a catalog entry reads as a complete description of the data set and its interfaces, current with every run.
- **Positive:** one owner and one review per data set; fewer entries to govern.
- **Negative / trade-offs accepted:** the page cannot hold notes; they belong in comments. An entry's interfaces are no longer governed one by one.
- **Negative / trade-offs accepted:** the field names of the source's answer were taken from its schema, not from a live answer. A field the source names differently appears under the further details rather than in its place.
- **Negative / trade-offs accepted:** a type with a workflow but no assignee or due field, such as `decision`, is no longer in the task list.

## Pros and cons of the options

### Option 2: the page written once

Strongest form: people may write into the page freely, and the source only gives a start.

- Bad: the page grows stale with every change in the source, and nothing tells which part is still the source's.

### Option 3: a read-only section of the source's own

Strongest form: always current, and the text stays free for notes.

- Bad: it needs a second kind of content next to the body, stored and shown apart. With comments kept by every run, option 1 serves the same need with the body as it is.

## Links

- amends [ADR-0075](0075-mirroring-the-interfaces-of-opendata-swiss.md) (entries per data set; the page is the source's) and [ADR-0049](0049-a-task-inbox-after-atlas.md) (which types are tasks)
- relates to ADR-0035 and ADR-0047 (comments), ADR-0077 (connectors)
