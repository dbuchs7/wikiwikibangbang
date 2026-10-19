# AGENTS.md

Operational guide for AI coding agents working on wikiwikibangbang. This file follows the [agents.md](https://agents.md) convention; [`CLAUDE.md`](CLAUDE.md) points here. It is the single source of truth for how agents work in this repository — do not duplicate its guidance elsewhere.

> **Read this whole file before writing anything.** The project is at its very beginning: the decisions recorded now shape every later line of code.

---

## What this project is

wikiwikibangbang is an open-source application written in **Go** that unifies **knowledge** (wiki pages, page tree, versions, comments) and **work** (issues, fields, workflows, boards, queries) in **one data model**.

The core idea is recorded in [ADR-0001](docs/adr/0001-unified-item-data-model.md): every piece of user-generated content is a typed **item** with properties, an optional document body, an optional workflow and typed relations. Pages and issues are item *types*, not separate systems.

**Current state:** early prototype. Besides the project's self-checks (ADR consistency, DCO rule) the module holds two prototype packages: `internal/item`, the item model with lifecycle and permissions (ADR-0001, ADR-0020 to ADR-0022), and `internal/migrate`, the import engine (ADR-0023). The item store persists to an event log on disk (`internal/wal`, `internal/storage`; ADR-0008, ADR-0025): every command is fsynced before it takes effect, and startup replays the log. Item content is encrypted with a key per item; purge and redaction destroy the key (ADR-0026). Authentication exists (`internal/auth`; ADR-0013, ADR-0027): local accounts, sessions and API tokens; the first start creates an administrator; security events go to a persisted audit trail (ADR-0029); OpenID Connect single sign-on is optional (ADR-0030). The single binary `cmd/wikiwikibangbang` opens the data directory and serves the web UI, a liveness probe, the authentication API and the item API (ADR-0028, described at `/api/openapi.json`); it is released for every common platform and as a container image (ADR-0024). A buildless web UI (`internal/web`, ADR-0031) runs on top of the API; bodies are edited with ProseMirror, vendored as one pre-built bundle (ADR-0032). Full-text search scans the in-memory state through the permission check (ADR-0033). Every item carries comments, held as events on the item and erased with it (ADR-0035). Several people edit a body at once in a live session with block locks (`internal/collab`, ADR-0010). Administrators download encrypted backups of a running server; `restore` brings one back (`internal/backup`, ADR-0034). Operational metrics are at `/metrics` for administrators and metrics-scoped tokens (ADR-0016). AI agents reach the API through an MCP adapter, at `POST /mcp` or as the stdio command `mcp`, with exactly their caller's rights (`internal/mcp`, ADR-0012).

**The persisted event format is a contract with every existing data directory** (ADR-0025). Never change what `item.EncodeBatch` writes — a field name, a value encoding, a role name — without raising `item.CodecVersion` and teaching the decoder the old version. `TestPersistedFormatIsStable` fails on any change; regenerating its golden file is the last step of such a change, never the fix for a failing test. `apply` must produce the same state from a decoded event as from the live one: every test in `internal/item` checks that.

**Content never reaches disk in plaintext** (ADR-0026). Anything a user can type into an item is content: if you add such a field to an event, it belongs in `item.Event.erase` and in the sealed part of the codec (`wireContent`), and `internal/storage` must show it does not appear in the event file. `keys.wal` is as precious as `events.wal`: without it all content is lost.

**Every resource budget lives in `internal/budget`** (ADR-0015). A new bound on what a caller can make the server hold — a size, a count, a number of sessions — is a field of `budget.Budgets`, read with `budget.Get()`, never a constant in the package that uses it. A bound that is not a budget (a security policy, a file-format limit) is classified with its reason in `internal/budget/registry_test.go`; the test fails on any `max…` constant or reading ceiling that is neither.

**Every route is authenticated unless it says otherwise** (ADR-0011, ADR-0027). Declare `Access: Public` only for what an anonymous caller may see, and add it to `publicAPI` in the server tests, and add the route to the table in `internal/server`, never to a mux directly. Handlers reach the item store only through `storage.DB.Read`/`Write`. Never log or audit a password, session or token secret — the generated first-administrator password is the one deliberate exception. A security-relevant action records an `audit.Entry` after it succeeded.

**The MCP adapter only calls the public API** (ADR-0012). A tool is one HTTP request with the caller's credential, never an internal call; the adapter holds no credential of its own. Offering a new route as a tool is a decision: destructive, permission-changing and credential routes are deliberately not offered, and `internal/mcp/tools.go` says so.

**UI code never turns a string into markup or code** (ADR-0014, ADR-0031). Create elements with `h()` from `internal/web/static/assets/dom.js`; `TestNoMarkupFromStrings` forbids `innerHTML` and its relatives. A UI change that adds a flow extends `e2e/smoke.cjs`.

**The product ships as one binary** (ADR-0009, ADR-0024): pure Go, no CGO, web assets embedded with `go:embed`, cross-compiled for every release platform. Never add anything that breaks this — a runtime file the binary needs next to it, a C dependency, a CDN asset, a second executable.

## Project language: English

English is the project language. This is binding for everything committed to the repository:

- code: identifiers, comments, log and error messages, test names;
- documentation: ADRs, README files, architecture docs, this file;
- commit messages, branch names, pull request titles and descriptions, issues and review comments;
- the default locale and source strings of the user interface (translations are added through i18n, never hard-coded).

**Agents write in English regardless of the language of the request.** A contributor may ask in German or any other language; the conversation with that person may follow their language, but every artifact that lands in the repository is English. If you find non-English content in the repository, translate it in a separate commit rather than mixing languages in one file.

## Other products are named only for import

Confluence and Jira are named only where the subject is importing from them: the import and sync engine ([ADR-0023](docs/adr/0023-import-and-sync-from-jira-and-confluence.md)), its readers and mappings, and their documentation and tests. Everywhere else — code, documentation, ADRs, the UI, commit messages, pull requests, release notes — describe the product in its own terms (wiki pages, issues, comments, workflows, boards), never by comparison with another product. This is the maintainer's decision of 2026-10-19; ADRs accepted before it keep their wording as recorded.

## The invariants (do not break these)

These follow from ADR-0001, accepted on 2026-10-17, and are settled. [`docs/architecture/invariants.md`](docs/architecture/invariants.md) lists them with the checks that hold them. If a task seems to require breaking one, **stop and flag it** — it needs a new or superseding ADR, not a workaround.

1. **D1 – One model for content.** User-generated content is exclusively items. Never introduce a parallel domain object (`Page`, `Issue`, …) for a single content type. System objects (users, groups, spaces, type definitions) are exempt.
2. **D2 – Stable identity.** Internal references (relations, document references, events) always use the immutable `id`, never the human-readable `key`.
3. **D3 – Validated changes.** Properties are written only through schema-validated change commands; `state` changes only through workflow transitions. The one exception is an import (ADR-0023), which sets the source's current state directly — still validated against the type's states — because imported history is data, not a replay of transitions.
4. **D4 – No shadow data in the document.** Anything queryable is a property. Relations derived from a document body are a deterministic function of the document version.
5. **D5 – One containment parent.** Every item has at most one `parent`. Permission inheritance follows containment only; every other hierarchy is a typed relation.
6. **D6 – Type-agnostic cross-cutting capabilities.** No type-specific code in search, permissions, history, comments or notifications.
7. **D7 – Identities are permanent.** An item's ID is never reused and always resolves — to the item, to the item in the trash, or to its tombstone. Deletion removes content, never identity. ([ADR-0021](docs/adr/0021-deletion-only-when-unreferenced.md))
8. **D8 – No deletion while referenced.** An item is trashed only when no live item outside its subtree references it; deleting never leaves live content pointing at it. The one exception is redaction by the erasure role, which is logged with a reason. ([ADR-0021](docs/adr/0021-deletion-only-when-unreferenced.md), [ADR-0022](docs/adr/0022-permission-model.md))
9. **D9 – Access only narrows below the space.** An item is readable (editable) only if the space role allows it and no restriction on it or its ancestors excludes the principal; restrictions never grant. ([ADR-0022](docs/adr/0022-permission-model.md))
10. **D10 – Unreadable is non-existent, and only sessions face callers.** Nothing a caller receives — errors, embeds, relations, search results, history — reveals an item the caller may not read. API and MCP code act through `Store.As(principal)` sessions, never through the trusted `Store` commands. ([ADR-0022](docs/adr/0022-permission-model.md))

## Commands

A change is not done until the whole sequence is green. CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs the same commands on every push and pull request:

```bash
gofmt -l .                 # must print nothing
go vet ./...
go test -race ./...        # includes the ADR checks in docs/adr
go test -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -1   # floor 90% (ADR-0005)
```

Run and package the binary:

```bash
go run ./cmd/wikiwikibangbang serve -addr :8080      # http://localhost:8080, data in ./data
go run ./cmd/wikiwikibangbang check -data-dir data   # verify a data directory, read-only
cd e2e && npm ci && BASE_URL=http://127.0.0.1:8080 ADMIN_PASSWORD=… node smoke.cjs   # UI in Chromium, against a fresh server
WWBB_TOKEN=… go run ./cmd/wikiwikibangbang mcp -url http://localhost:8080   # MCP tools on stdin/stdout for a local agent
go run ./cmd/wikiwikibangbang version
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/wikiwikibangbang   # any release platform
docker build -t wikiwikibangbang:dev .               # container image
```

CI builds every release platform on every push (`cross-build`); the list must equal the matrix in [`.github/workflows/release.yml`](.github/workflows/release.yml), which `internal/repoconfig` checks. A release is cut by a `v*` tag — pushed, or created on GitHub's release page, after which the workflow attaches the archives to that release — and that is the maintainer's decision, never an agent's. A release whose archives are missing is completed by running the `Release` workflow by hand with its tag.

On pull requests CI also runs the DCO check over the proposed commits. Run it locally before asking for review:

```bash
go run ./cmd/dcocheck origin/main HEAD
```

The repository settings on GitHub are compared with `.github/repository-settings.json` and `.github/rulesets/*.json` once a day by the `Settings drift` workflow ([ADR-0003](docs/adr/0003-repository-governance-settings.md)). A change to a setting goes into those files by pull request first and is applied on GitHub after the merge. Run the comparison locally with:

```bash
GITHUB_TOKEN=… go run ./cmd/settingscheck -repo dbuchs7/wikiwikibangbang   # exit 1 on drift
```

The default token cannot read the merge settings or the ruleset's bypass list. The workflow therefore uses the secret `SETTINGS_READ_TOKEN` (read-only, this repository, *Administration: read*) when it exists, and then runs with `-strict`, which also fails on a value it cannot read. Creating and renewing that token is the maintainer's task, never an agent's.

The ADR tests in `docs/adr` enforce file names, headings, front matter, the status rules (`Landed` requires `Accepted`; an `Open question` needs a `Question checked` month no older than a year) and that the index in `docs/adr/README.md` matches the records. A failing ADR test is fixed in the record or the index, never by loosening the test.

When a command is added (linters, end-to-end tests, a `Makefile` target), add it here and to CI in the same pull request.

`main` is protected by the ruleset in [`.github/rulesets/protect-main.json`](.github/rulesets/protect-main.json) ([ADR-0003](docs/adr/0003-repository-governance-settings.md)): every change goes through a pull request with green required checks; direct pushes and force pushes to `main` are rejected. **Never rename a CI job without updating the ruleset in the same pull request** — required checks are matched by job name, and a renamed job blocks every merge. `internal/repoconfig` tests this.

## Repository layout

```text
AGENTS.md          this guide (canonical)
CLAUDE.md          pointer to this guide for Claude agents
LICENSE            AGPL-3.0-only (ADR-0002)
THIRD_PARTY_NOTICES.md licenses of the modules in the binary; shipped with every release
README.md          what the project is and how to run it
SECURITY.md        how to report a vulnerability privately
go.mod             module github.com/dbuchs7/wikiwikibangbang
.github/workflows/ CI (fmt, vet, race tests, cross-build, DCO), container image, release, settings drift
.github/rulesets/  ruleset protecting main, source of truth for GitHub settings (ADR-0003)
.github/repository-settings.json  general GitHub settings (merge methods, default branch), checked daily for drift (ADR-0003)
Dockerfile         container image: distroless, nonroot, multi-arch (ADR-0024)
cmd/wikiwikibangbang/ the product's single binary: serve, check, healthcheck, version (ADR-0024)
cmd/dcocheck/      CLI that checks DCO sign-offs over a git range
cmd/settingscheck/ CLI that compares the GitHub settings with the recorded files (ADR-0003)
internal/audit/    security audit trail, persisted in audit.wal (ADR-0029)
internal/auth/     accounts, password hashing, sessions, API tokens, login throttle (ADR-0027)
internal/budget/   every resource budget in one struct, set from WWBB_BUDGET_* (ADR-0015)
internal/buildinfo/ version stamped at build time
internal/storage/  data directory: recovers the item store, journals every command, per-item keys (ADR-0025, ADR-0026)
internal/wal/      append-only log file: checksummed records, fsync, crash recovery, directory lock
internal/sso/       OpenID Connect relying party (ADR-0030); sso/ssotest is a fake provider for tests
internal/mcp/      MCP adapter over the HTTP API: JSON-RPC, tool table, stdio transport (ADR-0012)
internal/metrics/  Prometheus metrics on a registry of its own, labelled by route pattern (ADR-0016)
internal/backup/   encrypted backups and restore of a data directory (ADR-0016, ADR-0034)
internal/collab/   live editing sessions: block locks, commit on release (ADR-0010)
internal/server/   HTTP route table with access classes, principal resolution, JSON API, graceful shutdown
internal/web/      embedded web UI (plain HTML, CSS, ES modules; ADR-0009, ADR-0031)
internal/web/vendor/  pinned npm packages and build.sh for static/vendor/prosemirror.js (ADR-0032); only needed to update the editor
internal/dco/      DCO sign-off rules (humans only), with tests
internal/item/     prototype of the unified item model (ADR-0001), event-sourced, versioned event codec
internal/migrate/  prototype of the Jira/Confluence import engine (ADR-0023)
internal/repoconfig/ tests keeping ruleset, CI workflow and release matrix consistent
internal/settingsdrift/ comparison of recorded and live GitHub settings (ADR-0003)
e2e/               browser smoke test of the UI (Playwright; test tool only, not part of the build)
docs/architecture/ the invariants with their checks
docs/brand/        the logo in every format and its generator; the web UI embeds copies
docs/testing/      the guide for people testing a release; .github/ISSUE_TEMPLATE/ holds the matching report form
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

Before adding a dependency, check that its license is compatible with AGPL-3.0, name the license in the pull request, and add the module with its version, license and any NOTICE to `THIRD_PARTY_NOTICES.md` (a test in `internal/repoconfig` checks the list against `go.mod`). Current dependencies: go-oidc, go-jose (Apache-2.0) and x/oauth2 (BSD-3-Clause), for OIDC only (ADR-0030). Never copy code from sources whose license is incompatible or unknown.

## Pointers

- [ADR index](docs/adr/README.md)
- [ADR-0001: Unified item data model](docs/adr/0001-unified-item-data-model.md)
- Reference project for working method (ADRs, invariants, test discipline): [pblumer/atlas](https://github.com/pblumer/atlas)
