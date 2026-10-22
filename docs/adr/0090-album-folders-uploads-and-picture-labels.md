# ADR-0090: Album folders, uploads and labels of pictures

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators. The maintainer asked for the following in the album (2026-10-22):
  - an upload button;
  - a delete button per picture;
  - folders;
  - labels on pictures that do not appear on kanbans.

  They chose:
  - folders as items of a space;
  - deleting from the album only pictures that no text uses;
  - the same labels as items, kept apart from them.

## Context and problem statement

The album (ADR-0054) is a view: it lists the images and drawings of every item one may read, and each picture belongs to its item. There was no place to put a picture that belongs to no page or task, no way to order pictures, and no way to remove one from the album.

How does the album hold pictures of its own, ordered in folders, without a second model of access, and how are pictures labelled without their labels reaching kanbans?

## Decision drivers

- **One model (D1):** a folder that holds content is an item; access, encryption, backup and erasure stay those of items (ADR-0022, ADR-0026, ADR-0045).
- **Nothing lost:** a picture shown in a text is not removed from the album behind the text's back.
- **Labels kept apart:** a picture's label narrows the album only. Kanbans (ADR-0060), label boards (ADR-0087) and the task list read item labels, and never see it.

## Considered options

1. **A folder is an item of the type `folder`; its pictures are its files. A picture's labels are a field of the file.**
2. **Folders as a grouping of their own, holding any picture.**
3. **A picture's labels as labels of its item.**

## Decision outcome

Chosen option: **1**.

### Folders

- **Type:** a folder is an item of the type `folder`, without a body, workflow or fields. The album defines the type when its first folder is made. If a type of that name exists in another shape, the album refuses to use it.
- **Placement:** folders are made in a space, or in a folder, by whoever may create there. They nest by the parent (D5) and are read and edited as any item of the space.
- **Pictures:** the pictures uploaded into a folder are its files.
- **API:**
  - `GET /api/album/folders` lists the readable folders of a space, or below a folder, by name;
  - `POST /api/album/folders` makes one;
  - `GET /api/album?folder=` lists one folder's pictures.

### Upload and delete

- **Upload:** *Upload* in an open folder sends the chosen pictures to the folder (PNG, JPEG, GIF, WebP), through the files of an item (ADR-0045).
- **Delete:** *Delete* is shown on a picture to whoever may edit its item, unless its item's text shows it. A picture in a text is deleted where the text is; the album says so. Deleting destroys the file, as it does from the item's page.
- The album tells for each picture whether it is used (`used`) and whether the reader may change it (`editable`).

### Labels of pictures

- **Storage:** a file has labels of its own, set with `AttachmentLabelled`, by whoever may edit its item. Labels are normalised as an item's (ADR-0044); a picture has at most 20.
- **Sealing:** they are content, sealed with the file's description.
- **Where they act:** they narrow the album (`label=`, and `q` searches them) and are counted by `GET /api/album/labels`. They are not the item's labels: `/api/labels`, kanbans, label boards and the task list never show them.
- **Suggestions:** while one labels a picture, the labels of pictures and of items are suggested alike.
- **Governance:** the sensitive-data check reads them, as it reads file names (ADR-0040).

### Format

Codec version 23 adds the field `labels` to an attachment and the kind `AttachmentLabelled`, sealed. Versions 1 to 22 are still read.

### Consequences

- **Positive:** the album holds pictures that belong to no page or task, in folders with the access of their space.
- **Positive:** pictures can be found by labels that stay off the boards.
- **Negative / trade-offs accepted:**
  - Folders are items, so they show in the space's page tree and in search as items do.
  - Their pictures show on the folder's own page as its files.
- **Negative / trade-offs accepted:** a folder cannot be renamed, moved or deleted from the album. That is done on its page, as for any item.
- **Negative / trade-offs accepted:** inserting a picture into a text copies it (ADR-0054) without its labels.
- **Negative / trade-offs accepted:** a picture shown only in an earlier version of a text counts as unused.

## Pros and cons of the options

### Option 2: folders of their own

Strongest form: any picture, of a page too, could be put into any folder, and a picture could sit in several folders.

- Bad: a folder would need rules of its own about who sees and fills it. It would either duplicate the item model, against D1, or show pictures to people who may not read their items, against D10.

### Option 3: labels of the item

Strongest form: no new field; one label vocabulary everywhere.

- Bad: every picture would label its whole item, and with it the item's cards on kanbans and label boards. That is exactly what the maintainer ruled out. A folder with pictures of different subjects could not label them apart.

## Links

- amends [ADR-0054](0054-an-album-of-readable-pictures-inserted-by-copy.md) (the album)
- relates to ADR-0022 (permissions), ADR-0040 (governance), ADR-0044 (labels), ADR-0045 (attachments), ADR-0060 and ADR-0087 (kanbans and label boards)
