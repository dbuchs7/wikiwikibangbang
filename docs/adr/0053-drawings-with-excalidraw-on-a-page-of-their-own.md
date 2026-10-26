# ADR-0053: Drawings are made with Excalidraw on a page of their own and stored as PNG attachments that carry their scene

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for a drawing editor from an open-source module, whose drawings can be inserted into pages and tasks, 2026-10-20)

## Context and problem statement

People want to sketch diagrams, flows and layouts in a page or a task. The body already shows image attachments (ADR-0045). Which editor, how is it embedded under the UI's strict content security policy (no inline styles), and in what form is a drawing kept so it can be edited again?

## Decision drivers

- Open source under a licence compatible with AGPL-3.0; nothing fetched from elsewhere (ADR-0024).
- The UI's policy stays `default-src 'self'` without exceptions.
- A drawing shows wherever an image shows: reading view, search previews, exports, other clients.
- Edited again later from what is stored, without a second file to keep in step.
- Access, encryption and history of drawings are those of their item.

## Considered options

1. **Excalidraw** (MIT), bundled, on a page of its own in a frame.
2. **draw.io / diagrams.net** (Apache-2.0), self-hosted embed.
3. **tldraw**: its licence since version 2 requires a licence key for production use; not open source in the sense required.
4. **An own small SVG editor.**

## Decision outcome

Chosen option: **1, Excalidraw 0.17.6** with React 18.3.1, built from `internal/web/vendor` like the editor bundle (ADR-0032): `static/vendor/excalidraw.js` and the files it loads at run time in `static/vendor/excalidraw-assets/` (fonts, a chunk of libraries, the German translation).

- **A page of its own:** `GET /draw` serves `draw.html` with its own policy: `style-src 'self' 'unsafe-inline'`, `img-src 'self' data: blob:`, `font-src 'self' data:`, `worker-src 'self' blob:`, `frame-ancestors 'self'`; scripts still come from this server only. Every other response keeps the strict policy and `frame-ancestors 'none'`. The page holds no session data in its DOM; it loads the drawing it is given.
- **In a dialog:** *Insert → Drawing* in the editor opens a dialog with the page in a frame; the two talk by `postMessage`, checked for the same origin and the frame's window.
- **Storage:** *Save drawing* exports a PNG that embeds the scene (Excalidraw's `exportEmbedScene`), uploaded as an attachment of the item named `….drawing.png`, and an image block points at it. The picture is the source: *Edit drawing* (while editing) loads the scene from it.
- **Editing again** uploads a new attachment and points the block at it. The former picture stays, as earlier versions of the body show it (ADR-0043).
- **Pages and tasks alike:** any item with a body and its attachments.
- The exported picture is always light, as it is shown on pages whatever the reader's scheme.

### Consequences

- **Positive:** drawings are ordinary images everywhere; permissions, encryption, backup and history come from attachments without new code on the server.
- **Positive:** the relaxed policy is confined to one page that renders no user markup.
- **Negative / trade-offs accepted:** the binary grows by about 4.5 MB (bundle 1.3 MB, library chunk 3 MB, fonts). The page loads only when a drawing is opened.
- **Negative / trade-offs accepted:** each saved edit adds an attachment; old versions can be deleted by hand.
- **Negative / trade-offs accepted:** the drawing marker is the file name; renaming is not offered, so it holds.
- **Negative / trade-offs accepted:** a drawing is not edited live by several people at once, unlike text; the last one saved wins.
- **Follow-ups / risks to watch:** Excalidraw 0.18 moved to ES modules and loads fonts differently; an update is a separate change. Features that need other origins (library browser, embedded web pages) do not work under the policy, which is intended.

## Pros and cons of the options

### Option 2: draw.io

Strongest form: the most complete diagram editor (shapes for networks, BPMN, UML), and widely known in organisations.

- Good: rich shape libraries; XML embedded in PNG/SVG as well.
- Bad: the self-hosted application is tens of megabytes and expects `unsafe-eval` in places; much larger than the need.

### Option 4: an own SVG editor

Strongest form: no dependency, exactly the policy we want.

- Bad: a usable drawing editor (selection, snapping, arrows that follow shapes, undo, text) is a project of its own.

## Links

- relates to ADR-0024 (offline), ADR-0032 (vendored editor), ADR-0043 (versions), ADR-0045 (attachments)
- extended by [ADR-0116](0116-every-view-fits-a-phone.md) (phones), [ADR-0117](0117-palettes-for-the-drawing-editor.md) (palettes) and [ADR-0139](0139-processes-in-a-drawing.md) (processes in a drawing)
- amended by [ADR-0126](0126-excalidraw-0-18.md) (Excalidraw 0.18 and its bundle)
