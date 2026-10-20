# ADR-0057: Governance findings reach the bell: restrictions as notifications, open findings as a reminder

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that governance findings be told through the bell, 2026-10-20)

## Context and problem statement

Governance (ADR-0040) restricts an item on save when it brings in sensitive data, and reports stale and implausible items when someone opens a space's check. Nobody is told: the one who pasted a password does not learn that others lost access, the administrator who may lift the restriction does not learn of it, and findings wait until someone looks. How do findings reach the people who are to act, without a scheduler and without new stored state?

## Considered options

1. **Restrictions as notifications from the log; other findings as a computed reminder.**
2. **Every finding as a notification event**, written when it arises.
3. **A daily e-mail digest.**

## Decision outcome

Chosen option: **1**.

- **Restrictions:** the `RestrictionSet` that governance writes on save is told, without watching anything, to the person the item is now restricted to (the one who saved) and to the administrators of the item's space, who may lift it. It counts as unread like any notification and is marked as governance's.
- **Open findings:** the sensitive, stale (365 days) and implausible findings of every space one holds a role in, for which one is responsible (the last to change the content, ADR-0040), are computed when the notifications are read. The bell shows them as a dot and names their number; the notifications page lists them first, until they are resolved.
- The number on the bell still counts unread changes only, so a long-standing finding does not keep it at "99+".

### Consequences

- **Positive:** nothing new is stored and no job runs; findings disappear when resolved.
- **Positive:** what one hears is limited to what one may read now.
- **Negative / trade-offs accepted:** the reminder cannot be dismissed while the finding stands; a page that is stable on purpose keeps reminding as stale.
- **Negative / trade-offs accepted:** the findings are computed on every load of the bell, a scan of the in-memory state per space, like search (ADR-0033).
- **Follow-ups / risks to watch:** acknowledging a finding; a staleness period per space; e-mail.

## Pros and cons of the options

### Option 2: findings as events

Strongest form: findings would be read and unread like any change.

- Bad: stale and implausible arise from the passing of time, so something would have to run on a schedule and write them; they would stay in the log after being resolved.

### Option 3: an e-mail digest

Strongest form: reaches people who do not open the wiki.

- Bad: needs mail delivery, which the product does not have yet.

## Links

- relates to ADR-0033 (search), ADR-0040 (governance), ADR-0046 (notifications)
