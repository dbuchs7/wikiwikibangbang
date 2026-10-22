# Keeping a catalog current with agents

This guide is for an AI assistant (e.g. Claude) connected to wikiwikibangbang over MCP ([ADR-0012](../adr/0012-mcp-adapter-over-http-api.md), [ADR-0068](../adr/0068-oauth-for-ai-assistants.md)). It covers work on a catalog of interfaces ([ADR-0077](../adr/0077-connectors-keep-catalogs-in-step-with-their-sources.md), [ADR-0080](../adr/0080-one-page-per-data-set-written-from-the-source.md), [ADR-0093](../adr/0093-the-interface-catalog-is-ordinary-pages.md)), and can be given to the assistant as instructions.

## What the catalog is

- A space, for example `API`, holds ordinary wiki pages of the type `page`. Each page stands for one data set with interfaces.
- A page has no fields and no state. Everything the source says of the data set is in the page's text: publisher, contact, interfaces, formats, addresses, licence and more.
- A connector on the server mirrors the pages from a source such as opendata.swiss. A page's origin names the connector and links to the data set on the portal, which is the system of record.
- Every run writes a page's text anew. Do not edit the text; change the data at the source instead. Put notes in comments, which stay.
- A page whose data set the source no longer publishes carries the label `withdrawn` and a notice at the top of its text. It stays, with its comments.

## Rules for an agent

1. **Find before you create.**
   - Look a data set up with `search_items` by its title or name. Its page's origin holds the data set's ID as `page/<id>`.
   - Create a page only for an interface that no source of the synchronisation covers. Such a page is yours to write; the connector does not touch it.
2. **Add what is missing as comments.** When a page lacks a contact or a specification and you found one in a reliable place, add it with `add_comment` and say where you found it.
3. **Do not decide for people.** Who answers for a data set is for people to decide. Propose someone in a comment.
4. **Check against guidelines.** When asked to check a data set against API guidelines (for example the Swiss API guidelines), read the specification linked in its page. Record the outcome as a comment that names each rule and whether it is met. A finding is an assessment, not proof; say so.
5. **Keep a trace.** Everything you change is in the item's history under your credential. Prefer comments over silent edits when you are unsure.

## Useful calls

| Purpose | Tool and arguments |
|---|---|
| A data set by its title or name | `search_items` `q=…` `space=API` |
| The pages of the catalog | `find_items` `space=API` `type=page` |
| Withdrawn data sets | `find_items` `space=API` `label=withdrawn` |
| A page and its text | `get_item` `ref=API-12` |
| Add a note | `add_comment` `ref=API-12` |
