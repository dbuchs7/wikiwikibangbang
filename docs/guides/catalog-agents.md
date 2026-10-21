# Keeping a catalog current with agents

This guide is for an AI assistant (e.g. Claude) connected to wikiwikibangbang over MCP ([ADR-0012](../adr/0012-mcp-adapter-over-http-api.md), [ADR-0068](../adr/0068-oauth-for-ai-assistants.md)) that is asked to work on a catalog of interfaces ([ADR-0074](../adr/0074-catalogs-kept-in-step-with-other-systems.md), [ADR-0075](../adr/0075-mirroring-the-interfaces-of-opendata-swiss.md), [ADR-0077](../adr/0077-connectors-keep-catalogs-in-step-with-their-sources.md)). It can be given to the assistant as instructions.

## What the catalog is

- A space, e.g. `API`, holding items of the type `interface`, one per data set with interfaces ([ADR-0080](../adr/0080-one-page-per-data-set-written-from-the-source.md)); its `source-id` is the data set's ID.
- Entries from a source such as opendata.swiss are mirrored by a connector of the server; their origin names the connector and links to the data set on the portal, the system of record.
- Mirrored fields are overwritten by every run: `source-id`, `format`, `endpoint`, `publisher`, `dataset`, `modified`, the title and the page's text, which is written from everything the source says. Do not change them; change them at the source, and put notes in comments, which stay.
- Fields that belong to the catalog: `owner`, `reviewed`, the body. `contact` and `specification` are kept when the source names none, so they are the place to add what is missing.

## Rules for an agent

1. **Find before you create.** Look an entry up by its identifier with `find_items` and `where`, e.g. `space=API`, `type=interface`, `where=source-id=<id>`. Create an entry only for an interface that has no source the synchronisation reads, and give it a `source-id` of its own: the field is unique, and a second item with the same value is refused.
2. **Work on the findings.** `get_governance` with `space=API` lists entries with empty recommended fields (`contact`, `specification`, `owner`), broken rules and entries due for review. Fill a missing `contact` or `specification` with `set_property` when you found it in a reliable place, and say where in a comment (`add_comment`).
3. **Do not decide for people.** Leave `owner` and `reviewed` to people: the owner answers for the entry, and a review is a person's check. Propose an owner in a comment instead.
4. **Check against guidelines.** When asked to check an entry against API guidelines (e.g. the Swiss API guidelines), read its `specification`, and record the outcome as a comment that names each rule and whether it is met. A finding is an assessment, not proof; say so.
5. **Keep a trace.** Everything you change is in the item's history under your credential. Prefer comments over silent edits when you are unsure.

## Useful calls

| Purpose | Tool and arguments |
|---|---|
| The fields of the type | `list_types` |
| An entry by its identifier | `find_items` `space=API` `type=interface` `where=source-id=…` |
| Withdrawn entries | `find_items` `space=API` `type=interface` `state=withdrawn` |
| Entries of a publisher | `find_items` `space=API` `where=publisher=…` |
| The findings | `get_governance` `space=API` |
| Add a contact | `set_property` `ref=API-12` `name=contact` `value="Name <mail@…>"` |
| Explain a change | `add_comment` `ref=API-12` |
