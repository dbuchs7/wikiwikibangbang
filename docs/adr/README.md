# Architecture Decision Records

Every load-bearing architectural decision is recorded as an ADR: context, decision drivers, considered options, decision and consequences.

- **Status** says whether the decision holds (`Proposed`, `Accepted`, `Superseded by ADR-XXXX`, `Deprecated`).
- **Implementation** says whether it is built (`Not started`, `Partial`, `Landed`, `Superseded`). `Landed` requires `Accepted`.

New records start from [`template.md`](template.md). An accepted decision is not overwritten; it is superseded by a new ADR.

ADR-0004 to ADR-0017 were adopted from the reference project [pblumer/atlas](https://github.com/pblumer/atlas) and adapted to this product; each names its atlas sources under *Context* and *Links*.

## Index

| No. | Title | Status | Implementation |
|---|---|---|---|
| [0001](0001-unified-item-data-model.md) | Unified item data model for knowledge and work | Proposed | Not started |
| [0002](0002-license-agpl-3.0-only.md) | License the project under AGPL-3.0-only | Accepted | Landed |
| [0003](0003-repository-governance-settings.md) | Repository governance settings for main | Accepted | Not started |
| [0004](0004-go-without-cgo.md) | Go as implementation language, without CGO | Accepted | Landed |
| [0005](0005-test-driven-development.md) | Test-driven development as the default workflow | Accepted | Landed |
| [0006](0006-decision-record-states-and-open-questions.md) | A decision record has two states, and its open questions expire | Accepted | Landed |
| [0007](0007-adr-numbers-assigned-at-merge.md) | ADR numbers are assigned at merge, not on a branch | Proposed | Not started |
| [0008](0008-event-sourced-persistence.md) | Event-sourced persistence in an embedded store, with a single writer | Proposed | Not started |
| [0009](0009-single-binary-with-embedded-web-ui.md) | A single binary with an embedded web UI | Proposed | Not started |
| [0010](0010-real-time-collaboration-with-block-locks.md) | Real-time collaboration starts with block-level locks, not a CRDT | Proposed | Not started |
| [0011](0011-http-api-route-table-and-access-classes.md) | The HTTP API is one route table, described by OpenAPI, and every route declares its access class | Proposed | Not started |
| [0012](0012-mcp-adapter-over-http-api.md) | An MCP server as an adapter over the HTTP API, acting as its caller | Proposed | Not started |
| [0013](0013-authentication-and-security-boundary.md) | Authentication is required by default, behind one principal boundary | Proposed | Not started |
| [0014](0014-closed-renderer-for-user-content.md) | User content is rendered by one closed renderer, never as raw HTML | Proposed | Not started |
| [0015](0015-resource-budgets-and-paged-listings.md) | One place names every resource budget, and a capped listing answers with a page | Proposed | Not started |
| [0016](0016-metrics-and-backup.md) | Operational metrics behind authentication, and an allowlisted backup | Proposed | Not started |
| [0017](0017-internationalisation-via-message-catalogue.md) | The UI is translated through a message catalogue, with English as source | Proposed | Not started |
| [0018](0018-pull-request-level-dco-sign-off.md) | The DCO sign-off may be given once per pull request | Accepted | Partial |
| [0019](0019-dco-enforcement-suspended.md) | DCO enforcement is suspended until the maintainer switches it on | Accepted | Landed |
| [0020](0020-embedding-and-containment-are-independent.md) | Embedding shows an item, containment places it — the two stay independent | Proposed | Not started |
| [0021](0021-deletion-only-when-unreferenced.md) | Deletion only when unreferenced, then trash, then purge | Accepted | Partial |
| [0022](0022-permission-model.md) | Space roles, narrowing restrictions, and a logged erasure role | Proposed | Not started |
