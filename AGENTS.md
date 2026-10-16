# AGENTS.md

Operational guide for AI coding agents working on wikiwikibangbang. This file follows the [agents.md](https://agents.md) convention; [`CLAUDE.md`](CLAUDE.md) points here. It is the single source of truth for how agents work in this repository — do not duplicate its guidance elsewhere.

> **Read this whole file before writing anything.** The project is at its very beginning: the decisions recorded now shape every later line of code.

---

## What this project is

wikiwikibangbang is an open-source application written in **Go** that unifies **knowledge** (wiki pages, page tree, versions, comments — the Confluence domain) and **work** (issues, fields, workflows, boards, queries — the Jira domain) in **one data model**.

The core idea is recorded in [ADR-0001](docs/adr/0001-unified-item-data-model.md): every piece of user-generated content is a typed **item** with properties, an optional document body, an optional workflow and typed relations. Pages and issues are item *types*, not separate systems.

**Current state:** design phase. The Go module exists, but so far it holds only the project's self-checks (ADR consistency, DCO rule) — no product code yet.

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

A change is not done until the whole sequence is green. CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs the same commands on every push and pull request:

```bash
gofmt -l .                 # must print nothing
go vet ./...
go test -race ./...        # includes the ADR checks in docs/adr
```

On pull requests CI also runs the DCO check over the proposed commits. Run it locally before asking for review:

```bash
go run ./cmd/dcocheck origin/main HEAD
```

The ADR tests in `docs/adr` enforce file names, headings, front matter, the status rules (`Landed` requires `Accepted`; an `Open question` needs a `Question checked` month no older than a year) and that the index in `docs/adr/README.md` matches the records. A failing ADR test is fixed in the record or the index, never by loosening the test.

When a command is added (linters, end-to-end tests, a `Makefile` target), add it here and to CI in the same pull request.

`main` is protected by the ruleset in [`.github/rulesets/protect-main.json`](.github/rulesets/protect-main.json) ([ADR-0003](docs/adr/0003-repository-governance-settings.md)): every change goes through a pull request with green required checks; direct pushes and force pushes to `main` are rejected. **Never rename a CI job without updating the ruleset in the same pull request** — required checks are matched by job name, and a renamed job blocks every merge. `internal/repoconfig` tests this.

## Repository layout

```text
AGENTS.md          this guide (canonical)
CLAUDE.md          pointer to this guide for Claude agents
LICENSE            AGPL-3.0-only (ADR-0002)
go.mod             module github.com/dbuchs7/wikiwikibangbang
.github/workflows/ CI: fmt, vet, race tests, DCO check on pull requests
.github/rulesets/  ruleset protecting main, source of truth for GitHub settings (ADR-0003)
cmd/dcocheck/      CLI that checks DCO sign-offs over a git range
internal/dco/      DCO sign-off rules (humans only), with tests
internal/repoconfig/ tests keeping ruleset and CI workflow consistent
docs/adr/          architecture decision records, index in README.md, consistency tests
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

## License and dependencies

The project is licensed under **AGPL-3.0-only** ([`LICENSE`](LICENSE), [ADR-0002](docs/adr/0002-license-agpl-3.0-only.md)). Contributions are accepted under the same license; commits are signed off to certify the [Developer Certificate of Origin](https://developercertificate.org/).

### DCO sign-off: only humans sign

> **Current mode: advisory ([ADR-0019](docs/adr/0019-dco-enforcement-suspended.md)).** The DCO check reports missing sign-offs as a warning and does not fail. The rules below still apply — above all, agents never sign. Switching to enforcement is the maintainer's decision; agents do not change `DCO_MODE` on their own.

The DCO is a statement by a natural person that they have the right to submit the contribution under the project license. An AI agent cannot make that statement. Therefore:

- **Agents never write a `Signed-off-by` line anywhere** — not in commits, not in pull request descriptions, not in comments — neither with their own identity nor on behalf of a person. Do not run `git commit -s` as an agent. When an agent drafts a pull request description, it leaves the sign-off out; the human adds it.
- **Agents credit themselves only with a `Co-Authored-By` trailer** (e.g. `Co-Authored-By: Claude <noreply@anthropic.com>`).
- **The responsible human signs off** — the person who directed the agent and submits the pull request — after reviewing the diff, in one of two ways ([ADR-0018](docs/adr/0018-pull-request-level-dco-sign-off.md)):
  1. **In the pull request description (default):** edit the description and add a line of its own, outside any code block:
     `Signed-off-by: Full Name <email>`
     The DCO check re-runs on the edit and accepts every commit of the pull request that has no sign-off of its own. On squash merge, GitHub copies the description into the commit message, so the sign-off lands in the history of `main`.
  2. **On the commits:** `git rebase --signoff origin/main` on the pull request branch, then `git push --force-with-lease`.
- **By signing off, the human takes responsibility** for every commit of the pull request, including agent-written code: in particular that it does not contain code copied from sources with incompatible or unknown licenses.
- A sign-off that names an AI agent or a bot fails the check wherever it appears, and a human sign-off elsewhere does not cure it.
- Pull requests are merged by **squash** only, after the checks have finished. Once enforcement is on, a pull request without a human sign-off is not merged.
- Agents act on GitHub under the maintainer's account, so GitHub cannot tell an agent's edit from the maintainer's. The check enforces the *form* of the rule; that only a human writes the line is enforced by this guide.

Before adding a dependency, check that its license is compatible with AGPL-3.0 and name the license in the pull request. Never copy code from sources whose license is incompatible or unknown.

## Pointers

- [ADR index](docs/adr/README.md)
- [ADR-0001: Unified item data model](docs/adr/0001-unified-item-data-model.md)
- Reference project for working method (ADRs, invariants, test discipline): [pblumer/atlas](https://github.com/pblumer/atlas)
