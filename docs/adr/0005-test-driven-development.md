# ADR-0005: Test-driven development as the default workflow

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

Much of the code in this repository will be written by AI agents at high speed. Speed without a safety net produces code whose behaviour nobody has pinned down. The project wants self-tests in the style of atlas: behaviour that is stated before it is built, and checks that fail loudly when it changes.

The question is: **when is a test written, and what does CI hold the repository to?**

Adapted from atlas [ADR-0018](https://github.com/pblumer/atlas/blob/main/docs/adr/0018-test-driven-development.md), including the lessons of its three amendments about the coverage floor.

## Decision drivers

- Tests describe intended behaviour, not the implementation that happened to be written.
- Agent-written code must be held to the same bar as human-written code.
- Error, recovery and permission paths get first-class coverage.
- A coverage number must not invite "coverage theatre" (tests that execute lines without asserting anything).

## Considered options

1. **Tests after the code, no gate**
2. **Test-driven development as the default, with a repository-wide coverage floor**
3. **Test-driven development, with a per-change coverage-delta gate**

## Decision outcome

Chosen option: **2.** For any change to behaviour, persistence, permissions or a public API:

1. **Red** — write a test that states the intended behaviour and fails for the right reason.
2. **Green** — write the minimum code to make it pass.
3. **Refactor** — clean up with the test as the safety net, keeping `go test -race ./...` green.

Stated exceptions, named in the pull request: purely mechanical changes (renames, formatting, dependency bumps, documentation); spikes, which are thrown away and redone test-first before merge. A bug fix always starts with a failing regression test.

**Coverage floor:** CI checks one repository-wide statement-coverage number. The floor starts at **90%** and is introduced together with the first product package. Lessons taken from atlas:

- The measurement must be reproducible (count each coverage block once).
- A floor with a margin of a few statements fails on whichever change comes next, not on the one that spent the margin; keep a real margin.
- The floor is an alarm, not a target. It is raised when the number holds comfortably above it, and it is never lowered silently — a change to it is an amendment to this record.

### Consequences

- **Positive:** behaviour is pinned before it exists; contributors and agents have an unambiguous answer to "when do I write the test?"; regressions in coverage become a visible CI failure.
- **Negative / trade-offs accepted:** more effort per change up front; a repository-wide number can hide a poorly tested new package behind well-tested old ones, so reviewers still check that new code carries its own tests.
- **Follow-ups / risks to watch:** add the coverage check to CI with the first product package (then set *Implementation* to `Landed`); watch for coverage theatre in review.

## Pros and cons of the options

### Tests after the code, no gate

Strongest form: the fastest way to explore an unknown problem; tests written afterwards test what was actually built.

- Good: no friction while the design is still moving.
- Bad: tests confirm the implementation instead of the intent; with agent-generated code, untested paths accumulate unnoticed.

### TDD with a repository-wide floor

- Good: intent first; one simple, explainable number in CI.
- Bad: up-front effort; a floor needs active care (see atlas amendments).

### TDD with a per-change coverage-delta gate

Strongest form: every change must carry its own tests; nothing can hide behind the rest of the repository.

- Good: precise per change.
- Bad: rewards contrived tests for hard-to-reach defensive code; punishes legitimate exceptions; atlas rejected it for this reason.

## Links

- atlas [ADR-0018](https://github.com/pblumer/atlas/blob/main/docs/adr/0018-test-driven-development.md)
- [AGENTS.md](../../AGENTS.md), "How to approach a task"
