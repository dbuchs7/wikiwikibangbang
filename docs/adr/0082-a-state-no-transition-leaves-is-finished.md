# ADR-0082: A state no transition leaves is finished

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer decided that an accepted decision counts as done, 2026-10-21)

## Context and problem statement

The task list (ADR-0049) and the governance check for passed deadlines (ADR-0040) take an item as done when it is in its workflow's last state. A decision's workflow is `proposed → accepted | rejected`, with `rejected` last. An accepted decision therefore stayed in the task list for good, and its deadline counted as missed, while a rejected one was done. Which states are finished?

## Decision drivers

- A workflow says when work ends; the list of states should not need a particular order for that.
- Existing workflows keep their meaning: a task's `done` stays finished although `reopen` leaves it.

## Considered options

1. **The last state, and every state no transition leaves.**
2. **Every state no transition leaves, only.**
3. **A list of finished states set on the workflow.**

## Decision outcome

Chosen option: **1**.

- **Rule.** A state is finished when it is the workflow's last state, or when no transition has it among its `from` states (`Workflow.Finished`).
  - `accepted` and `rejected` of a decision are finished.
  - `done` of a task is finished, as the last state, although `reopen` leaves it.
- **Where it applies.** The task list shows only items that are not finished. The governance check reports a passed deadline only for items that are not finished.

### Consequences

- **Positive:** decisions, and any workflow with several end states, leave the task list when they end, without a change to the type.
- **Negative / trade-offs accepted:** a state without a way out that is not meant to end the work (a dead end by mistake) now counts as finished. A workflow that means otherwise needs a transition out of it.

## Pros and cons of the options

### Option 2: only states without a way out

Strongest form: a single, uniform rule with no special place for the last state.

- Bad: a task's `done` has `reopen` and would count as open work forever.

### Option 3: finished states set on the workflow

Strongest form: explicit, and free from guesses about intent.

- Bad: a change to the type model and to every existing type, for what the transitions already say in almost every case.

## Links

- amends [ADR-0049](0049-a-task-inbox-after-atlas.md) and ADR-0040 (which items are done)
- relates to ADR-0080 (which types are tasks)
