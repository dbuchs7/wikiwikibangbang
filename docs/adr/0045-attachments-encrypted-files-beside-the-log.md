# ADR-0045: Attachments are encrypted files beside the event log, described by events of their item

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators; the maintainer accepted the proposal on 2026-10-19

## Context and problem statement

Pages need files: diagrams and screenshots in the text, documents to download. Without them most wiki pages cannot move to the product, and an import stays incomplete (ADR-0023).

The event log is held in memory as a whole and replayed on every start (ADR-0025); files of megabytes cannot live in it. Whatever holds them must keep the guarantees the log gives: content readable exactly by those who may read its item (ADR-0022), made unrecoverable by a purge or a redaction (ADR-0026), included in backups (ADR-0034), and never run as code in the browser (ADR-0014).

## Decision drivers

- The log stays small: it describes files, it does not hold them.
- Erasure by construction: an item's files must become unreadable with the item, through the same key, without finding every copy.
- A file uploaded by anyone may be hostile: an HTML page, an SVG with a script, a file that claims to be an image.
- Backups stay consistent and restorable without stopping the server.
- An upload must not exhaust memory or disk at once.

## Considered options

1. **Encrypted files in the data directory, one per attachment, under the item's key; events describe them**
2. **Files in the event log**, sealed like other content
3. **An object store** (S3-compatible) outside the data directory

## Decision outcome

Chosen option: **1**.

- **Storage:** `attachments/<item>/<attachment>` in the data directory. The content is encrypted with AES-256-GCM under the item's key (ADR-0026), in chunks of 64 KiB, each with a nonce of a random prefix and its number and with the item, the attachment and whether it is the last chunk in the associated data: a file changed, cut at a chunk boundary, extended, reordered or moved to another attachment does not decrypt. An upload is streamed to a temporary file, synced and renamed; its size, SHA-256 and media type (detected from the content, never taken from the client) are computed on the way.
- **Model:** `AttachmentAdded` records an attachment's ID, name, media type, size and hash, sealed as content; author and time are the event's. `AttachmentDeleted` marks it deleted and removes its file once the event is durable. A purge or a redaction removes the item's directory, and its key is destroyed as before. Files no attachment needs (an upload that was never recorded, a removal a crash interrupted) are removed when the data directory opens. Names are 1 to 255 characters, without slashes or control characters, and the governance check reads them (ADR-0040).
- **Images in the text:** the block kind `image` names an attachment of the same item in `Ref` and holds a plain caption. A body may only show an attachment of its item that is not deleted; one deleted since may stay where the current body already shows it, so the page stays editable.
- **Rights:** reading the item is reading its files; adding and deleting needs the right to edit it.
- **Serving:** `GET /api/items/{ref}/attachments/{id}`. PNG, JPEG, GIF and WebP are sent as themselves and shown inline; everything else, SVG included, as `application/octet-stream` with `Content-Disposition: attachment`. Every answer forbids sniffing (`X-Content-Type-Options: nosniff`) and sandboxes the response (`Content-Security-Policy: default-src 'none'; sandbox`), so a file never runs as a page of this site.
- **Uploading:** `POST /api/items/{ref}/attachments?name=` with the file as the body. The body must be `application/octet-stream`, which a cross-site form cannot send, as `application/json` protects the other routes (ADR-0027). The budget `AttachmentBytes` (25 MiB by default, ADR-0015) bounds one file; the server reads no more.
- **Backups:** a backup holds the files of the attachments the captured log describes, opened only when written, so a large wiki does not open every file at once. A file deleted or purged after the capture is left out: its content was meant to go. A restore accepts only paths of this form.
- **Format:** `CodecVersion` 8 adds the event kinds, the field `attachment` and the block kind `image`. Versions 1 to 7 are still read.
- **UI:** every item lists its files with size, author and time, uploads several at once, and deletes one after a confirmation, since the content is destroyed. The editor's *Insert* menu adds an image: it is uploaded as an attachment and shown with its caption.

### Consequences

- Good: the log grows by a few hundred bytes per file; erasure, permissions and backups extend to files without new mechanisms.
- Good: a hostile file is stored and served, never run.
- Bad: the content of files is not searched and not checked by governance; only their names are.
- Bad: a deleted file is gone; there is no trash for attachments, hence the confirmation.
- Bad: no total quota per space or instance yet; the budget bounds one file.
- Neutral: the import does not bring the source's attachments yet.

## Pros and cons of the options

### 2. Files in the event log

- Good: one mechanism; backups and erasure unchanged.
- Bad: the log is in memory and replayed on every start: a wiki with a gigabyte of files would need a gigabyte of memory and replay it each time.

### 3. An object store

- Good: scales beyond one disk.
- Bad: a second system to run, secure and back up consistently with the log; erasure depends on the store's deletion. It can come later behind the same interface.

## Links

- amended by [ADR-0097](0097-videos-in-the-text-the-instance-in-the-tab-done-beside-the-text.md) (files are read and served in parts)
- ADR-0014 (no user content runs), ADR-0015 (budgets), ADR-0022 (permissions), ADR-0023 (import), ADR-0025 (the log), ADR-0026 (erasure), ADR-0027 (CSRF), ADR-0034 (backups), ADR-0040 (governance), ADR-0042 (block kinds)
