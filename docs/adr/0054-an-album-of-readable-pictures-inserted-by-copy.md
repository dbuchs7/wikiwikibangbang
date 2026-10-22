# ADR-0054: The album lists the pictures of every readable item; inserting one copies it into the target item

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked whether a central album of all graphics makes sense, and chose the album with a copy on insert, 2026-10-20)

## Context and problem statement

Images and drawings (ADR-0053) are attachments of one item (ADR-0045), encrypted with that item's key, and a body may show only its own item's files. People want to find a picture once made and use it in another page or task. Where are pictures found, and how does one picture get into another item without weakening access?

## Decision drivers

- Access stays per item (ADR-0022): a picture of a restricted item must not become visible through another.
- No new kind of stored object and no change to the persisted format.
- Simple to understand: what one sees in the album is what one could open item by item.

## Considered options

1. **An album as a view, insert by copy:** list the image attachments of every live item the caller may read; inserting uploads a copy to the target item.
2. **A central store of pictures** with access of its own, referenced from bodies.
3. **An album as a view, insert by reference:** a block that shows another item's file, to readers who may read that item.

## Decision outcome

Chosen option: **1**.

- **Listing:** `GET /api/album` lists images (PNG, JPEG, GIF, WebP) of live items the caller may read, newest first, narrowed by space, text (file name, item key or title) and kind (image or drawing), in pages. Other files are not listed.
- **The album page** (module *Album*) shows them as a grid; each leads to its item.
- **Inserting:** while editing, *Insert → From the album* opens the album in a dialog. The chosen picture is read with the reader's rights and uploaded to the edited item with the writer's rights, under its name, so a drawing stays editable as a drawing. A picture of the edited item itself is not copied but shown again.
- No server code writes across items; the copy uses the existing download and upload.

### Consequences

- **Positive:** access, encryption, backup and erasure stay exactly those of attachments; erasing the source item (ADR-0026) does not reach copies elsewhere, and copies are erased with their own item.
- **Positive:** nothing new is persisted.
- **Negative / trade-offs accepted:** copies diverge; changing the original does not change its copies. A plan used in many pages is updated page by page.
- **Negative / trade-offs accepted:** a copy takes on the target's access. Someone who may read a restricted picture can put it in an open page; they could already download and upload it, so no right is added, but the album makes it easier. The sensitive-content check (ADR-0040) reads text, not pictures.
- **Negative / trade-offs accepted:** the listing scans every item in memory, like search (ADR-0033).
- **Follow-ups / risks to watch:** insert by reference (option 3) if living plans across pages are needed. Copies carry their source since ADR-0055.

## Pros and cons of the options

### Option 2: a central store

Strongest form: one picture, one place; a change reaches every page at once, and the store can be curated.

- Bad: needs access rules of its own, which either duplicate the item model or ignore it; a picture from a restricted page would need a second set of rules to stay restricted.

### Option 3: insert by reference

Strongest form: a living picture shared by many pages without copies.

- Bad: a block would show content of another item, so a reader of the page who may not read the source sees a gap, and the body format changes (`CodecVersion`). It remains open as a follow-up.

## Links

- amended by [ADR-0090](0090-album-folders-uploads-and-picture-labels.md) (folders, uploads, deleting and labels of pictures)
- relates to ADR-0022 (permissions), ADR-0026 (erasure), ADR-0033 (search), ADR-0045 (attachments), ADR-0053 (drawings)
