# AGENTS.md

Operational guide for AI coding agents working on wikiwikibangbang. This file follows the [agents.md](https://agents.md) convention; [`CLAUDE.md`](CLAUDE.md) points here. It is the single source of truth for how agents work in this repository — do not duplicate its guidance elsewhere.

> **Read this whole file before writing anything.** The project is at its very beginning: the decisions recorded now shape every later line of code.

---

## What this project is

wikiwikibangbang is an open-source application written in **Go** that unifies **knowledge** (wiki pages, page tree, versions, comments — the Confluence domain) and **work** (issues, fields, workflows, boards, queries — the Jira domain) in **one data model**.

The core idea is recorded in [ADR-0001](docs/adr/0001-unified-item-data-model.md): every piece of user-generated content is a typed **item** with properties, an optional document body, an optional workflow and typed relations. Pages and issues are item *types*, not separate systems.

**Current state:** design phase. There is no Go module and no code yet. Commands below marked *(planned)* become binding as soon as the corresponding code exists.

## Project language: English

English is the project language. This is binding for everything committed to the repository:

- code: identifiers, comments, log and error messages, test names;
- documentation: ADRs, README files, architecture docs, this file;
- commit messages, branch names, pull request titles and descriptions, issues and review comments;
- the default locale and source strings of the user interface (translations are added through i18n, never hard-coded).

**Agents write in English regardless of the language of the request.** A contributor may ask in German or any other language; the conversation with that person may follow their language, but every artifact that lands in the repository is English. If you find non-English content in the repository, translate it in a separate commit rather than mixing languages in one file.

## The invariants (do not break these)

These follow from ADR-0001. That ADR is currently **Proposed**, so the invariants are the agreed design direction rather than settled law: build towards them, and if a task seems to require breaking one, **stop and flag it** — it needs a new or superseding ADR, not a workaround. Once ADR-0001 is accepted they move into `docs/architecture/invariants.md` with checks attached.

1. **D1 – One model for content.** User-generated content is exclusively items. Never introduce a parallel domain object (`Page`, `Issue`, …) for a single content type. System objects (users, groups, spaces, type definitions) are exempt.
2. **D2 – Stable identity.** Internal references (relations, document references, events) always use the immutable `id`, never the human-readable `key`.
3. **D3 – Validated changes.** Properties are written only through schema-validated change commands; `state` changes only through workflow transitions.
4. **D4 – No shadow data in the document.** Anything queryable is a property. Relations derived from a document body are a deterministic function of the document version.
5. **D5 – One containment parent.** Every item has at most one `parent`. Permission inheritance follows containment only; every other hierarchy is a typed relation.
6. **D6 – Type-agnostic cross-cutting capabilities.** No type-specific code in search, permissions, history, comments or notifications.

## Commands

*(planned — binding once the Go module exists)* A change is not done until the whole sequence is green:

```bash
gofmt -l .                 # must print nothing
go vet ./...
go test -race ./...
```

When a command is added (linters, end-to-end tests, a `Makefile` target), add it here in the same pull request.

## Repository layout

```text
AGENTS.md          this guide (canonical)
CLAUDE.md          pointer to this guide for Claude agents
docs/adr/          architecture decision records, index in docs/adr/README.md
```

Update this section whenever a top-level directory is added.

## How to approach a task

1. **Read the relevant ADRs** in [`docs/adr/`](docs/adr/) before designing anything. They explain *why*.
2. **Check the invariants** above against your plan *before* writing code.
3. **Work test-first.** Write a failing test that states the intended behaviour, make it pass with the minimum code, then refactor. A bug fix starts with a failing regression test.
4. **Run the full check sequence** (see Commands) until green, including `-race`.
5. **Record architectural decisions.** If your change makes or alters an architectural decision, write an ADR instead of silently diverging:
   - copy [`docs/adr/template.md`](docs/adr/template.md) to `docs/adr/NNNN-<slug>.md`, using the next free number;
   - add a row to the index in [`docs/adr/README.md`](docs/adr/README.md);
   - new records are `Proposed` / `Not started`; `Implementation: Landed` requires `Status: Accepted`;
   - an accepted decision is never edited in substance — supersede it with a new ADR.
   - Present the strongest form of every rejected option before rejecting it, and list what the decision could not settle under *Open questions*.
6. **Keep changes small and focused.** One concern per pull request; no drive-by refactoring.

## Style

- Go code follows `gofmt` and the conventions of [Effective Go](https://go.dev/doc/effective_go).
- Prefer the standard library; every new dependency needs a reason in the pull request (and an ADR if it is architectural, e.g. storage, CRDT library, search engine).
- Documentation is precise and neutral: no marketing language, no unverified claims. Mark assumptions as assumptions.

## Pointers

- [ADR index](docs/adr/README.md)
- [ADR-0001: Unified item data model](docs/adr/0001-unified-item-data-model.md)
- Reference project for working method (ADRs, invariants, test discipline): [pblumer/atlas](https://github.com/pblumer/atlas)
