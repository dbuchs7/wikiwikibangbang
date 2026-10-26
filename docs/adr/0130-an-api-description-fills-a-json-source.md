# ADR-0130: An API description fills a JSON source

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for the OpenAPI connector that ADR-0127 named as a follow-up, 2026-10-26)

## Context and problem statement

A JSON source (ADR-0127) is set up by naming paths into the API's answer: where the list is, and where each entry's ID, title, link, text and further values are. An administrator who sets one up has to read the API's answers to find them. Many APIs describe themselves in an OpenAPI document (OpenAPI 3, or its predecessor Swagger 2.0), in JSON or YAML: their operations, parameters and the schema of each answer. How does the wiki use such a description to connect an API, without a second way of reading APIs beside the JSON source?

## Decision drivers

- An OpenAPI document describes an API; it holds no entries. What is read stays the API's answers.
- One way of reading JSON over http, with its paging, limits, guard against private addresses, credential, mapping and withdrawal (ADR-0127).
- What a description suggests can be wrong or incomplete; the administrator sees and may change every setting.
- Descriptions are often written in YAML; the standard library reads only JSON.
- Only open source without licence fees; a dependency needs a reason (AGENTS.md).

## Considered options

1. The description offers its lists; the one picked fills a JSON source's settings, which stay the connector's
2. A connector kind `openapi` that reads the description at every run and derives its settings anew
3. No support: administrators read the description themselves

## Decision outcome

Chosen option: **1**, because a description is a help to set up a JSON source, not a source; derived once and shown, its suggestions can be checked and corrected, and every run reads the API the same way as any JSON source.

- **Reading a description** (`internal/connector/openapi.go`). `GET /api/connectors/{id}/openapi?url=…` reads an OpenAPI 3 or Swagger 2.0 document, in JSON or YAML, through the connector's guarded client with its credential as a bearer token, up to 16 MB. Local references (`#/components/…`, `#/definitions/…`) are followed, schemas that refer to themselves only to a fixed depth; `allOf` members are merged, of `oneOf` and `anyOf` the first counts; external references are not followed.
- **What is offered.** Every GET operation whose answer, of `200`, `2XX` or `default` in a JSON media type, is a list of objects or holds one, up to three levels deep, the usual names (`entries`, `items`, `results`, `data`, …) first; at most 200. An operation is left out, with the reason, when it needs path parameters, a required query parameter without a default, describes no JSON answer, or answers no list of objects, or when its entries have no field that identifies them. A query parameter's default is put into the address.
- **What is suggested.** The address of the list, from the first server with its variables at their defaults (Swagger 2.0: scheme, host and base path), relative to the description's own address; the path to the list; the ID (`id`, `identifier`, `uuid`, `key`, `slug`, `code`, else a name ending in id); the title (`title`, `name`, `label`, …, else the ID); the link (`url`, `href`, …, or a text of format `uri`); the text (`description`, `text`, `summary`, …); the address of the next page (a text whose path says `next`, as `_links.next.href`); and the entry's other texts, numbers, truth values and lists of them, up to two levels deep, as further values for the mapping to fields.
- **In the administration.** A JSON connector's form has a section *From an API description*: its address, *Show the lists*, and the lists offered with the operations left out. Picking one fills the form; it is saved as any JSON source. A template *OpenAPI* starts a JSON connector with that section first. The settings keep the description's address and the operation (`spec`, `operation`) so they can be taken again; a run does not read the description.
- **YAML.** `go.yaml.in/yaml/v3` (MIT and Apache-2.0), the maintained continuation of the YAML library most Go programs use, reads descriptions in YAML; keys it reads as numbers, as response codes written `200`, are taken as text.

### Consequences

- **Positive:** an API with a description is connected by picking a list, not by reading its answers.
- **Positive:** nothing new runs on a schedule: a connector set up from a description is a JSON connector, read, mapped and withdrawn as ADR-0127 decided.
- **Negative / trade-offs accepted:** when the API and its description change, the connector's settings do not follow; the administrator reads the description again and picks the list anew. A run that finds no list at the path says so.
- **Negative / trade-offs accepted:** a description's paging other than a next address (offsets, cursors in headers, page numbers) is not understood; such a list is read up to its first page, and the report says nothing is withdrawn when far fewer entries come than the space holds (ADR-0127).
- **Negative / trade-offs accepted:** a dependency more; it parses untrusted input, bounded in size and nesting, and the library refuses excessive aliasing.
- **Follow-ups / risks to watch:**
  - offsets and page numbers as a JSON source's paging;
  - a check at every run that the description still names the operation, with a warning when it changed.

## Pros and cons of the options

### Option 2: a kind that reads the description at every run

Strongest form: when the API changes, the connector follows the description without anyone acting.

- Bad: a changed description would change what is read, mapped and withdrawn without anyone seeing it; a suggestion that was wrong could not be corrected for good. Reading the API stays the same as for a JSON source, so the kind would duplicate it.

### Option 3: no support

Strongest form: nothing to build, and the JSON source can already read every such API.

- Bad: an administrator has to find paths in answers by hand that the description already names.

## Links

- amends [ADR-0127](0127-connectors-read-any-json-source-into-a-type-of-ones-own.md) (a JSON source's settings from an API description)
- relates to ADR-0062 (public addresses only), ADR-0077 (credentials from the environment)
