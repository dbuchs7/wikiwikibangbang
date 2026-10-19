# ADR-0044: Labels are part of their item's content, set as a whole, counted per space

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for the wiki to come as close as possible to the wiki it replaces, 2026-10-19)

## Context and problem statement

A page tree orders pages one way (ADR-0041); people also group them across the tree: every runbook, every page about one customer, every decision of a quarter. Wikis do that with labels: short words on a page, a list of the pages with a label, and the labels of a space. ADR-0001 lists universal capabilities that exist once for every item; labels are one, for pages and issues alike (D6).

## Decision drivers

- One model for every item (D1, D6): no page-only feature.
- A label is written by a person and can hold anything, so it is content: readable exactly when its item is (ADR-0022), sealed with the item's key and gone when the item is purged or redacted (ADR-0026), and read by the governance check (ADR-0040).
- Labels are compared by people and by queries, so two spellings of one label must not become two labels.

## Considered options

1. **A list of labels on the item**, replaced as a whole by one event
2. **Labels as items** of a built-in type, related to what they label
3. **A property** of a type, defined per type

## Decision outcome

Chosen option: **1**.

- **Model:** `Item.Labels`, sorted and unique. A label is lower case, letters and digits joined by `-`, `_` or `.`, up to 64 characters; what a person types is trimmed, lowered, and inner spaces become `-` (`Disaster Recovery` becomes `disaster-recovery`). Anything else is refused rather than silently changed further.
- **Event:** `LabelsSet` carries the item's labels after the change, so a change is one event and a replay needs no merging. Setting the labels an item already has records nothing. The labels are sealed content; purge and redaction erase them, from the item and from its history.
- **Rights:** setting labels needs the right to edit the item, like its title.
- **Queries:** `Query.Label` finds the items carrying a label, in listings and in search. `Session.Labels(space)` counts the labels of the readable live items of a space (or of all), most used first.
- **Governance:** labels are among the texts the sensitive-data check reads; a card number used as a label restricts its item like one in its title.
- **Format:** `CodecVersion` 7 adds the kind `LabelsSet` and the field `labels`. Versions 1 to 6 are still read.
- **API:** `PUT /api/items/{ref}/labels` replaces them; `GET /api/labels?space=` counts them, at most a page of the most used, with the total; `label=` filters `GET /api/items` and `GET /api/search`. Items carry `labels`; the history shows a `LabelsSet` with its labels. The MCP adapter (ADR-0012) offers `label` as a filter of `find_items`; setting labels is not offered.
- **UI:** a page shows its labels under its title, each a link to the list of readable items carrying it; on a live item a field adds one and a cross removes one. A space shows its labels with their counts.

### Consequences

- Good: one capability for pages and issues; erasure and governance cover labels by construction.
- Good: the event holds the result, so concurrent edits end in one of the two lists, never in a mix nobody chose.
- Bad: two people adding different labels at the same moment: the later list wins and the earlier label is lost; the history shows both.
- Bad: counting labels scans the items of the space, as search does (ADR-0033).
- Neutral: the import does not bring labels yet; a list of labelled pages inside a body is not built.

## Pros and cons of the options

### 2. Labels as items

- Good: a label could have its own description and rights.
- Bad: a relation per label and item; a page with five labels becomes six items to read, erase and permit.

### 3. A property per type

- Good: no new event kind.
- Bad: every type would have to define it, and a query across types would need to know each type's field; D6 asks for capabilities that exist once.

## Links

- ADR-0001 (universal capabilities), ADR-0012 (MCP), ADR-0015 (paged listings), ADR-0022 (permissions), ADR-0026 (erasure), ADR-0033 (search), ADR-0040 (governance), ADR-0041 (page tree)
