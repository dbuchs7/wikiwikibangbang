# ADR-0078: Protecting an item for people and groups from its page

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked that pages, decisions, tasks and interfaces can be protected from the right side of their page, either for a group or for one or more people, 2026-10-21)

## Context and problem statement

ADR-0022 lets an item's restriction narrow who may read and who may edit it, below the space's roles, inherited by its children. A restriction lists principals: user IDs and groups (`@name`). The item page only offered "only I" for reading and editing, "only I edit" and lifting. Adding someone else was possible only for space administrators, only for people, and only to a restriction that already existed; groups could not be chosen at all. How does whoever may edit an item protect it for a group, or for several people, without breaking the rules of ADR-0022?

## Decision drivers

- ADR-0022 holds: a restriction only narrows, applies the same way to every type (D6) and is inherited by children.
- No one locks themselves out by accident; a space administrator always keeps access.
- Choosing a group must not reveal more than needed: the group's name, not its members.
- A typing error must not close an item to everyone.

## Considered options

1. **A protection editor on the item's access card: people and groups chosen by typing, each reading only or editing too, for reading and editing or for editing only.**
2. **Keep the card; protect through the API or the administration only.**
3. **Protection levels as named presets** (e.g. "confidential", "internal") defined by administrators.

## Decision outcome

Chosen option: **1**.

- **Two modes.** *Reading and editing* lists who may read; each of them is marked as editing too or reading only. *Editing only* leaves reading open to the space and lists who may edit.
- **People and groups.** Both are found by typing, in one list. Groups are searched with `GET /api/groups/search?q=`, open to everyone who may edit in a space, like the search for people. It answers group names only, never members.
- **No lock-out.** One who is not a space administrator stays on both lists when saving. An empty list of editors under a restricted read list would let every reader edit, so the saving person is put on it.
- **Only what exists.** `PUT /api/items/{id}/restriction` refuses a user ID that is no account and a group without members.
- **Unchanged.** The quick actions ("only I", "only I edit", lift) stay. The card says that protection also holds for the item's children. A person on a list still needs a role in the space; protection never grants access (ADR-0022).

### Consequences

- **Positive:** an editor can protect a page, decision, task or interface for a team in a few steps, without asking an administrator.
- **Positive:** groups make protections last: a new member of the group gets access without touching each item.
- **Negative / trade-offs accepted:** everyone who may edit in some space learns the names of all groups. The names are usually not sensitive; members stay hidden.
- **Negative / trade-offs accepted:** a group that loses its last member no longer counts as existing for a new restriction. Items already restricted to it stay closed to all but space administrators until the group has members again.

## Pros and cons of the options

### Option 2: protection only through the API or the administration

Strongest form: protection is rare and consequential, so it belongs with administrators, who know the groups and are accountable.

- Bad: the people who know which content is sensitive are those who write it. Routing every protection through an administrator delays it, and sensitive content stays open meanwhile.

### Option 3: named protection levels

Strongest form: an organisation's classification ("internal", "confidential") is applied consistently; editors choose a level, not people.

- Bad: it needs a second model on top of ADR-0022 and a mapping of levels to groups per space. It may follow later as presets that fill this editor.

## Links

- builds on [ADR-0022](0022-permission-model.md) (roles and narrowing restrictions)
- relates to ADR-0040 (automatic restriction of sensitive content), ADR-0051 (the access card)
