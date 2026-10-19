# ADR-0035: Comments are part of their item: events on the item, sealed with its key

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer chose comments as the next feature, 2026-10-19)
- **Open question:** may viewers comment, or only those who may edit the item? This record starts with editors; testers' feedback decides.
- **Question checked:** 2026-10

## Context and problem statement

Confluence pages and Jira issues both carry comments; ADR-0001 lists them among the universal capabilities that exist once, for every item, and D6 forbids type-specific code for them. Nothing in the product holds a discussion yet. The questions are what a comment is in the model, who may write one, and what happens to it when its item is purged or redacted — content must then be unrecoverable (ADR-0026).

## Decision drivers

- One model for content (D1): no parallel domain object that search, permissions, history and erasure would have to learn separately.
- A comment is readable exactly when its item is (ADR-0022, D10); there is no second permission check to get wrong.
- Purge and redaction erase comments with the item, by the same key (ADR-0026).
- Old data directories stay readable (ADR-0025).

## Considered options

1. **Comments are part of their item**: events on the item, held in the item's state
2. **A comment is an item** of a built-in type, contained by the item it is about
3. **A comment store of its own**, beside the items

## Decision outcome

Chosen option: **1**.

- **Model:** `Item.Comments` lists the item's comments in the order written: an ID, the author (a principal), when it was written and last edited, the text, and whether it was deleted. Three event kinds change them: `CommentAdded`, `CommentEdited` and `CommentDeleted`, each on the item, with the comment's ID frozen into the event. Comments do not count as a change of the item itself: they leave its `Version` alone, so they never conflict with an edit of its body.
- **Text:** plain text, rendered as text (ADR-0014). No marks, mentions or embeds: a mention would derive a relation, and derived relations are a function of the body alone (D4). Empty text is refused; the request-body budget bounds its size (ADR-0015).
- **Rights:** reading the item is reading its comments. Writing a comment needs the right to edit the item, including the restriction's edit list; synchronised items (ADR-0023) take no comments until cut-over, like every other change. The author edits and deletes their comment while they may still edit the item; a space administrator may delete any comment. A trashed item takes no comments.
- **Erasure:** comment text is content: `CommentAdded` and `CommentEdited` are sealed with the item's key and erased by purge and redaction like a title or a body (ADR-0026); a purged item keeps no comments. Deleting a comment hides its text from every reader, and the history shows that a comment changed but never its text. The text stays in the encrypted log until the item is purged or redacted: the key is per item, not per comment.
- **Format:** `CodecVersion` 4 adds the event kinds and the fields `comment` and `text`; the text is part of the sealed content. Versions 1 to 3 are still read; a version 3 build refuses a version 4 log rather than ignore the comments.
- **API:** `GET /api/items/{ref}/comments` (paged, ADR-0015), `POST` to add, `PUT /api/items/{ref}/comments/{id}` to edit, `DELETE` to delete. A deleted comment is listed in its place without text. The MCP adapter (ADR-0012) offers `list_comments` and `add_comment`; editing and deleting are not offered to agents.
- **UI:** a comment section on every item: the list, a field to add one, and edit and delete for one's own.

**Implementation is Partial:** comments show the author's user ID, as the history does, because non-administrators have no way to look up names yet (ADR-0031); search does not find comment text (ADR-0033); no replies, no notifications.

### Consequences

- **Positive:** comments come with permissions, history, the codec round trip and erasure that items already have, for every type at once; no new store, no new key handling.
- **Negative / trade-offs accepted:** a deleted comment's text can be recovered by whoever holds the data directory and its keys, until the item is erased; an item with very many comments holds them all in memory, like its body; the codec version rises, so a data directory written by this build cannot be opened by an older one.
- **Follow-ups / risks to watch:** search over comment text; display names instead of IDs; replies and notifications; the open question above.

## Pros and cons of the options

### Comments are part of their item

- Good: everything that holds for an item holds for its comments without new code paths; erasure is complete by construction.
- Bad: a single comment cannot be crypto-shredded on its own; comments cannot be moved between items.

### A comment is an item

Strongest form: D1 taken literally — every piece of user content is an item, so comments get keys, history, restrictions, relations and search with no new concept at all, and a comment could even be embedded or linked.

- Good: maximal reuse; each comment has its own key, so deleting one could shred it.
- Bad: comments would appear in the containment tree, in listings, in key numbering (`PLAT-12` for a comment) and in every count, so each of those needs an exception for one type — exactly what D6 forbids; a comment's own restriction could make it unreadable while its item is readable, which no user expects.

### A comment store of its own

Strongest form: comments are a different shape of data (append-heavy, small, many per item), and a separate store could page and index them independently of the item state.

- Good: memory per item stays small; independent evolution.
- Bad: a second permission check, a second erasure path and a second key handling, each of which can drift from the item's — the duplication ADR-0001 was written to avoid.

## Links

- refines [ADR-0001](0001-unified-item-data-model.md) (universal capabilities); uses [ADR-0008](0008-event-sourced-persistence.md), [ADR-0014](0014-closed-renderer-for-user-content.md), [ADR-0015](0015-resource-budgets-and-paged-listings.md), [ADR-0022](0022-permission-model.md), [ADR-0025](0025-event-log-file-and-in-memory-state.md), [ADR-0026](0026-crypto-shredding-of-item-content.md), [ADR-0012](0012-mcp-adapter-over-http-api.md)
