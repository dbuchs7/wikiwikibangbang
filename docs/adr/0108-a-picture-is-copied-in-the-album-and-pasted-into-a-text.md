# ADR-0108: A picture is copied in the album and pasted into a text

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked to copy a picture in the album with a click and paste it, as the original, into a page, 2026-10-23)

## Context and problem statement

A picture of the album reached a text only through "Insert… → From the album", a dialog that searches the album again (ADR-0054). Someone already looking at the picture in the album could not take it from there. The editor also did nothing with an image pasted from the clipboard.

How does a picture go from the album into a text the way people know from other programs, copy and paste, while staying the original file and keeping its source (ADR-0055)?

## Decision drivers

- Copy and paste as anywhere else: a button in the album, Ctrl+V (Cmd+V) in the text.
- **The original:** the full file in its own format, not a screenshot or a scaled preview, copied into the target item with its source, as from the album dialog.
- **Access (ADR-0022):** pasting copies only what the person may read, into what they may edit; the server checks both, as for every copy.
- It must work where the browser does not let a page write the clipboard (a plain-HTTP installation, an older browser).

## Considered options

1. **"Copy" writes a marker naming the picture, plus the picture as PNG, to the clipboard, and keeps the picture in the browser; pasting the marker copies the original with its source.**
2. **"Copy" puts only the image on the clipboard; pasting uploads it as a new file.**
3. **Drag and drop from the album into the editor.**

## Decision outcome

Chosen option: **1**.

- **Album:** every picture, older versions included (ADR-0103), has **Copy**. It writes three things to the system clipboard:
  - a small HTML marker that names the picture's item and file;
  - the file name as text;
  - the picture as PNG, for other programs.

  It also keeps the picture in this browser. The album says where to paste it.
- **Pasting into a text being edited:**
  - **The marker:** the picture is copied from its original file into the item, with its source (ADR-0055), and placed after the paragraph at the cursor. A picture that is already a file of the item is placed again, without a copy.
  - **Any other image** pasted from the clipboard, e.g. a screenshot (PNG, JPEG, GIF, WebP), is uploaded as a new file of the item and placed.
- **Insert… → Copied picture** places the picture last copied in this browser. This is the way where the clipboard could not be written. It says so when nothing was copied.
- **What the server checks:** the paste is an ordinary upload with a source. The server checks that the person may read the source and edit the target, and that the copy has the source's content. A marker that names a picture one may not read is refused.

### Consequences

- **Positive:** a picture goes from the album into a page in two steps, and stays the original with its provenance.
- **Positive:** screenshots and images from other programs can be pasted into a text.
- **Negative / trade-offs accepted:** writing the clipboard needs a secure page (HTTPS, or localhost) and the browser's permission. Without them only this browser keeps the copy, and Insert… → Copied picture places it.
- **Negative / trade-offs accepted:** other programs receive a PNG; a JPEG is converted for them. Inside the wiki the original file is used.

## Pros and cons of the options

### Option 2: only the image

Strongest form: the plainest clipboard, understood everywhere.

- Bad: the picture arrives as a new file without its source, re-encoded as PNG, so it is no longer the original and does not learn of a changed drawing (ADR-0055).

### Option 3: drag and drop

Strongest form: one gesture.

- Bad: the album and the page being edited are rarely on screen together. A drag between two tabs does not work in every browser.

## Links

- amends [ADR-0054](0054-an-album-of-readable-pictures-inserted-by-copy.md) (pictures are copied from the album itself)
- relates to ADR-0055 (sources of copies), ADR-0103 (versions as a stack), ADR-0022 (permissions), ADR-0032 (editor)
