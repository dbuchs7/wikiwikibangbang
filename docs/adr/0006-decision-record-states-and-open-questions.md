# ADR-0006: A decision record has two states, and its open questions expire

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

Architecture decision records are only useful if a reader can tell, without reading the code, whether a decision holds and whether it has been built. A single status field mixes the two: "Accepted" is read as "done" although nothing may be implemented yet, and a record built on an assumption nobody verified reads as settled truth for years.

Adapted from atlas [ADR-0298](https://github.com/pblumer/atlas/blob/main/docs/adr/0298-two-states-for-a-record.md) and [ADR-0293](https://github.com/pblumer/atlas/blob/main/docs/adr/0293-open-questions-in-records-expire.md). The template and the tests in `docs/adr` already followed these rules; this record states them as a decision.

## Decision drivers

- A reader distinguishes "decided" from "built" at a glance.
- An unresolved assumption stays visible until somebody looks at it again.
- The rules are checked by tests, not by reviewers' memory.

## Considered options

1. **One status field**
2. **Two state lines (Status, Implementation) and free-text open questions**
3. **Two state lines, plus an open question with a "checked" month that expires**

## Decision outcome

Chosen option: **3.**

- **Status** says whether the decision holds: `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, `Deprecated`.
- **Implementation** says whether it is built: `Not started`, `Partial`, `Landed`, `Superseded`. `Landed` requires `Accepted`.
- A record whose reasoning rests on something unresolved carries **`Open question`** and **`Question checked: YYYY-MM`** as a pair. The month records when somebody last looked — a fact, not a promise. The test fails when the month is more than twelve months old **or lies in the future**, because a future month would silence the check.

`go test ./docs/adr` enforces all of it, together with file names, headings and the index.

### Consequences

- **Positive:** records cannot claim to be built while they are not; open questions resurface once a year by failing CI.
- **Negative / trade-offs accepted:** a red CI run can be caused by the calendar rather than by a change; the fix is to look at the question and update the month, which is the intended effect.
- **Follow-ups / risks to watch:** updating the month without actually looking defeats the mechanism; reviewers check that a month change comes with a sentence on what was looked at.

## Pros and cons of the options

### One status field

Strongest form: simplest possible format, used by most ADR templates.

- Good: minimal.
- Bad: "Accepted" is ambiguous between decided and built.

### Two state lines, free-text open questions

Strongest form: separates the two states without any mechanism that can fail on its own.

- Good: clear states.
- Bad: open questions are written once and never revisited.

### Two state lines with expiring open questions

- Good: both problems addressed and tested.
- Bad: a calendar-driven failure in CI.

## Links

- atlas [ADR-0298](https://github.com/pblumer/atlas/blob/main/docs/adr/0298-two-states-for-a-record.md), [ADR-0293](https://github.com/pblumer/atlas/blob/main/docs/adr/0293-open-questions-in-records-expire.md)
- [`docs/adr/adr_test.go`](adr_test.go)
