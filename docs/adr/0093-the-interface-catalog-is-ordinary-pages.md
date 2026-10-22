# ADR-0093: The interface catalog is ordinary pages

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators. The maintainer asked to keep the interface catalog as ordinary wiki pages instead of its own type. They chose:
  - every interface becomes an ordinary page;
  - the connector stays and writes pages;
  - the existing entries are rebuilt, not converted.

  (2026-10-22)

## Context and problem statement

A connector mirrors the interfaces of a source, such as opendata.swiss, into a space (ADR-0075, ADR-0077). Each data set becomes an item of its own type, `interface` (ADR-0074), with the following:

- **Fields:** source ID, format, endpoint, specification, publisher, owner, contact, data set, modified, reviewed.
- **Workflow:** active and withdrawn.
- **Review:** a rule to check the entry once a year.

Since ADR-0080 the same item also has a page, written from everything the source says. The fields repeat what the page shows. The workflow puts boards and states on something that is no task, and the catalog does not read like the rest of the wiki.

How does the catalog become ordinary pages, while the source still keeps it current?

## Decision drivers

- A data set is read as a page, like any other page.
- The connector keeps the pages in step with their source, and nothing added here is lost: comments, labels and the place in the tree.
- An entry erased here stays erased (ADR-0026). A rule that protects erasure is not weakened to rebuild the catalog.
- Catalog rules on types (ADR-0074) stay available to other types.

## Considered options

1. **Pages of the type `page`; the connector writes their text and marks withdrawn ones with a label; old entries are erased and written anew as pages.**
2. **Convert the existing entries into pages in place.**
3. **Keep the type and only show its entries like pages.**

## Decision outcome

Chosen option: **1**.

### Pages

- **Type:** each data set with interfaces is an item of the type `page`, with a text and no fields or workflow.
  - The connector defines that type when the site has none.
  - An existing `page` type must have a text and no field a synchronisation would have to fill.
- **Text:** written from everything the source says (ADR-0080). Every run writes it anew when it changed, as before.
- **Kept across runs:** comments, labels, the page's place in the tree, and any fields its type has.
- **Identity:** the origin names the connector and the source's installation. The data set's ID is held as `page/<id>`.
  - This identity differs from the old entry's, so erasing the old entry does not stop its page from being written.
  - The rule that an erased item is not imported again stays as it is.

### Withdrawn data sets

- **Marking:** a data set the source no longer publishes is not deleted. Its page gets the label `withdrawn` and a warning at the top of its text, in the page's language.
- **Coming back:** when the data set comes back, the label goes and the text is the source's again.
- **Safeguard:** as before, a source that answers far fewer data sets than the catalog holds marks nothing.

### Old entries

- **Removal:** a run erases the connector's entries of the type `interface`, by trashing and purging them (ADR-0021, ADR-0026), and writes pages in their place.
- **Referenced entries:** an entry that something still points to stays, and the run names it.
- **Dry runs:** a dry run counts what would be removed.
- **The type itself:** it stays defined but unused; administrators may leave it or reuse it.

### Steward

- **Watching:** the connector's steward now watches every new page (ADR-0046), and so hears of what happens to it.
- **No owner:** a page has no owner field to set.

### Interface

- **Connector list:** it counts pages.
- **Run results:** they count removed old entries beside new, changed, unchanged and withdrawn ones.
- **Agent guide:** `docs/guides/catalog-agents.md` describes the catalog as pages.

### Consequences

- **Positive:**
  - The catalog reads like the rest of the wiki.
  - No fields repeat the text.
  - No boards or states sit on something that is no task.
- **Negative / trade-offs accepted:** the history, comments and fields of the old entries are erased with them, as the maintainer chose. Owners, contacts and reviews recorded on entries are lost.
- **Negative / trade-offs accepted:**
  - Without fields, the catalog loses the following: finding a data set by its ID with `where`, governance findings on empty recommended fields, the yearly review, and the link check of an endpoint field.
  - What remains: search, labels, the origin's link to the data set, and links in the text, which the link check covers.
- **Negative / trade-offs accepted:** an entry that something points to stays until the reference is removed and the next run erases it.
- **Neutral:** the catalog rules on types (unique, url, recommended fields, reviews; ADR-0074) remain for other types.

## Pros and cons of the options

### Option 2: convert in place

Strongest form: the history, comments and fields would stay.

- Bad: an item's type does not change in this model (ADR-0001). Converting means a new item anyway, with the old one's history copied or lost. The maintainer chose to rebuild.

### Option 3: keep the type, show it like pages

Strongest form: no data changes.

- Bad: the fields, workflow and review stay, with their boards and findings. That is exactly what the maintainer wants gone.

## Links

- amends [ADR-0074](0074-catalogs-kept-in-step-with-other-systems.md), [ADR-0075](0075-mirroring-the-interfaces-of-opendata-swiss.md), [ADR-0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md) and [ADR-0080](0080-one-page-per-data-set-written-from-the-source.md)
- relates to ADR-0021 (deletion), ADR-0023 (imports and origins), ADR-0026 (erasure), ADR-0046 (watching), ADR-0044 (labels)
