# ADR-0086: A space is renamed; its key stays

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators (the maintainer asked that a space's display name be changeable on its settings page, 2026-10-22)

## Context and problem statement

A space has a key and a name, both set when it is created. Neither could change afterwards. A name typed in a hurry, such as "Factsheet's", stayed for good.

The key is part of every item's key (`FACTSHEET-12`). Those keys are in links, mentions, exports, comments and in other systems that point at items. The name is only shown.

What may change, and how?

## Decision drivers

- A space's name can be corrected by those who administer it.
- No item's key, no link and no reference changes because a space is renamed.
- The change is part of the log, so it survives restarts, backups and replays (ADR-0025).

## Considered options

1. **The display name changes; the key stays.**
2. **The name and the key change, with the old key kept as an alias.**
3. **Neither changes; a wrong name means a new space.**

## Decision outcome

Chosen option: **1**.

- **Event:** a new event kind, `SpaceRenamed`, carries the new name (codec version 20). Versions 1 to 19 are still read.
- **Who:** a space's administrators rename it, and system administrators, who administer every space.
  - Others with a role in the space are refused.
  - To those without a role, the space does not exist.
- **Rules:** the name is trimmed, single-line, 1 to 80 characters. Renaming to the same name records nothing. A space in the trash cannot be renamed.
- **Interface:** `PUT /api/spaces/{space}/name`. The space's settings page has a "Display name" card; its help text names the key that stays.

### Consequences

- **Positive:** a name can be corrected without touching any item, link or export.
- **Negative / trade-offs accepted:** the key cannot be corrected. A key that no longer fits stays visible in every item's key.
- **Negative / trade-offs accepted:** the codec version rises. A data directory written by this version is not read by earlier releases, as with every earlier rise.
- **Follow-ups:** changing a key, if it is ever needed, with option 2.

## Pros and cons of the options

### Option 2: key with alias

Strongest form: the key is what people see most; with the old key kept as an alias, links keep working.

- Bad: every lookup by key, every export, every external reference and every item's history would have to know two keys. The aliases would grow with every rename, and the key could then never be given to another space. Far more than a wrong name calls for.

### Option 3: no change

Strongest form: nothing to build; a space's identity stays fixed.

- Bad: moving every item to a new space to fix a typo changes all their keys, which is exactly what renaming must not do.

## Links

- amends [ADR-0069](0069-deleting-a-space-in-two-steps.md) (the space kinds of the log)
- relates to ADR-0022 (space roles), ADR-0025 (the event log and its codec), ADR-0062 (space icons)
