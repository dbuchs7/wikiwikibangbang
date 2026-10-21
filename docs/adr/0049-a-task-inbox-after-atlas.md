# ADR-0049: A task inbox built on the item model, after atlas's task list; no atlas code taken over

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked whether atlas's tasks could serve here as a module, and if so to build it, 2026-10-19)
- **Open question:** should wikiwikibangbang also show the user tasks of an atlas server, through its API? That needs an atlas address, a credential per user (atlas's OAuth access tokens; its API tokens act as a machine) and a mapping of accounts between the two; this record builds the inbox both would share.
- **Question checked:** 2026-10

## Context and problem statement

[atlas](https://github.com/pblumer/atlas) has a task list: an inbox of the user tasks its BPMN processes wait on, with claim and unclaim, assignee and candidate groups, due dates, priorities, saved filter folders and forms that complete a task. wikiwikibangbang's *Tasks* module listed open items as a flat table. The question was whether atlas's tasks can be used here as a module.

## What atlas's tasks are

- A task is a job of the process engine (`io.atlas.user-task`), not a record of its own: it exists because a process instance waits on it, and completing it moves the instance on with the form's variables.
- The code lives in atlas's `api`, `engine`, `state`, `compiler` and `model` packages; the handlers import the engine, its storage and around twenty connectors. There is no task package that stands alone.
- Both projects are AGPL-3.0, so licensing would not prevent reuse.

## Considered options

1. **Import atlas's task code** as a Go module.
2. **Call atlas's API** from the server and show atlas's tasks in the inbox.
3. **Take atlas's concepts over into the item model**: an inbox of open work with an assignee, taking over and giving back, due dates and priorities.

## Decision outcome

Chosen option: **3**, for now.

Option 1 is not feasible: importing the tasks imports the process engine. Option 2 is feasible and useful where both run, but it is a new external dependency with its own authentication; per user it needs atlas's OAuth access tokens, since atlas's API tokens act as a machine, and atlas's API has no CORS, so every call would go through this server. That is a decision of its own (the open question above), not a precondition of a good inbox.

Option 3 fits ADR-0001: types are data, a task is any item whose type has a workflow.

- **Inbox:** `#/tasks` lists the open items: those whose type has a workflow and that are not in its last state. Three lists: *Assigned to me*, *Unassigned*, *All open*, each with its count, filtered by space. Overdue and due ones come first, then the most urgent, then by key.
- **Conventions, not new fields:** the assignee is the type's user field named `assignee` (or its first user field), the due date its first date field named like a deadline (as governance reads it, ADR-0040), the priority an enum named `priority`, its values ordered from low to high. The ready-made type `task` has all three.
- **Take over and give back:** set or clear the assignee, as atlas's claim and unclaim, last writer wins. Offered to who may edit the item; the server decides.
- **Moving on:** the workflow's transitions are buttons in the list.
- **On the item:** a user field shows the person's name and offers *Assign to me*.

Not taken over yet: candidate groups (who may take a task over), forms that complete a task, saved filter folders, and paging past the inbox's ceiling (it reads at most the board's 2000 items, and says so).

### Consequences

- Good: no new dependency, no format change; every type with a workflow is in the inbox at once.
- Good: the inbox works for imported items too, as their types carry the same kinds of fields.
- Bad: the conventions are names; a type that calls its deadline "target" has no due date in the inbox until it is renamed.
- Bad: the inbox is computed in the browser from the items one may read; for very many open items it needs a server-side query.

## Links

- amended by [ADR-0082](0082-a-state-no-transition-leaves-is-finished.md) (a state no transition leaves is finished)
- amended by [ADR-0080](0080-one-page-per-data-set-written-from-the-source.md) (which types are tasks)
