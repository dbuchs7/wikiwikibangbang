# ADR-0063: A comment overview across items

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for a Comments section in the main menu showing comments on watched pages, one's own comments and comments that mention one, 2026-10-20)

## Context and problem statement

Comments (ADR-0035, ADR-0047) are read on their item. Someone who follows discussions across many pages has to open each one, and has no way to see where they were asked something. The notifications (ADR-0046) say that a comment was added, not what it says. How does one see the comments that concern one, in one place?

## Decision drivers

- Nothing new to store or keep in step: comments already live on their items (D1).
- Only what one may read (D10).
- "Mentioned" must work with comments as they are: plain text.

## Considered options

1. **An overview read from the items: comments on watched items or spaces, one's own, and those containing "@username".**
2. **Structured user mentions in comments**, stored with the comment like item mentions in bodies.
3. **Extend the notifications** to carry comment texts.

## Decision outcome

Chosen option: **1**.

- `GET /api/comments` lists the comments that are not deleted on live items the caller may read, newest first. Each entry says why it is listed (`watched`, `mine`, `mentions`, possibly several); `only` narrows to one reason; paging is by offset.
- **Watched** means the item or its space is watched (ADR-0046).
- **Mentioned** means the text contains `@username`, ignoring case, not followed by a further letter, digit, `_` or `-`; `@bert` does not mention `berta`.
- The UI lists the overview under **Comments** in the module drawer, with tabs All, Watched, Mine and Mentioning me; each entry links to its item.

### Consequences

- **Positive:** no codec change; the overview cannot disagree with the items.
- **Negative / trade-offs accepted:** a renamed user is not mentioned by comments written with the old name, and a text that only looks like a mention counts.
- **Negative / trade-offs accepted:** the overview scans the readable items on each request; this is linear in the number of items, like search (ADR-0033).

## Pros and cons of the options

### Option 2: structured mentions

Strongest form: exact, survives renames, and could notify the person mentioned.

- Bad: a change of the comment format, a codec version, and an editor for comments; the request did not need it to start with.

### Option 3: comments in the notifications

Strongest form: one place for everything.

- Bad: notifications are changes by others; one's own comments and mentions on unwatched items would still be missing.

## Links

- relates to ADR-0035 (comments), ADR-0046 (watching and notifications), ADR-0047 (inline comments)
