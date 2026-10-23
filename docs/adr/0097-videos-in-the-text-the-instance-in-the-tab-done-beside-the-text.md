# ADR-0097: Videos in the text, the instance's name in the tab, Done beside the text

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. The maintainer asked for three changes:
  - videos that play inside a text;
  - the browser tab named after the instance, not "wikiwikibangbang", with the name given in the administration;
  - the Done button in the same place as Comment, Protect and Edit.

## Context and problem statement

**Videos.** A video could be attached to an item as a file, but only downloaded. Pasting its address into the editor did nothing useful, and the body had no block that plays a video. The server also sent each file whole: Safari plays a video only when the server answers requests for parts of it (HTTP Range), and no browser can seek within a video without them. Files are stored encrypted in chunks (ADR-0045), so serving a part means decrypting from the middle of a file.

**Tab title.** The tab read "ACME · wikiwikibangbang" at best, and "wikiwikibangbang" until the design was loaded. With several instances open, the tabs did not tell them apart.

**Done.** After ADR-0091 and the change that moved Edit into the bar over the text, Edit stood beside Comment and Protect. Done, which ends editing, still stood in the item's header, away from where the text and its tools are.

## Decision drivers

- A video behaves like a picture in the text: placed, sized and captioned the same way (ADR-0045, ADR-0056, ADR-0059).
- Files stay encrypted at rest. Reading a part must authenticate what is read, as reading the whole file does.
- No new dependency, no player library: the browser's own video element.
- The body's model stays explicit (D1). A video is its own kind of block, not an image that happens to move.
- The name in the tab comes from the setting that exists (the design's name, ADR-0052), not from a new one.

## Considered options

1. **A block kind `video` that names a video attachment of the item, files served in parts, the design's name as the tab's title, Done in the editor's bar.**
2. **Videos as image blocks whose file happens to be a video.**
3. **Embedding external players (YouTube, Vimeo) by address.**

## Decision outcome

Chosen option: **1**.

- **The block:** `video` names an attachment of the item, with a plain caption, a width and an alignment, like `image`. It is valid only for a file whose bytes read as MP4 or WebM (`item.VideoTypes`), the formats browsers play without a plug-in. As for images, a new item gets its videos after it is created. Codec version 25.
- **Inserting:** "Insert… → Video" uploads the file as an attachment and places it in the text. The browser's own controls play it, in the editor and while reading.
- **Serving:** files are answered with `http.ServeContent`, so a request for a part (Range) gets that part (206), and ETag answers a repeated request.
  - Videos of these formats are sent inline, like safe images.
  - The stored file is read from any position: the chunk holding it is decrypted and authenticated, the last chunk as the last. A file changed, cut short or extended still fails where it is read.
  - Every file keeps `nosniff` and a sandboxing content security policy.
- **Elsewhere:**
  - The page printed as PDF shows the video's player (`media-src 'self'`).
  - The Word export names the video, `[Video: name]`, with its caption.
  - A copy of an item copies its videos with it.
  - Videos are not copied into exported archives.
- **The tab's title** is the design's name, now labelled "Name of the instance (shown in the browser tab)". The server writes it into the page, so the tab shows it before the design loads. Without a name, the tab keeps "wikiwikibangbang".
- **Done** stands at the right end of the editor's bar while editing, where Edit stands while reading. An item without a text keeps Done in its header, as it has no bar.

### Consequences

- **Positive:** a video plays and seeks in every current browser, Safari included, without leaving the page.
- **Positive:** several open instances are told apart by their tabs.
- **Negative / trade-offs accepted:**
  - A video counts against the attachment size limit (`budget`); long videos need a higher limit or another place.
  - An older build cannot read a log that has a video block (codec version 25).
- **Negative / trade-offs accepted:** formats other than MP4 and WebM (e.g. MOV from older cameras, AVI) are refused for a video block. They stay attachments to download.
- **Negative / trade-offs accepted:** whether an MP4 plays depends on its codec. H.264 plays everywhere; H.265 (HEVC) does not play in every browser. The server does not convert videos.

## Pros and cons of the options

### Option 2: videos as image blocks

Strongest form: no new block kind and no codec change.

- Bad: the model would say "image" for a video, and every reader of the model (export, governance, the album) would have to look at the file to know what it shows (D1).

### Option 3: external players

Strongest form: no storage for large files, and streaming done by others.

- Bad: the reader's visit is reported to a third party, a frame from elsewhere needs the content security policy opened (ADR-0014), and the video leaves the installation's control and its backups (ADR-0034).
- Not ruled out for later, as a separate decision with its own privacy review.

## Links

- amends [ADR-0045](0045-attachments-encrypted-files-beside-the-log.md) (files are read and served in parts), [ADR-0052](0052-corporate-design-in-a-journal-of-its-own.md) (the name is the tab's title) and [ADR-0091](0091-rules-every-view-follows.md) (Done beside the text)
- relates to ADR-0056 and ADR-0059 (sizing and placing pictures), ADR-0071 (export), ADR-0025 (codec versions)
