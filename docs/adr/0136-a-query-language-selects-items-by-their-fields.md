# ADR-0136: A query language selects items by their fields

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for a query language over the items' fields, 2026-10-26); proposed for the maintainer's acceptance
- **Open question:** Should a query follow relations, as ADR-0001's example `relation(implements) -> state != done` does, and how does a condition on the other end stay silent about an item the caller may not read? And should a query be kept by name and shared, and if so as an item, as a system object, or only as its address, as now?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0001 promised "one query language for all content" and named an ADR on the query language as a follow-up; it was never written. Items are found today in three ways: the full-text search, which ranks words (ADR-0033, ADR-0083); the filters of a list, which narrow by one space, type, state or label and by words (ADR-0098); and `where`, which compares a property with a text (ADR-0074). None of them joins conditions with OR or NOT, compares numbers or dates by size, asks for "mine", "overdue" or "changed this week", or orders the answer by a field.

A team that plans work and keeps knowledge in one model needs exactly such questions across spaces and types: the open tasks assigned to me, by due date; the decisions changed last week; every page below a handbook that still lacks an owner. How are items selected by their fields, in the UI, the API and for agents?

## Decision drivers

- One model (D1, D6): every type's fields are queried the same way, with no code for a type; a field is what a type definition declares.
- Unreadable is non-existent (D10): the answer, its count, its order and its error messages say nothing about an item the caller may not read; protected passages (ADR-0088) are not searched.
- Erasure stays complete (ADR-0026): no second copy of content in an index or another store.
- People write the queries: readable words, a mistake answered with where it is.
- The same question in the UI, the API, the CSV export and for agents over MCP (ADR-0012).
- Bounded cost (ADR-0015): a query is checked against every readable item.
- One binary, pure Go, no new dependency without need (ADR-0004, ADR-0009).

## Considered options

1. **A query language of the product's own**: conditions on fields joined with AND, OR and NOT, and ORDER BY, parsed by a small package and evaluated over the live state through the caller's session
2. **More list filters**: operators for `where` (`estimate>3`) and further parameters, chosen in the UI, without a language
3. **An existing expression language** such as CEL, evaluated over each item
4. **SQL** over a copy of the items in an embedded database
5. **Search operators** in the search field (`type:task state:open`)

## Decision outcome

Chosen option: **1**, because it answers every question above with words people read, is evaluated by the same permission check and veiling as every other read, and needs neither a dependency nor a copy of the content.

### The language

`internal/query` reads a query; it knows the language, not the items.

```text
query  = [ or ] [ ORDER BY sort { "," sort } ]
or     = and { OR and }
and    = not { AND not }
not    = NOT not | "(" or ")" | clause
clause = field ( op value | [ NOT ] IN "(" value { "," value } ")" | IS [ NOT ] ( EMPTY | NULL ) )
op     = "=" | "!=" | "<" | "<=" | ">" | ">=" | "~" | "!~"
value  = word | "text" | 'text' | function "(" [ argument ] ")"
sort   = field [ ASC | DESC ]
```

