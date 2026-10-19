# ADR-0040: Content governance: sensitive data restricts its item on save; staleness and plausibility are checked on demand

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer chose the checks, automatic restriction, the last editor as responsible, and checking sensitive data on save, 2026-10-19)

## Context and problem statement

People paste passwords, keys and account numbers into pages and tasks, and articles go out of date or contradict themselves without anyone noticing. A wiki that many people read spreads a pasted secret to all of them. The maintainer asked for a governance check that finds sensitive data, outdated and implausible content, names who is to act, and protects an item that holds sensitive data.

## Decision drivers

- A secret must not be readable by others for even a moment after it is saved.
- Protection must hold on every path that writes content: the API, live editing, AI agents, import.
- A false alarm must be correctable, and must not lock the item again on every save.
- The check itself must not become the leak: findings are shown masked, and only to readers of the item (D10).
- Type-agnostic (D6): the same checks for every item.

## Considered options

For sensitive data:

1. **Restrict automatically, in the same batch as the content**
2. **Propose a restriction; an administrator confirms it**
3. **Report only**

For staleness and plausibility: a report computed on demand, a stored report, or a daily job.

## Decision outcome

Chosen: **1** for sensitive data, and **a report computed on demand** for the rest.

- **Detection** (`internal/governance`): passwords after a label (`password`, `Passwort`, `pwd`, `passcode`, `Kennwort`) whose value has a digit or symbol or is long, and is not masked already; private key headers; access tokens of common services and of this server; signed tokens (JWT); IBANs with valid check digits; card numbers with a valid Luhn digit. A finding carries its position and a masked form; the secret is never stored or shown.
- **On save:** every command passes through one place (`recordBatch`). When an event brings content (title, properties, body, comments, import) holding sensitive data that the item did not hold before, the same batch gets a `RestrictionSet` by the actor `governance`: reading is limited to the person who saved, and the edit list stays as it was. Space administrators still read the item (ADR-0022). Data the item already held, e.g. after an administrator lifted the restriction, does not restrict it again. Live sessions check access again on every tick, so participants who lost the right leave.
- **Report** (`GET /api/spaces/{space}/governance`): computed when asked, like search (ADR-0033). It lists:
  - sensitive data with location, kind, masked form, responsible person and whether the item is restricted;
  - items unchanged for `staleDays` (365 by default);
  - implausible items: a date field named like a deadline (due, deadline, fällig) that has passed while the item is not in its workflow's last state; a required field of the type's latest version that is empty; an article without workflow whose body is empty.
- **Who is shown what:** only items the reader may read now; anyone with a role in the space reads the report.
- **Responsible:** whoever changed the item's content last.
- **UI:** "Governance" on the space page; an administrator lifts a restriction there after checking. A restricted item says why at its top.

### Consequences

- **Positive:** a pasted secret is out of others' reach from the moment it is saved, on every write path; findings come with a name; nothing is stored, so erasure (ADR-0026) needs nothing new.
- **Negative / trade-offs accepted:**
  - A restriction narrows the item **and everything below it** (D5, D9): a secret in a parent page hides its children from others too.
  - Editors other than the author lose access until an administrator lifts the restriction.
  - Participants of a live session see what is typed before it is saved.
  - Detection is a heuristic: it misses secrets in unusual form and flags some harmless text.
  - The restriction appears in the history as a change by `governance`.
- **Follow-ups / risks to watch:** notifying the responsible person; patterns per installation; a daily report; a restriction that narrows only the item, not its subtree.

## Pros and cons of the options

### Restrict automatically

- Good: no window in which a secret is readable; works without anyone watching.
- Bad: a false alarm locks readers out until an administrator acts; the subtree is restricted too.

### Propose, administrator confirms

Strongest form: a person decides before access changes, so a false alarm never locks anyone out, and the system never changes rights on its own.

- Good: no surprise changes of access.
- Bad: the secret stays readable to everyone until someone confirms, which may take days.

### Report only

- Good: simplest.
- Bad: protects nothing by itself.

### Stored report or daily job for staleness and plausibility

Strongest form: findings are there when someone looks, and can be tracked over time.

- Good: history of findings; no waiting for a scan.
- Bad: a second copy of content-derived data to keep consistent and erase; the scan of the live state is fast enough today.

## Links

- uses [ADR-0001](0001-unified-item-data-model.md) (D5, D6), [ADR-0010](0010-real-time-collaboration-with-block-locks.md), [ADR-0022](0022-permission-model.md) (D9, D10), [ADR-0026](0026-crypto-shredding-of-item-content.md), [ADR-0033](0033-full-text-search-by-scanning-the-live-state.md), [ADR-0035](0035-comments-are-part-of-their-item.md), [ADR-0039](0039-link-check-on-demand-public-addresses-only.md) (the same notion of responsible)
