# Invariants

These follow from [ADR-0001](../adr/0001-unified-item-data-model.md) and the records named with each, all accepted on 2026-10-17. They are settled: a change that would break one needs a new or superseding ADR first, never a workaround. Each names the checks that hold it; a check that is missing is listed as such.

| | Invariant | Checked by |
|---|---|---|
| **D1** | **One model for content.** User-generated content is exclusively items; there is no parallel domain object (`Page`, `Issue`, …) for one content type. System objects (users, groups, spaces, type definitions) are exempt. | Review; `internal/item` has one `Item` type and types are data (`TestScenarioPageWithEmbeddedTask`). |
| **D2** | **Stable identity.** Relations, document references and events use the immutable `id`, never the `key`. | `internal/item` (`Move` re-keys without breaking references); the API resolves keys to IDs (`resolveItem`). |
| **D3** | **Validated changes.** Properties change only through schema-validated commands; `state` only through workflow transitions — except an import (ADR-0023), which sets a valid state directly. | `TestValueKinds`, transition tests in `internal/item`; `Upsert` validates states (`import_test.go`). |
| **D4** | **No shadow data in the document.** Anything queryable is a property; relations derived from a body are a function of the body version. | `Doc.refs`; derived-relation tests in `internal/item`; replay tests compare state. |
| **D5** | **One containment parent.** At most one `parent`; permissions inherit along containment only. | `Move` refuses cycles; permission tests (`permissions_test.go`). |
| **D6** | **Type-agnostic capabilities.** No type-specific code in search, permissions, history, comments or notifications. | Review. *No automated check.* |
| **D7** | **Identities are permanent.** An ID always resolves — to the item, the trashed item or its tombstone. ([ADR-0021](../adr/0021-deletion-only-when-unreferenced.md)) | Lifecycle tests (tombstones resolve by ID and key). |
| **D8** | **No deletion while referenced.** An item is trashed only when no live item outside its subtree references it; redaction is the logged exception. ([ADR-0021](../adr/0021-deletion-only-when-unreferenced.md), [ADR-0022](../adr/0022-permission-model.md)) | Reference-gate tests in `lifecycle_test.go`; API returns 409 (`TestItemLifecycleThroughTheAPI`). |
| **D9** | **Access only narrows below the space.** Readable (editable) only if the space role allows it and no restriction on the item or its ancestors excludes the principal; restrictions never grant. ([ADR-0022](../adr/0022-permission-model.md)) | `permissions_test.go`; `TestPermissionsThroughTheAPI`. |
| **D10** | **Unreadable is non-existent; only sessions face callers.** Nothing a caller receives reveals an item it may not read; API code acts through `Store.As(principal)`. ([ADR-0022](../adr/0022-permission-model.md), [ADR-0028](../adr/0028-item-api.md)) | `TestEveryCommandIsChecked`, `TestHistoryDoesNotLeakUnreadableItems`, `TestChangeFeedNeverRevealsAnUnreadableItem`; the API answers 404 for unreadable items (`TestPermissionsThroughTheAPI`). |

Beyond the item model, these hold the system together and are checked the same way:

| Rule | Record | Checked by |
|---|---|---|
| A change is durable before it is visible; a failed write changes nothing. | ADR-0008, ADR-0025 | `TestAChangeThatIsNotPersistedDoesNotHappen`, `TestEveryCrashPointRecovers` |
| Live state equals the state rebuilt from disk. | ADR-0025 | the round-trip journal in every `internal/item` test; golden files |
| Content never reaches disk in plaintext; erasure destroys the key. | ADR-0026 | `TestContentIsNeverWrittenInPlaintext`, `TestErasureDestroysTheKey` |
| Every route is authenticated unless declared public. | ADR-0011, ADR-0027 | `TestEveryAPIRouteButLoginNeedsAPrincipal`, `TestUndeclaredAccessIsGated` |
| User content is never interpreted as markup or code. | ADR-0014, ADR-0031 | `TestNoMarkupFromStrings`; `e2e/smoke.cjs` |
| The product is one binary, without CGO, for every release platform. | ADR-0004, ADR-0024 | CI `cross-build`; `TestCrossBuildCoversReleasePlatforms` |
