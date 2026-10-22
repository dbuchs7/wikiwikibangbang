# ADR-0094: A type nothing uses can be removed

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators (the maintainer asked to remove the type "interface" once the catalog became ordinary pages, ADR-0093, 2026-10-22)

## Context and problem statement

Types are defined and versioned (ADR-0001); there was no way to take one out of use. After ADR-0093 the catalog's former type `interface` has no purpose left, yet it stays in the list of types, in the choice of types when making an item, and in the administration. How does a type go without breaking the items and history that refer to it?

## Decision drivers

- An item names its type and the version it was made with (ADR-0001); replaying the log must keep working for every item, also purged ones (D7).
- `applyToState` stays deterministic: removing is an event like every other change.
- Nothing visible may lose its type: no live or trashed item, no kanban.
- A name once used may be wanted again.

## Considered options

1. **An event TypeRemoved that takes a type out of use; its versions stay.** Refused while a live or trashed item or a kanban uses the type.
2. **Erase the type's definitions.**
3. **Hide types without items in the UI only.**

## Decision outcome

Chosen option: **1**.

- **Model:** the event kind `TypeRemoved` carries the type's name (codec version 24). A removed type is not listed, is not the latest of its name, and no item can be made of it. Its versions stay readable, so purged items and the history keep their type.
- **Guard:** removal is refused with "still referenced" (HTTP 409) while a live or trashed item of the type exists or a kanban shows it; those go first. Purged items do not hold a type.
- **Defining again:** defining a removed name starts its next version; the earlier versions stay part of its history.
- **Access:** administrators, as for defining types: `DELETE /api/types/{name}` and "Remove" beside each type in the administration, with a confirmation.
- **The catalog:** the connector removes the type `interface` in the run that erases its last entries of that type (ADR-0093), unless other items or a kanban still use it.

### Consequences

- **Positive:** types no longer used disappear from every choice; the log stays complete.
- **Positive:** the removal is undone by defining the type again.
- **Negative / trade-offs accepted:** a type used in many places must first be emptied by hand; the removal does not delete items.
- **Negative / trade-offs accepted:** an older build cannot read a log that contains a removal (codec version 24).

## Pros and cons of the options

### Option 2: erase the definitions

Strongest form: nothing of the type is left.

- Bad: purged items and the history name the type and its version; replay would no longer find them (D7).

### Option 3: hide in the UI only

Strongest form: no new event, no codec change.

- Bad: the API, MCP and imports would still offer the type, and "unused" would be decided anew in every view.

## Links

- amends [ADR-0001](0001-unified-item-data-model.md) (types are defined and versioned) and [ADR-0093](0093-the-interface-catalog-is-ordinary-pages.md) (the catalog's former type)
- relates to ADR-0021 (deletion), ADR-0025 (codec versions), ADR-0028 (item API), ADR-0060 (kanbans)
