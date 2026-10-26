# ADR-0127: Connectors read any JSON source into a type of one's own

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked which further kinds of connector the administration should offer and how connectors are to be solved cleanly, and chose to rebuild them: a reader per kind of source, one canonical entry, a type and field mapping per connector, a generic JSON kind and connectors kept to public addresses, 2026-10-26)

## Context and problem statement

ADR-0077 made connectors: several sources, each on its own schedule, with a dry run, a report and an audited run. Yet the only kind it reads is CKAN, and what a run writes is fixed: one ordinary page per data set (ADR-0093), whose fields nobody can fill from the source. An organisation's own registries, service catalogs or inventories rarely run CKAN; they answer JSON over http in a shape of their own. ADR-0077 also left open that a connector reads any address an administrator sets, the server's own network included. How does the wiki read further kinds of source, write what they give into the fields of a type, and keep a connector from reaching what it should not?

## Decision drivers

- What ADR-0075 and ADR-0077 decided for a run holds for every kind of source: upsert by origin (ADR-0023), what people add is kept, withdrawal instead of deletion, a dry run, an audited run, credentials from the environment.
- A new kind of source is code in one place, not a second runner.
- Most sources can be read without code: an administrator names where in the answer the entries and their values are.
- A field's value has the field's kind; a value that does not fit is reported, never stored wrong.
- A connector cannot be pointed at the server's own network unless the operator, not an administrator, allows it, as for webhooks (ADR-0062) and the link checker (ADR-0039).
- Only open source without licence fees; nothing outside the standard library.

## Considered options

1. A reader per kind behind one interface, one canonical entry, a type and field mapping per connector, and a generic JSON kind whose defaults read a feed format of the connectors' own
2. A kind per product (Backstage, ServiceNow, LeanIX, …), each with its own settings
3. An OpenAPI kind that reads any described API
4. No further kinds: adapters outside the server write through the API or MCP

## Decision outcome

Chosen option: **1**, because a JSON source with configurable paths reads most registries without code, an adapter for any other system needs only to write the feed format, and the canonical entry keeps scheduling, mapping, withdrawal and the report in one place for every kind.

- **Readers.** A `Reader` turns a connector's source into entries (`internal/connector/reader.go`). The kinds are a registry: `ckan` (ADR-0075) and `json`. A run asks the reader of its connector's kind; everything after the reading — applying entries, withdrawing, the report, the audit — is the same for every kind.
- **The canonical entry.** Every reader gives the same `Entry`: an ID in the source, a title, a link to the entry in the source, the page's text and the entry's further values by name. A CKAN entry gives `dataset`, `publisher`, `contact`, `url` and `formats`; a JSON entry the values its connector names.
- **The JSON kind.** Its settings are the address of a list and paths into the answer — keys and list positions joined by dots, e.g. `result.items` or `data.0.rows`: to the list, and within an entry to its ID, title, link, text, and the address of the next page. Pages are followed up to 50 times, each answer up to 64 MB, as for CKAN. Further values are named with a path each. The defaults read the connectors' feed format, `{"entries": [{"id", "title", "url", "text", …}], "next": "…"}`, which an adapter for any other system can write. A credential, `WWBB_CONNECTOR_<ID>_TOKEN`, goes as a bearer token. An entry without an ID, with an ID given twice or longer than 200 characters, and a next page that is no web address are reported and left out.
- **Type and fields.** A connector names the type of the items it writes; empty is `page`, which a run defines if the site has none (ADR-0093). The type needs a text; a field it requires must be mapped, and a mapped field must exist and not hold a user, as no source names one of the wiki's users. `fields` maps a field to the name of a value the source gives; saving checks that the source gives it. Every run converts the value's text to the field's kind — a number, true or false, a date as the CKAN dates are read, one of an enum's values, a web address — and writes it; an empty value clears the field. A value that does not fit is reported in the run and the field left as it was. Fields not mapped stay as people set them. The type cannot change once a connector has written items, as an import does not change an item's type; a run that finds them of another type stops and says so.
- **Origin.** A JSON entry's item has the external ID `entry/<ID>` and the list's address as its instance; a CKAN page keeps `page/<ID>` (ADR-0093). Erasing the entries of before ADR-0093 is the CKAN kind's alone.
- **Public addresses only.** Every request a connector makes, redirects included, is checked before it is sent: a name must resolve to public addresses only (`linkcheck.Public`). A source in the server's own network needs the operator to set `WWBB_CONNECTOR_ALLOW_PRIVATE=on`. Requests still go through the proxy the environment names; a name the server cannot resolve itself is then left to the proxy, which an organisation's network may require.
- **API and UI.** `GET /api/connectors` also lists the types an item can be written as, with their fields, and the values a CKAN source gives. *Administration → Connectors* offers a new connector per kind; a JSON connector is set up with its address, paths and values; every connector has a section for what it writes: the type and, per field, the source's value.

### Consequences

- **Positive:** a registry, a service catalog or an inventory that answers JSON becomes a connector without code, its values in the fields of a type with the catalog rules of ADR-0074.
- **Positive:** a new kind of source is one reader; scheduling, mapping, withdrawal and the report come with it.
- **Positive:** a connector no longer reaches the server's own network unless the operator allows it.
- **Negative / trade-offs accepted:** an existing connector to a source in the own network stops working until `WWBB_CONNECTOR_ALLOW_PRIVATE=on` is set; its runs say so.
- **Negative / trade-offs accepted:** the check of a name and the connection are two lookups; a name that changes its address between them is not caught, as for webhooks. A proxy is trusted to keep to the organisation's rules.
- **Negative / trade-offs accepted:** a source whose list is not reachable by paths alone — XML, a POST query, a cursor in a header — needs an adapter that writes the feed format.
- **Follow-ups / risks to watch:**
  - the Atlassian import (ADR-0023) as a kind of connector, so it runs on a schedule with a dry run;
  - an OpenAPI kind that offers an API's described lists to choose from;
  - kinds for DCAT catalogs, the Swiss interoperability platform I14Y, Git repositories and CSV files;
  - a user field filled by matching a source's e-mail address to an account.

## Pros and cons of the options

### Option 2: a kind per product

Strongest form: each product's connector knows its API, paging and authentication, and asks the administrator nothing about paths.

- Good: the least to set up per source.
- Bad: every product is code to write, test and keep up with its API, for a list that never ends; most organisations' own registries are no product at all.

### Option 3: an OpenAPI kind

Strongest form: an API described by OpenAPI names its lists and their fields itself, so the administrator only picks.

- Good: the best guidance where a description exists.
- Bad: many sources have none or an inaccurate one, and the paths would still have to be chosen. It is a follow-up that fills the JSON kind's settings, not a replacement.

### Option 4: adapters outside the server

Strongest form: the server never calls out, and adapters in any language write through the API or MCP with the caller's rights.

- Good: no outbound calls.
- Bad: as ADR-0077 found, scheduling, the dry run, the report and withdrawal would be rebuilt by every adapter. An adapter that writes the feed format keeps them in the server, which this decision allows.

## Links

- amends [ADR-0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md) (further kinds, a type and fields per connector, public addresses only)
- relates to ADR-0023 (origin), ADR-0039 and ADR-0062 (public addresses only), ADR-0074 (catalog rules), ADR-0075 (CKAN), ADR-0093 (pages)
