# Architecture Decision Records

Every load-bearing architectural decision is recorded as an ADR: context, decision drivers, considered options, decision and consequences.

- **Status** says whether the decision holds (`Proposed`, `Accepted`, `Superseded by ADR-XXXX`, `Deprecated`).
- **Implementation** says whether it is built (`Not started`, `Partial`, `Landed`, `Superseded`). `Landed` requires `Accepted`.

New records start from [`template.md`](template.md). An accepted decision is not overwritten; it is superseded by a new ADR.

ADR-0004 to ADR-0017 were adopted from the reference project [pblumer/atlas](https://github.com/pblumer/atlas) and adapted to this product; each names its atlas sources under *Context* and *Links*.

## Index

| No. | Title | Status | Implementation |
|---|---|---|---|
| [0001](0001-unified-item-data-model.md) | Unified item data model for knowledge and work | Accepted | Partial |
| [0002](0002-license-agpl-3.0-only.md) | License the project under AGPL-3.0-only | Accepted | Landed |
| [0003](0003-repository-governance-settings.md) | Repository governance settings for main | Accepted | Landed |
| [0004](0004-go-without-cgo.md) | Go as implementation language, without CGO | Accepted | Landed |
| [0005](0005-test-driven-development.md) | Test-driven development as the default workflow | Accepted | Landed |
| [0006](0006-decision-record-states-and-open-questions.md) | A decision record has two states, and its open questions expire | Accepted | Landed |
| [0007](0007-adr-numbers-assigned-at-merge.md) | ADR numbers are assigned at merge, not on a branch | Accepted | Not started |
| [0008](0008-event-sourced-persistence.md) | Event-sourced persistence in an embedded store, with a single writer | Accepted | Partial |
| [0009](0009-single-binary-with-embedded-web-ui.md) | A single binary with an embedded web UI | Accepted | Partial |
| [0010](0010-real-time-collaboration-with-block-locks.md) | Real-time collaboration starts with block-level locks, not a CRDT | Accepted | Partial |
| [0011](0011-http-api-route-table-and-access-classes.md) | The HTTP API is one route table, described by OpenAPI, and every route declares its access class | Accepted | Partial |
| [0012](0012-mcp-adapter-over-http-api.md) | An MCP server as an adapter over the HTTP API, acting as its caller | Accepted | Partial |
| [0013](0013-authentication-and-security-boundary.md) | Authentication is required by default, behind one principal boundary | Accepted | Partial |
| [0014](0014-closed-renderer-for-user-content.md) | User content is rendered by one closed renderer, never as raw HTML | Accepted | Partial |
| [0015](0015-resource-budgets-and-paged-listings.md) | One place names every resource budget, and a capped listing answers with a page | Accepted | Partial |
| [0016](0016-metrics-and-backup.md) | Operational metrics behind authentication, and an allowlisted backup | Accepted | Landed |
| [0017](0017-internationalisation-via-message-catalogue.md) | The UI is translated through a message catalogue, with English as source | Accepted | Partial |
| [0018](0018-pull-request-level-dco-sign-off.md) | The DCO sign-off may be given once per pull request | Accepted | Partial |
| [0019](0019-dco-enforcement-suspended.md) | DCO enforcement is suspended until the maintainer switches it on | Accepted | Landed |
| [0020](0020-embedding-and-containment-are-independent.md) | Embedding shows an item, containment places it — the two stay independent | Accepted | Partial |
| [0021](0021-deletion-only-when-unreferenced.md) | Deletion only when unreferenced, then trash, then purge | Accepted | Partial |
| [0022](0022-permission-model.md) | Space roles, narrowing restrictions, and a logged erasure role | Accepted | Partial |
| [0023](0023-import-and-sync-from-jira-and-confluence.md) | Import from Jira and Confluence through one engine — snapshot import and one-way sync, never two masters | Accepted | Partial |
| [0024](0024-release-targets-and-container-image.md) | One binary for every common platform, and a distroless container image | Accepted | Partial |
| [0025](0025-event-log-file-and-in-memory-state.md) | The event log is one file of checksummed batch records; state is folded in memory until it outgrows it | Accepted | Partial |
| [0026](0026-crypto-shredding-of-item-content.md) | Item content is encrypted with a key per item; purge and redaction destroy the key | Accepted | Partial |
| [0027](0027-local-accounts-sessions-and-api-tokens.md) | Local accounts with PBKDF2, in-memory sessions and scoped API tokens | Accepted | Partial |
| [0028](0028-item-api.md) | The item API exposes one command per request, through the permission model and one writer | Accepted | Partial |
| [0029](0029-persisted-security-audit-trail.md) | The security audit trail is its own append-only journal | Accepted | Partial |
| [0030](0030-openid-connect-single-sign-on.md) | Single sign-on through OpenID Connect, with go-oidc and just-in-time accounts without roles | Accepted | Partial |
| [0031](0031-web-ui-shell.md) | The first web UI is a buildless shell over the item API, tested in a real browser | Accepted | Partial |
| [0032](0032-prosemirror-rich-text-editor.md) | The rich-text editor is ProseMirror, vendored as one pre-built bundle | Accepted | Partial |
| [0033](0033-full-text-search-by-scanning-the-live-state.md) | Full-text search scans the live state; there is no search index yet | Accepted | Partial |
| [0034](0034-encrypted-backups-from-log-prefixes.md) | Backups are encrypted with a passphrase and cut from the logs while the server runs | Accepted | Landed |
| [0035](0035-comments-are-part-of-their-item.md) | Comments are part of their item: events on the item, sealed with its key | Accepted | Partial |
| [0036](0036-a-board-is-a-view-of-a-query.md) | A board is a view of a query, not a stored object | Accepted | Partial |
| [0037](0037-trusted-reverse-proxies.md) | Forwarded headers count only from trusted reverse proxies | Accepted | Landed |
| [0038](0038-container-starts-as-root-and-drops.md) | The container starts as root only to hand over the data directory, then drops to an unprivileged account | Accepted | Landed |
| [0039](0039-link-check-on-demand-public-addresses-only.md) | Web links are checked on demand, from public addresses only | Accepted | Landed |
| [0040](0040-governance-checks-and-automatic-restriction.md) | Content governance: sensitive data restricts its item on save; staleness and plausibility are checked on demand | Accepted | Landed |
| [0041](0041-page-tree-and-sibling-order.md) | The page tree: one level at a time, siblings in an order the move event carries | Accepted | Landed |
| [0042](0042-tasks-panels-tables-and-computed-blocks.md) | Tasks, panels, tables, tables of contents and lists of child pages as flat blocks | Accepted | Landed |
| [0043](0043-comparing-and-restoring-body-versions.md) | Earlier bodies are read from the log, compared in the browser and restored as a new commit | Accepted | Landed |
| [0044](0044-labels.md) | Labels are part of their item's content, set as a whole, counted per space | Accepted | Landed |
| [0045](0045-attachments-encrypted-files-beside-the-log.md) | Attachments are encrypted files beside the event log, described by events of their item | Accepted | Landed |
