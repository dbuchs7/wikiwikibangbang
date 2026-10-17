# ADR-0007: ADR numbers are assigned at merge, not on a branch

- **Status:** Accepted
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17
- **Open question:** How does a numbering step write to `main` when the `protect-main` ruleset (ADR-0003) allows no bypass and no direct pushes?
- **Question checked:** 2026-10

## Context and problem statement

Today an author picks the next free ADR number on their branch and adds a row to the index. With several agents working in parallel, two branches pick the same number and both touch the same line of `docs/adr/README.md`. The second merge then conflicts, or — worse — two records share a number.

Adapted from atlas [ADR-0170](https://github.com/pblumer/atlas/blob/main/docs/adr/0170-adr-numbers-assigned-at-merge.md).

## Decision drivers

- Parallel branches must not collide on ADR numbers or index lines.
- A record keeps a stable, citable name while it is in flight.
- The final number is unambiguous once the record is on `main`.
- The mechanism must be compatible with the branch protection of ADR-0003.

## Considered options

1. **Pick the next number on the branch** (current practice)
2. **Drafts without a number; a workflow numbers them on `main` after merge** (atlas)
3. **Drafts without a number; the number is assigned in the pull request as its last commit before merge**

## Decision outcome

Chosen option: **2 or 3 — drafts carry no number while in flight**; the choice between them depends on the open question.

A record in flight:

- lives at `docs/adr/draft-<slug>.md` and heads with `# ADR-DRAFT: Title`;
- has no row in the index;
- is cited as `ADR-draft-<slug>`.

A numbering tool (`go run ./docs/adr/cmd/adrnum`) renames each draft to the next free number, fixes its heading, adds the index row and rewrites every `ADR-draft-<slug>` citation.

**Conflict with ADR-0003:** atlas runs this tool in a workflow that pushes to `main`. Here the ruleset has an empty bypass list, so such a push is rejected. Option 3 avoids the conflict — the human merging the pull request runs the tool as the last, signed-off commit — but reintroduces a small race between two pull requests merged in quick succession, which the required "up to date" check then catches. Option 2 needs a bypass for one GitHub App, which weakens ADR-0003.

Until this is decided, the current practice (option 1) stays in force.

### Consequences

- **Positive:** no number collisions between parallel branches.
- **Negative / trade-offs accepted:** a tool and tests to maintain; citations change from draft names to numbers.
- **Follow-ups / risks to watch:** decide the open question; then implement the tool, extend `go test ./docs/adr` to accept drafts, and update AGENTS.md.

## Pros and cons of the options

### Number on the branch

Strongest form: no tooling at all; the number is known from the first commit and can be cited immediately.

- Good: simple.
- Bad: collisions with parallel work, which is the normal case with several agents.

### Workflow numbers on main

- Good: one place decides; authors never think about numbers.
- Bad: needs push rights to `main` for a bot, contradicting the empty bypass list of ADR-0003.

### Number assigned in the pull request

- Good: compatible with ADR-0003; the numbering commit is reviewed and signed off like any other.
- Bad: a manual step at merge time; two quick merges can still race (caught, not prevented, by the up-to-date requirement).

## Links

- atlas [ADR-0170](https://github.com/pblumer/atlas/blob/main/docs/adr/0170-adr-numbers-assigned-at-merge.md)
- conflicts with [ADR-0003](0003-repository-governance-settings.md) in its atlas form
