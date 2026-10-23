# ADR-0106: An embedded item is shown whole

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that embedding a post or page shows its whole content, not only a link, 2026-10-23)

## Context and problem statement

An embed block shows another item in a text (ADR-0020). In the editor and while reading, it showed only the item's key and title as a link, so it looked no different from a mention. Readers had to open the item to see what the author meant to show them.

How does an embed show the item itself, without widening who may see it?

## Decision drivers

- An embed never grants access (ADR-0020, ADR-0022): the reader sees what they could open themselves, and protected passages stay veiled (ADR-0088).
- The body model stays as it is (D1, D4): an embed names its target, and its content is never copied into the host.
- An embed that shows itself, directly or through others, must not loop.

## Considered options

1. **The page shows the target's content in the embed block, loaded when shown, read-only, with the reader's rights.**
2. **Copy the target's content into the host's body when embedding.**
3. **The server renders the host with its embeds resolved.**

## Decision outcome

Chosen option: **1**.

- **What the block shows:**
  - a heading with the target's key and title, which leads to it;
  - its kind, presence and state, and the fields it has values for;
  - its text, read-only, with its pictures and videos.
- **In every view:** the content shows while reading, in the item's page and in the kanban detail, and in the editor while editing the host.
- **Who sees what:** the page loads the target through the API as the reader, so a reader who may not read the target sees the block as before, "an item you cannot see". Protected passages are veiled as on the target's own page.
- **Nesting:**
  - Embeds inside the target show their content too, up to three levels; deeper ones show their heading only.
  - An item that is already shown around the place where it is embedded (A embeds B embeds A) shows a note instead of itself.
- **Always current:** the content is the target's current state when the page is shown. Changing the target changes every place it is embedded.
- **Editing:** the content can be read but not edited in place; editing happens in the target itself.
- **Export:** the Word export already writes embedded items with their content (ADR-0071).

### Consequences

- **Positive:** a page can gather other pages and tasks and be read as one document.
- **Negative / trade-offs accepted:** each embed costs two requests when shown; a page with many embeds loads more.
- **Negative / trade-offs accepted:** comments and protected passages act on the target's own page, not inside the embed.

## Pros and cons of the options

### Option 2: copy the content

Strongest form: the host reads as one text, even offline or in a backup alone.

- Bad: copies drift from their source, and a copy shows the text to everyone who may read the host, which widens access (ADR-0020).

### Option 3: render on the server

Strongest form: one request per page.

- Bad: the body is drawn in the browser (ADR-0032). A second renderer on the server would have to match it, and a permission check per embed is needed either way.

## Links

- amends [ADR-0020](0020-embedding-and-containment-are-independent.md) (an embed shows its target whole)
- relates to ADR-0022 (permissions), ADR-0032 (editor), ADR-0071 (export), ADR-0088 (protected passages), ADR-0096 (embedding as in a word processor)
