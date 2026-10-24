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
| [0035](0035-comments-are-part-of-their-item.md) | Comments are part of their item: events on the item, sealed with its key | Accepted | Landed |
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
| [0046](0046-watching-and-notifications-from-the-log.md) | Watching items and spaces; notifications are read from the event log | Accepted | Partial |
| [0047](0047-inline-comments-anchored-by-quoted-text.md) | Inline comments are anchored by the text they quote; threads take replies and are resolved | Accepted | Landed |
| [0048](0048-roles-are-listed-and-taken-back.md) | Roles, group memberships and system roles are listed and taken back | Accepted | Landed |
| [0049](0049-a-task-inbox-after-atlas.md) | A task inbox built on the item model, after atlas's task list; no atlas code taken over | Accepted | Partial |
| [0050](0050-profile-pictures-in-the-accounts-journal.md) | Profile pictures are kept in the accounts journal; the header has an account menu | Accepted | Landed |
| [0051](0051-pages-open-for-reading-and-are-edited-in-place.md) | Pages open for reading and are edited in place; what belongs to a page sits in a column beside it | Accepted | Landed |
| [0052](0052-corporate-design-in-a-journal-of-its-own.md) | The corporate design is kept in a journal of its own and applied as a stylesheet over the product's | Accepted | Landed |
| [0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) | Drawings are made with Excalidraw on a page of their own and stored as PNG attachments that carry their scene | Accepted | Landed |
| [0054](0054-an-album-of-readable-pictures-inserted-by-copy.md) | The album lists the pictures of every readable item; inserting one copies it into the target item | Accepted | Landed |
| [0055](0055-copied-files-carry-their-source.md) | Copied files and new versions of drawings carry their source; a copy learns of a changed original | Accepted | Landed |
| [0056](0056-images-have-a-width-in-percent-of-the-column.md) | Images have a width in percent of the text column | Accepted | Landed |
| [0057](0057-governance-findings-in-the-bell.md) | Governance findings reach the bell: restrictions as notifications, open findings as a reminder | Accepted | Landed |
| [0058](0058-data-protection-review-holds-open-items-as-drafts.md) | A data protection review holds open items as drafts until a second person releases them | Accepted | Landed |
| [0059](0059-alignment-dividers-and-more-marks.md) | Text and pictures are aligned, bodies get dividers and three more marks, and the toolbar shows what applies | Accepted | Landed |
| [0060](0060-kanbans-are-topics-whose-cards-carry-their-label.md) | Kanbans are named topics of a space whose cards carry the kanban's label | Accepted | Landed |
| [0061](0061-account-requests-and-message-channels-on-accounts.md) | Account requests at the sign-in page and message channels on accounts | Accepted | Landed |
| [0062](0062-spaces-carry-an-icon-and-show-as-tiles-or-a-list.md) | Spaces carry an icon and show as tiles or a list | Accepted | Landed |
| [0063](0063-a-comment-overview-across-items.md) | A comment overview across items | Accepted | Landed |
| [0064](0064-message-delivery-and-password-reset.md) | Message delivery over the account's channels, and the password reset by mail | Accepted | Landed |
| [0065](0065-a-uniform-airy-ui-system.md) | A uniform, airy UI system | Accepted | Landed |
| [0066](0066-several-identity-providers-entra-id-and-google.md) | Several identity providers side by side: Microsoft Entra ID and Google | Accepted | Landed |
| [0067](0067-copying-pages-with-their-child-pages.md) | Copying pages with their child pages, and moving them by a form | Accepted | Landed |
| [0068](0068-oauth-for-ai-assistants.md) | OAuth for AI assistants on the MCP endpoint | Accepted | Landed |
| [0069](0069-deleting-a-space-in-two-steps.md) | Deleting a space in two steps | Accepted | Landed |
| [0070](0070-an-own-stylesheet-without-a-dark-look-turns-the-dark-scheme-off.md) | An own stylesheet without a dark look turns the dark scheme off | Accepted | Landed |
| [0071](0071-exporting-pages-and-lists.md) | Exporting pages as PDF and Word, tables and lists as CSV | Accepted | Landed |
| [0072](0072-search-hits-rated-with-stars.md) | Search hits rated with up to five stars | Accepted | Landed |
| [0073](0073-automatic-full-and-incremental-backups.md) | Automatic full and incremental backups to a destination | Accepted | Landed |
| [0074](0074-catalogs-kept-in-step-with-other-systems.md) | Catalogs kept in step with other systems | Accepted | Landed |
| [0075](0075-mirroring-the-interfaces-of-opendata-swiss.md) | Mirroring the interfaces of opendata.swiss into a catalog | Accepted | Landed |
| [0076](0076-kanban-columns-card-fields-and-free-moves.md) | A kanban keeps its own columns, card fields and card order; moves stay workflow transitions | Accepted | Landed |
| [0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md) | Connectors keep catalog spaces in step with their sources | Accepted | Landed |
| [0078](0078-protecting-items-for-people-and-groups.md) | Protecting an item for people and groups from its page | Accepted | Landed |
| [0079](0079-owner-and-details-lead-the-main-column.md) | Owner and details lead the main column of an item's page | Superseded by ADR-0102 | Superseded |
| [0080](0080-one-page-per-data-set-written-from-the-source.md) | One catalog page per data set, written from everything the source says | Accepted | Landed |
| [0081](0081-the-server-stays-fast-with-tens-of-thousands-of-items.md) | The server stays fast with tens of thousands of items | Accepted | Landed |
| [0082](0082-a-state-no-transition-leaves-is-finished.md) | A state no transition leaves is finished | Accepted | Landed |
| [0083](0083-search-finds-any-word-and-names-the-words-of-each-hit.md) | Search finds any word and names the words of each hit | Accepted | Landed |
| [0084](0084-a-mention-in-a-comment-is-a-notification.md) | A mention in a comment is a notification | Accepted | Landed |
| [0085](0085-ready-made-themes-under-the-corporate-design.md) | Ready-made themes under the corporate design | Accepted | Landed |
| [0086](0086-a-space-is-renamed-its-key-stays.md) | A space is renamed; its key stays | Accepted | Landed |
| [0087](0087-kanban-pictures-and-boards-by-label.md) | Kanban pictures, and a board for every label | Accepted | Landed |
| [0088](0088-protected-passages-of-a-body.md) | Protected passages of a body | Accepted | Landed |
| [0089](0089-a-protected-passage-is-erased-by-its-key.md) | A protected passage is erased by destroying its key | Accepted | Landed |
| [0090](0090-album-folders-uploads-and-picture-labels.md) | Album folders, uploads and labels of pictures | Accepted | Landed |
| [0091](0091-rules-every-view-follows.md) | Rules every view follows | Accepted | Landed |
| [0092](0092-labels-in-search-and-task-counts-on-label-boards.md) | Labels in search, and task counts on the boards by label | Accepted | Landed |
| [0093](0093-the-interface-catalog-is-ordinary-pages.md) | The interface catalog is ordinary pages | Accepted | Landed |
| [0094](0094-a-type-nothing-uses-can-be-removed.md) | A type nothing uses can be removed | Accepted | Landed |
| [0095](0095-language-and-look-in-the-header-and-an-own-start-picture.md) | Language and look in the header, an own start picture, an empty sheet to write on | Accepted | Landed |
| [0096](0096-links-mentions-and-embeds-as-in-a-word-processor.md) | Links, mentions and embeds as in a word processor | Accepted | Landed |
| [0097](0097-videos-in-the-text-the-instance-in-the-tab-done-beside-the-text.md) | Videos in the text, the instance's name in the tab, Done beside the text | Accepted | Landed |
| [0098](0098-every-list-can-be-filtered.md) | Every list can be filtered | Accepted | Landed |
| [0099](0099-the-layout-is-left-aligned.md) | The layout is left-aligned | Accepted | Landed |
| [0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) | A tidy top bar on every width, and columns that fold away | Accepted | Landed |
| [0101](0101-a-reading-mode.md) | A reading mode | Accepted | Landed |
| [0102](0102-owner-and-details-beside-the-text-sign-in-centred.md) | Owner and details beside the text; the sign-in card centred | Accepted | Landed |
| [0103](0103-the-versions-of-a-file-form-one-stack-in-the-album.md) | The versions of a file form one stack in the album | Accepted | Landed |
| [0104](0104-rules-for-the-older-versions-of-pictures.md) | Rules for the older versions of pictures | Accepted | Landed |
| [0105](0105-the-automatic-clean-up-runs-at-a-fixed-hour.md) | The automatic clean-up of the album runs at a fixed hour | Accepted | Landed |
| [0106](0106-an-embedded-item-is-shown-whole.md) | An embedded item is shown whole | Accepted | Landed |
| [0107](0107-personal-notes-seen-by-their-maker-alone.md) | Personal notes, seen by their maker alone | Accepted | Landed |
| [0108](0108-a-picture-is-copied-in-the-album-and-pasted-into-a-text.md) | A picture is copied in the album and pasted into a text | Accepted | Landed |
| [0109](0109-a-design-logo-and-start-picture-for-the-dark-look-and-the-product-icon-in-the-tab.md) | A design logo and start picture for the dark look, and the product's icon in the browser tab | Accepted | Landed |
| [0110](0110-the-kanbans-page-in-three-columns.md) | The kanbans page in three columns | Accepted | Landed |
| [0111](0111-protecting-removing-and-copying-respect-the-whole-tree.md) | Protecting, removing and copying respect the whole page tree | Accepted | Landed |
| [0112](0112-the-bell-is-the-last-of-a-heads-buttons.md) | The bell is the last of a head's buttons, and the tree makes no pages | Accepted | Landed |
| [0113](0113-ones-notes-from-the-start-page.md) | One's notes from the start page | Accepted | Landed |
| [0114](0114-tasks-in-a-text.md) | Tasks in a text: made below the page, shown as a link or whole, listed by label | Accepted | Landed |
| [0115](0115-who-edits-shows-as-faces-and-readers-follow-the-text.md) | Who edits shows as faces, and readers follow the text | Accepted | Landed |
| [0116](0116-every-view-fits-a-phone.md) | Every view fits a phone | Accepted | Landed |
| [0117](0117-palettes-for-the-drawing-editor.md) | Palettes for the drawing editor from the Excalidraw library collection | Accepted | Landed |