- Keywords and the names of built-in fields are read without regard to case; AND binds closer than OR. A word runs up to a space or one of `()=!<>~,"'`, so `PLAT-12`, `2026-10-01` and `-7d` are words; anything else is quoted, with `\` before a quote inside.
- **Fields of every item:** `id`, `key` (current or former, ADR-0086), `title`, `type`, `space`, `state`, `parent` and `ancestor` (an item by key or ID), `label` (also `labels`), `text` (title, key, body, text properties, labels and comments, as search reads them), `body`, `comment`, `created` and `updated` (points in time), `creator`, and `finished` (the state is one no transition leaves, ADR-0082).
- **Properties:** any other name is a property, as type definitions declare it; the same name may have different kinds in different types, and each item is compared by the kind its own type version gives the field. A quoted name always names a property, so a property called `created` (as imports bring, ADR-0023) is reached as `"created"`, and names with spaces as `"due date"`. The reserved field names of ADR-0001 cannot be properties and stay built-in when quoted.
- **Comparisons:** `=`, `IN` and `IS EMPTY` everywhere. `<`, `<=`, `>`, `>=` for text (by letters), numbers, dates and enums, an enum by the order of its values (`priority >= high`), a state, when ordering, by its place in the workflow. `~` asks for every word of the value somewhere in the field's text, as the search's words (letters and digits, case and `ß`/`ss` folded); it takes text fields, `title`, `text`, `body` and `comment`. Text, names, enums and labels compare without regard to case.
- **Negation is two-valued:** `a != v`, `a NOT IN (…)` and `a !~ v` hold exactly where `a = v`, `a IN (…)` or `a ~ v` do not, for an item without a value of `a` too. `IS NOT EMPTY` narrows to items with a value.
- **Time:** a date `2026-10-01` names a whole day, a time is RFC 3339; `-7d`, `2w`, `+12h` and `-1y` count from now; `now()`, `startOfDay()`, `endOfDay()`, `startOfWeek()`, `endOfWeek()`, `startOfMonth()`, `endOfMonth()`, `startOfYear()` and `endOfYear()` take an optional offset, a whole number in their own unit (`startOfMonth(-1)`) or a span added to the start or end (`startOfDay(+8h)`). Weeks start on Monday; days are those of UTC. Compared with a whole day, and always for a date field, a time counts by its day.
- **People:** `currentUser()` is the caller. In a user field and `creator`, a word is a username, in any case, or a user ID. An unknown name stands for nobody, without an error, so a query does not tell which accounts exist.
- **Order:** `ORDER BY` one or more fields, ascending unless `DESC`; items without the value come last either way; values of different kinds sort by their kind; items ordered alike keep the order they were made in, which is also the order without `ORDER BY`. `text`, `body`, `comment`, `parent` and `ancestor` do not order.
- **Mistakes** are refused with the character where they are: syntax, an unknown field (with its spelling when only case differs), a comparison the field does not take, a value no definition of the field can hold (an unknown type, state or enum value, a word that is not a date). The UI puts the cursor there.
- **Budgets:** a query is at most `QueryBytes` long (2048) and has at most `QueryTerms` conditions (64), each value of a list counted as one (ADR-0015).

### Evaluation

`Session.Select` and `Session.SelectPage` (`internal/item/select.go`) compile the query against the type definitions and scan the live items, as search does (ADR-0033): only items the caller may read are candidates, and each is matched as the caller reads it, protected passages veiled (ADR-0088). The answer is veiled the same way. A key or ID in `key`, `parent` or `ancestor` names an item only when the caller may read it; otherwise it names nothing, with the same empty answer as a key that does not exist (D10). `created` and `creator` are the time and actor of the event that made the item; `updated` is the time of the newest event that changed the item itself: its title, properties, state, body, labels, place, files or comments, not relations or access. No index is kept and nothing is written.

Measured with `BenchmarkSelect` over 10,000 items on four cores of a development container: about 8 ms for conditions on fields with an order, 16 ms for `text ~` over pages of about 1 KB, 15 ms for `updated` with an order.

### Where it is offered

- **API:** `GET /api/items?query=…` selects and orders; the listing's other parameters (`space`, `type`, `state`, `label`, `parent`, `where`, `q`) narrow it further; paged as every listing (ADR-0015). A mistake is answered `400` with `error` and `at`, the character from 0. `GET /api/items.csv?query=…` exports the same items in the same order (ADR-0071).
- **MCP:** the tool `query_items`, one request to that listing with the caller's credential (ADR-0012); its description teaches the language.
- **UI:** the view *Query* (`#/query?q=…`), reached from the search page, where the search's words become `text ~ "…"`. The query is kept in the address, so it survives a reload and can be bookmarked and shared (ADR-0098). The view shows the answer as a table with *More* and a CSV link, a help with the built-in fields, the fields of the installation's types (a click puts one at the cursor), the comparisons and examples. Ctrl+Enter runs the query.

