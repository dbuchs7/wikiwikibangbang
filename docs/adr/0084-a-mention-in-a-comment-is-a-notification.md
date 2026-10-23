# ADR-0084: A mention in a comment is a notification

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for mentions in comments, shown to the person mentioned under Notifications, 2026-10-21)

## Context and problem statement

A comment can name a person as `@username`. The comment feed already lists such comments under "Mentions" (ADR-0063). Nothing told the person named: the notifications (ADR-0046) list only what others change on what one watches. Someone asked for their opinion in a comment heard of it only if they watched the item, or looked in the comment feed by chance.

Writing a mention was also unaided: the writer had to know the exact username.

How does a mention reach the person it names?

## Decision drivers

- A person named in a comment hears of it where they hear of everything else: the bell and Notifications, and the digest sent to their channels (ADR-0064).
- Notifications are not stored; they are read from the log (ADR-0046). A mention must not change that.
- No one hears of an item they may not read; a comment deleted or erased is no longer told (ADR-0026).
- `applyToState` stays as it is; this is a reading of the log.

## Considered options

1. **Read mentions from the log, as the other notifications are.**
2. **Record a mention as an event of its own when the comment is added.**
3. **Make a mention a watch of the item.**

## Decision outcome

Chosen option: **1**.

- **What counts:** a comment added by someone else whose text names the reader as `@username`, compared as the comment feed compares it (case ignored; a further letter, digit, `_` or `-` makes it another name).
- **How it is read:** walking the log for the bell, a `CommentAdded` event is a mention when the comment, as it is now, names the reader. It is told whether or not the reader watches the item, and only if they may read the item.
- **What it shows:** the notification carries `mentioned: true`. The UI says "mentioned you in a comment on". A mention on a watched item is told once, as a mention.
- **Writing a mention:** typing `@` and part of a name in a comment, a reply or an inline comment lists matching people; choosing one completes the name. The list uses the people search of ADR-0078, so it appears for those who may edit in a space. Others type the name in full, and it works the same.

### Consequences

- **Positive:** a person asked in a comment is told, on the bell and in the digest, without watching anything.
- **Negative / trade-offs accepted:** the comment is read as it is now. A mention added by editing the comment later is shown at the time of the comment, so it may count as already seen. A comment deleted after it was read drops out of the list.
- **Negative / trade-offs accepted:** the bell's walk reads a comment for each `CommentAdded` event in the log. It is bounded by the item's comments and was not noticeable in the tests; it is not measured at the size of ADR-0081.
- **Follow-ups:** telling a mention added by an edit; marking `@username` in a shown comment.

## Pros and cons of the options

### Option 2: an event of its own

Strongest form: a stored mention is exact about when it happened, and an edit could record one too.

- Bad: it stores who was named at the time, which would outlive a deleted or erased comment unless every erasure also removed it; it doubles what the log already says.

### Option 3: a watch

Strongest form: no new kind of notification; the person then hears of everything on the item.

- Bad: it changes what someone watches without their asking and floods them with every later change.

## Links

- amended by [ADR-0096](0096-links-mentions-and-embeds-as-in-a-word-processor.md) (a mention in a text is told too)
- relates to ADR-0046 (watching and notifications), ADR-0063 (comment feed), ADR-0064 (digest to channels), ADR-0078 (people search), ADR-0026 (erasure)
