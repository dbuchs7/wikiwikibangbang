# ADR-0096: Links, mentions and embeds as in a word processor

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. The maintainer found Link, Mention and Embed unfriendly and asked for them to work like Microsoft Word, so that no one has to get used to something new. They chose:
  - "@" for both people and items;
  - links, "@" and embedding, without remodelling the whole toolbar.

## Context and problem statement

The editor (ADR-0032) had one field in its toolbar for "an address or an item key". The buttons Link, Mention and Embed then acted on what was typed there:
- Link needed the text selected first, and an exact URL.
- Mention and Embed needed the exact key of an item.
- A web address typed into the text stayed plain text.
- "@" in the text did nothing, while in comments it names a person (ADR-0084).

Word and its kin work otherwise: Ctrl+K opens a dialog with the text to display and the address, addresses become links as they are typed, and "@" offers people to mention.

## Decision drivers

- Users of word processors find what they know: Ctrl+K, a link dialog, links recognised by themselves, "@" with suggestions.
- The body model stays as it is (D1, D4, ADR-0001). A link is a link mark, a mention of an item is a mention, and an embed is an embed block. A person is named as `@username`, as in comments.
- No new dependency; ProseMirror stays the editor (ADR-0032).
- A person named in a text is told, as one named in a comment is (ADR-0084).

## Considered options

1. **Writing aids on the existing editor:** a link dialog, links recognised from typing and pasting, a bubble on a link, an "@" list, an embed dialog. A person mentioned in a text is told from the log.
2. **Another editor component with a Word-like interface,** e.g. a commercial one.
3. **Keep the field and explain it better.**

## Decision outcome

Chosen option: **1**.

- **Link (Ctrl+K or the Link button):** a dialog "Insert link" with "Text to display" and "Address".
  - The address is a web or e-mail address. A bare `www.` address is taken as https, a bare e-mail address as mailto.
  - Typing a title instead lists matching pages and tasks. Choosing one links it as a mention of the item, which follows the item when it moves or is renamed.
  - Within a link, the dialog edits the whole link and can remove it.
- **Links recognised:** a web address followed by a space or Enter becomes a link. A pasted address becomes a link, or links the selected text.
- **A link while editing:** with the cursor in a link, a bubble shows its address, to open it, edit it or remove it. Ctrl+click opens a link.
- **"@":** typing "@" and part of a name lists people (ADR-0078) and pages and tasks. The list is used with the arrow keys and Enter, and closed with Escape. The "@" button starts the same.
  - **A person** is written as `@username`.
  - **An item** becomes a mention showing "@" and its title.
- **A person mentioned in a text is told:**
  - The notification is read from the log, as for comments: an edit by someone else that adds `@username` to the text tells that person, once.
  - Names inside protected passages (ADR-0088) do not count.
  - The notification says "mentioned you in the text of".
- **Embed:** "Insert… → Embed an item…" opens a dialog that finds the item by typing.
- The field for an address or key and the buttons Mention and Embed go.

### Consequences

- **Positive:** links, mentions and embeds work as users know them from Word, Outlook and Teams.
- **Positive:** naming a person in a text reaches them, without watching the item.
- **Negative / trade-offs accepted:**
  - A person's name in a text is plain text `@username`, not a link to the person.
  - A mention is told when the edit that adds it is committed. While someone writes in a live session (ADR-0010), that is when their block is saved.
- **Negative / trade-offs accepted:** the bell reads the text of each edit that might name the reader. A quick check of each block for "@name" keeps this cheap. It is not measured at the size of ADR-0081.

## Pros and cons of the options

### Option 2: another editor

Strongest form: the closest copy of Word, with little of our own code.

- Bad: a large dependency, often commercial, against ADR-0032.
- Bad: its model would have to be mapped onto ours (D1, D4), and live editing (ADR-0010) and protected passages (ADR-0088) built again.

### Option 3: explain the field

Strongest form: no change in behaviour.

- Bad: the habit users bring stays unmet. Word's way is the expectation.

## Links

- amends [ADR-0032](0032-prosemirror-rich-text-editor.md) (the editor's toolbar) and [ADR-0084](0084-a-mention-in-a-comment-is-a-notification.md) (mentions in a text are told too)
- relates to ADR-0078 (finding people), ADR-0088 (protected passages), ADR-0091 (the UI's rules)
