# ADR-0135: A JSON source is read by page numbers or offsets

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for paging by page numbers and offsets, which ADR-0130 named as a follow-up, 2026-10-26)

## Context and problem statement

A JSON source (ADR-0127) reads a long list by the address of the next page that each answer names. Many APIs do not name one: they take a page number (`?page=3`) or an offset (`?offset=200`, `$skip`) in the address, often with the size of a page (`limit`, `per_page`, `$top`), and some name how many entries the list holds. Such a list was read to its first page only. As a run withdraws what it does not see, only the safeguard of ADR-0127 (nothing is withdrawn when far fewer entries come than the space holds) kept that from doing harm. How does a JSON source read such lists completely, and know when to stop, without a source that misbehaves keeping a run going?

## Decision drivers

- A list is read completely, or the run says why not; a missed page must not look like withdrawn entries.
- Sources differ: some name a total, many do not; some cap a page below the size asked; some ignore an unknown parameter and answer the first page again.
- Every run stays bounded.
- An API description (ADR-0130) names the parameters; they are taken from it.

## Considered options

1. A paging setting of the JSON source: page number or offset, its parameter and start, the page size and its parameter, and the path to the total; stop rules that tolerate capped pages
2. Paging by page number and offset that stops at the first page shorter than the size asked
3. Leave paging to adapters that write the feed format

## Decision outcome

Chosen option: **1**, because the stop rules decide from what the source actually answers, so a source that caps its pages is read completely, and a source that ignores the parameter is caught at once.

- **Settings.** A JSON source either follows the next page's address (`next`, ADR-0127) or has `paging`, not both: its kind, `page` (a page number) or `offset` (the number of entries to skip); the parameter that carries it and its first value (`start`, mostly 1 for pages and 0 for offsets); optionally the parameter that asks for a page's size and the size (up to 10 000); optionally the path to the number of all entries in the answer (`total`). The run sets these parameters in the list's address, replacing any it has.
- **When reading stops.**
  - Where the answer names the total, the run reads until it has that many entries, or until a page is empty.
  - Otherwise it stops at an empty page, or at a page shorter than the longest before it. A first page shorter than the size asked does not end the list, as a source may cap its pages below the size asked; its end then costs one more request.
  - Whatever the paging, a page that brings only entries already read stops the run and says so: the source ignores the parameter, or names the same page again.
  - No run reads more than 500 pages (before: 50), and it says so when it stops there.
- **From an API description.** A GET operation's query parameters of a number type, or of none stated, named as is usual — `page`, `pageNumber`, `page[number]`; `offset`, `skip`, `$skip`, `start`, `from`; `limit`, `per_page`, `pageSize`, `size`, `$top` and the like — become the paging, with the page's start from the parameter's default or minimum and its size from its maximum (up to 1000), else its default, else 100. A number named `total`, `totalCount`, `count` and the like in the object that holds the list becomes the total. A next page's address the answer names is preferred over a page parameter. A required page or size parameter no longer excludes an operation.
- **In the administration.** The JSON form says how a long list is read: by the next page's address, by a page number or by an offset, with the parameters, the first value, the page size and the path to the total.

### Consequences

- **Positive:** lists paged by number or offset are read completely; picked from a description, without setting anything by hand.
- **Positive:** a source that ignores its paging, by parameter or by next address, ends the run after one repeated page instead of after the page limit.
- **Negative / trade-offs accepted:** without a total, a list whose last page is full, or whose only page is shorter than the size asked, costs one request more, for the empty page that ends it. A source that answers an error instead of an empty page beyond its end fails that run; naming the total avoids it.
- **Negative / trade-offs accepted:** a run reads up to ten times as many pages as before, and takes as much longer against a large list.
- **Follow-ups / risks to watch:**
  - cursors in the answer that go into the next address as a parameter (`?cursor=…`) and the `Link` header of RFC 8288;
  - reading only what changed since the last run, where a source can filter by date.

## Pros and cons of the options

### Option 2: stop at the first short page

Strongest form: the fewest requests; a short page is the usual sign of a list's end.

- Bad: a source that caps its pages below the size asked, which is common, would end the list after its first page; the run would read a fraction and report no problem.

### Option 3: adapters

Strongest form: no paging rules in the server; an adapter knows its source.

- Bad: page numbers and offsets are the most common paging of all; every such source would need an adapter for what a setting does.

## Links

- amends [ADR-0127](0127-connectors-read-any-json-source-into-a-type-of-ones-own.md) (paging by page numbers and offsets; at most 500 pages; a repeated page stops a run)
- amends [ADR-0130](0130-an-api-description-fills-a-json-source.md) (the description's paging parameters and total are taken)