### Consequences

- **Positive:** questions across spaces and types that no list filter could ask: "mine", "overdue", "changed this week", "below this page", in any order.
- **Positive:** correct by construction for permissions and erasure: the same candidates, the same veiling, no derived state.
- **Positive:** people, scripts and agents ask the same question in the same words.
- **Negative / trade-offs accepted:** cost grows with the content, as for search; reads run side by side (ADR-0081), so a slow query delays no write. An index (ADR-0033's option 2) would serve both when the scan becomes too slow.
- **Negative / trade-offs accepted:** a language to learn and to keep compatible: a query in a bookmark must keep its meaning. New fields and functions may be added; a changed meaning needs a new ADR.
- **Negative / trade-offs accepted:** days and weeks are those of UTC, not the reader's time zone; around midnight `startOfDay()` differs from the local day by the offset.
- **Negative / trade-offs accepted:** a built-in field shadows a property of the same name unless the name is quoted.
- **Follow-ups / risks to watch:**
  - relations in queries, and saved, named queries (the open question);
  - a board or kanban over a query instead of one space and type (ADR-0036);
  - completion of fields and values while typing;
  - the reader's time zone for days and weeks.

## Pros and cons of the options

### Option 2: more list filters

Strongest form: nothing to learn; every list already has a filter (ADR-0098), and choices in the UI cannot be mistyped.

- Good: no parser, no syntax errors; the filter bar stays the one way of narrowing a list.
- Bad: every operator, every OR and every order becomes a parameter and a control of its own; a question that spans conditions ("mine, or unassigned and urgent") cannot be asked at all. The filters stay what they are, the narrowing of one list.

### Option 3: an existing expression language (CEL)

Strongest form: CEL is mature, typed, sandboxed, with cost limits, and has a pure Go implementation under Apache-2.0; nothing of the language would have to be designed or maintained here.

- Good: a proven evaluator and type checker.
- Bad: it is written for programmers (`item.props["due"] < timestamp("…")`), knows no `ORDER BY`, no "every word occurs", no keys of readable items and no relative dates; each would be a function of our own on top, so the language people read would be ours anyway, with a large dependency (protobuf, a parser runtime) underneath.

### Option 4: SQL over an embedded database

Strongest form: SQL is the best-known query language there is, and a database brings indexes, joins and an optimiser.

- Bad: a second copy of every item's content on disk, which crypto-shredding (ADR-0026) would have to reach on every purge; SQLite needs CGO (ADR-0004), a pure Go port is a large dependency; permissions, veiling and keys of readable items would have to be mirrored into every statement, or applied after it, which leaks counts and order. People do not write joins over a schema of items, properties and relations.

### Option 5: search operators

Strongest form: familiar from web search; one field for everything.

- Good: no new view.
- Bad: search ranks by words, a query selects exactly; mixing them makes both harder to explain (ADR-0083). OR, ranges and order turn the operators into a language anyway, without its grammar and its errors.

## Links

- answers the follow-up "query language" of [ADR-0001](0001-unified-item-data-model.md); indexing stays [ADR-0033](0033-full-text-search-by-scanning-the-live-state.md)'s scan
- relates to [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets, paging), [ADR-0022](0022-permission-model.md) (D10), [ADR-0026](0026-crypto-shredding-of-item-content.md), [ADR-0012](0012-mcp-adapter-over-http-api.md), [ADR-0071](0071-exporting-pages-and-lists.md), [ADR-0074](0074-catalog-rules-for-types.md) (`where`), [ADR-0082](0082-a-state-no-transition-leaves-is-finished.md), [ADR-0088](0088-protected-passages-of-a-body.md), [ADR-0098](0098-every-list-can-be-filtered.md)
