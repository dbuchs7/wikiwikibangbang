# ADR-0047: Inline comments are anchored by the text they quote; threads take replies and are resolved

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for inline comments, 2026-10-19)

## Context and problem statement

Comments (ADR-0035) are on a whole item. Reviewing a page needs more: a remark on one sentence, shown next to it, answered by others and closed when it is dealt with. A wiki offers this as *inline comments*: select text, comment on it, see the passage highlighted, reply, resolve.

The hard part is the anchor. The body changes while the comment stands, through live editing by several people at once (ADR-0010), restoring an earlier version (ADR-0043) or an import. The comment must keep finding its passage, and say so when the passage is gone.

## Decision drivers

- Commenting must not change the body: a remark is not an edit, needs only the right to comment, and must work while others hold the blocks in a live session.
- The anchor must survive edits around the passage and tell apart repeated words.
- The history of the body must stay the history of what was written.
- An anchor is content: it quotes the body, so it is sealed and erased with the item (ADR-0026) and checked for sensitive data (ADR-0040).

## Considered options

1. **A quote with context** stored on the comment: the selected text, and up to 32 characters before and after it. The browser finds it again in the body when it shows the page.
2. **A mark in the body**, a comment mark over the range (like bold), carried through edits by the editor.
3. **A position**: block number and character offsets.

## Decision outcome

Chosen option: **1**.

Option 2 is the strongest alternative: the editor moves a mark through every edit exactly, so an anchor never drifts, and a passage rewritten word by word stays commented. It loses on the drivers: adding a comment would commit the body, which a live session refuses while someone else holds the block (ADR-0010), every comment would become a body version (ADR-0043), and commenting would need the right to edit the body rather than to comment. Option 3 breaks on the first edit above the passage.

The quote of option 1 is the selector annotation systems use for documents they do not own. Its weakness is that a passage whose words all change is lost; the comment then says that its text is no longer in the page and still shows the quote, which is what a reader needs to understand it.

- **Model:** `Comment` gains `Anchor` (quote, prefix, suffix), `Reply` (the first comment of the thread it answers; threads are one level deep, an answer to a reply joins its thread) and `Resolved`, `ResolvedBy`, `ResolvedAt`. Only the first comment of a thread is resolved or reopened.
- **Events:** `CommentAdded` may carry `anchor` (sealed, erased with the item) and `reply`; the kinds `CommentResolved` and `CommentReopened` (codec version 10). They show in the item's history like the other comment events, without their text.
- **Validation:** the quote must not be blank; the item's type must have a body; a reply carries no anchor and answers a comment that is not deleted. The server does not require the quote to be in the stored body: text typed a moment ago in a live session may not be saved yet.
- **Rights:** as for comments (ADR-0035): commenting, replying, resolving and reopening need the right to edit the item. A deleted first comment drops its anchor, and its thread takes no more answers.
- **Governance:** the quote and its context count as content of the item for the detection of sensitive data (ADR-0040).
- **API:** `POST /api/items/{ref}/comments` takes `anchor` and `reply`; `PUT`/`DELETE /api/items/{ref}/comments/{id}/resolved`. The MCP adapter (ADR-0012) offers anchor and reply on `add_comment`, not resolving: closing a discussion is a person's decision.
- **UI:** a *Comment* button in the editor's toolbar comments on the selected text of one block; open threads highlight their passages, a click on a highlight shows the thread, a click on a thread's quote shows the passage. Threads list their replies; resolved ones fold away under *Resolved*.

### Consequences

- Good: commenting never conflicts with live editing and never creates a body version.
- Good: an anchor that no longer matches is visible as such, rather than pointing at the wrong words.
- Bad: a passage rewritten completely loses its highlight; the thread stays, with the quote.
- Bad: highlighting is computed in the browser for every change of the document. For a long page with many threads this is a search per thread per change; it is linear in the body's size and has not been measured on large pages.
- Neutral: replies notify watchers like any comment (ADR-0046); resolving does not.
