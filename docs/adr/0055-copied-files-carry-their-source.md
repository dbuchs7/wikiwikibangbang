# ADR-0055: Copied files and new versions of drawings carry their source; a copy learns of a changed original

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for a mark of origin on pictures copied from the album, 2026-10-20)

## Context and problem statement

Inserting a picture from the album copies it into the target item (ADR-0054); editing a drawing saves a new file (ADR-0053). Copies diverge: when the original drawing is drawn again, its copies stay as they were, and nothing tells their readers. How does a copy know where it came from, and learn that its original changed, without weakening access per item (ADR-0022)?

## Decision drivers

- A copy stays a file of its own item, under its key and access.
- The source of a copy is told only to those who may read the source.
- A mark of origin cannot be made up: a "copy" with other content would mislead.
- Taking over a newer original is a deliberate step, not automatic.

## Considered options

1. **A source on the attachment:** `Attachment.Source = {Item, Attachment}`, sealed with the rest of the attachment; a new version of a drawing names the version it was drawn from in the same item, a copy names the file of the other item.
2. **Insert by reference** (ADR-0054, option 3): no copy, the body shows the other item's file.
3. **A separate provenance journal** outside the items.

## Decision outcome

Chosen option: **1**, codec version 12.

- **Uploading** takes `sourceItem` and `sourceAttachment`. The source must be a file the uploader may read; a copy from another item must have the source's content (same SHA-256) and the source must not be deleted. A version in the same item may differ; that is what a version is.
- **Versions** form a chain within their item. A deleted file keeps its source, so the chain holds when an older version is deleted.
- **Origin of a copy:** the listing of an item's files gives each copy `origin`: the source item's key and title, and `newer`, the newest version of the source file when it is newer than the one copied. A reader who may not read the source, or whose source item is gone, learns only that the file is a copy.
- **In the UI:** the side column marks copies ("copy of PLAT-5") and changed originals ("original changed"); while editing, the image block offers to take the changed original over, which copies the newer version (with its source) and points the block at it.
- A drawing edited in the copy's item stays a version of the copy and still leads back to the original.

### Consequences

- **Positive:** the source is sealed under the copy's item key and erased with it (ADR-0026); erasing the source item leaves copies as plain files.
- **Positive:** no file is shared between items; access stays per item.
- **Negative / trade-offs accepted:** taking over a changed original replaces the copy's picture; changes made to the copy since are kept only as the earlier file of the item.
- **Negative / trade-offs accepted:** the former copy keeps its mark "original changed" in the file list until it is deleted.
- **Negative / trade-offs accepted:** finding the newest version walks the versions of the source item's files, in memory, when an item's files are listed.

## Pros and cons of the options

### Option 2: insert by reference

Strongest form: no copy at all, so nothing to keep current.

- Bad: a body would show another item's file, readers of the page who may not read the source would see a gap, and the body format changes.

### Option 3: a provenance journal

Strongest form: no change to the item events.

- Bad: a second place to keep consistent with erasure and backups, and its entries would name items outside their encryption.

## Links

- relates to ADR-0022 (permissions), ADR-0026 (erasure), ADR-0045 (attachments), ADR-0053 (drawings), ADR-0054 (album)
