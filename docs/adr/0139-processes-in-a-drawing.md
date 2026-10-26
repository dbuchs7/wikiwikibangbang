# ADR-0139: Processes in a drawing, live over its picture where they come from an engine

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested 2026-10-26: what a page offers for processes since ADR-0129, offered in a drawing too. A process can be drawn anew as a BPMN model, shown from an existing BPMN file, or taken from an atlas instance, with its moving operations view in the drawing); proposed for the maintainer's acceptance
- **Open question:** A process in a drawing is pictured by bpmn-js's own SVG export (`saveSVG`), which carries no bpmn.io watermark. Where bpmn-js draws a process live over a drawing, the watermark shows, as ADR-0129 requires. This record assumes that the license's condition binds diagrams rendered by bpmn-js, not pictures exported from it, as bpmn.io's own demo exports SVG without the watermark. No statement by its authors was found. Does the maintainer accept that reading?
- **Question checked:** 2026-10

## Context and problem statement

Since ADR-0129 a page shows BPMN processes in three ways:
- a model drawn in the modeler;
- a BPMN file;
- the newest version of a process of a process engine, with its operations: how many tokens passed, wait and are held by an incident on each element, refreshed while the page is in sight.

A drawing (ADR-0053, ADR-0126) is where a process is often explained in context: next to the systems it touches, the people involved, notes and arrows. A process there could only be redrawn by hand with the BPMN palette (ADR-0117), which shows neither the model nor its operations.

A drawing is a PNG attachment that carries its Excalidraw scene; the picture is its source. It is shown in pages as an image, and in exports and other clients as that image. Excalidraw runs on a page of its own with a looser content security policy, in a frame; the UI and it talk by messages.

So the question is how a process gets into a drawing:
- drawn anew;
- from a BPMN file;
- from an engine, its newest version and its operations kept current while the drawing is shown.

And all of that without changing what a drawing is: a picture that shows wherever pictures show.

## Decision drivers

- A drawing stays one PNG that carries its scene (ADR-0053). Nothing beside it has to be kept in step, and it can be copied into another page as it is (ADR-0055, ADR-0108).
- The item model does not change (D1, D4). A process in a drawing is part of the picture, not a block, a property or a relation.
- The UI's content security policy stays `default-src 'self'`. bpmn-js runs in the UI, not in the drawing's frame.
- The live view is what a process block shows (ADR-0129): the newest version, operations as counts only, where an administrator allows them, read through this server.
- Wherever the live view cannot be drawn, the picture still shows the process: in exports, in other clients, or when the engine does not answer.
- A picture is anyone's upload. Nothing read from it is believed unchecked.

## Considered options

1. **A picture in the scene, and for an engine's process a region in the PNG.** Each process is an image element whose `customData` says what it shows. A model drawn anew or taken from a file is kept in that `customData`. On saving, the drawing page lists where the engine processes lie in the picture, in a text chunk of the PNG. The page that shows the drawing lays a live bpmn-js view over each region.
2. **Excalidraw's embeddable elements, rendered live by the drawing editor.** Pages would show drawings with Excalidraw instead of as images.
3. **Process blocks next to the drawing:** no process inside the drawing; the drawing refers to blocks of the page.
4. **A BPMN file per process in a drawing**, kept as an attachment of the item, and named by the drawing.

## Decision outcome

Chosen option: **1**. Only it keeps a drawing one picture that shows everywhere and still shows an engine's process live.

### In the drawing editor

- **The Process chooser.** It sits beside the palettes (ADR-0117), top right, or above the bottom bar on a phone, and offers four entries:
  - *New BPMN model* opens the modeler of ADR-0129;
  - *BPMN file* lists the page's BPMN files and offers one from this device;
  - *Process from an engine* opens the process picker of ADR-0129. It also asks whether the operations show at first, where the engine allows them;
  - *Edit the selected process* draws a model again in the modeler, or reads an engine's process again in its newest version.
- **Who does what.** The drawing page asks the UI by message (`process`, with what it wants and, for an edit, what the selected element shows). The UI opens its dialogs over the drawing, draws the model with bpmn-js out of sight, and answers with an SVG picture, its size and what it shows.
- **The picture.** The SVG is bpmn-js's export on white. The drawing adds it as an image element, at most 640 wide, in the middle of what is in sight, or in place of the element edited, as wide as that was.
- **What an element shows** is in `customData.wikiwikibangbang`:
  - `{kind: "bpmn", name, xml}` is a model kept in the drawing;
  - `{kind: "process", engine, process, name, operations}` is a process of an engine.

  Both are checked by the rules of the process block (`drawprocesses.js`, `processOf`).
- **Models are kept in the drawing.** A model drawn anew or taken from a file is kept in the drawing, not as a file of the item. The drawing is its one source. It survives copying into another page, where an attachment ID would not (copies rename attachments, ADR-0067). A file taken into a drawing is a copy of it at that moment.

### The saved picture

- **What the picture shows.** The PNG is Excalidraw's export as before: the drawing with each process as it was pictured.
- **The regions.** If the drawing holds processes of engines, the drawing page also writes a `tEXt` chunk `wikiwikibangbang.processes` directly after the PNG header. It holds `{"v":1,"regions":[…]}`: for each engine process, its place in shares of the picture's width and height, and its engine, process, name and whether the operations show at first. The text is ASCII, at most 50 regions.
- **How a region is measured.** The places are measured as Excalidraw lays out the picture:
  - the scene's bounds by its `getCommonBounds` of the elements it measures (`rootsOf`), and the padding passed explicitly;
  - the picture's size as Excalidraw reports it (`getDimensions`, unchanged).

  A frame's name is drawn above the frame and can make the picture taller at the top. The top is therefore measured from the bottom.
- **Shown as drawn:** a turned or mirrored picture of a process, and one that would not fit the picture, get no region.
- **No regions, no chunk.** A drawing without engine processes is saved exactly as before.

### Where the drawing is shown

- **Reading the regions.** A drawing in a text reads its regions from the first 64 KiB of its picture (a `Range` request, cached like the picture). This applies to:
  - a picture the page knows as a drawing;
  - a picture whose name is not known there, as in an embedded text or one saved by someone else since.

  Each region is checked: a valid chunk checksum, shares within the picture, and engine and process IDs a process block accepts.
- **The live view.** Over each region, bpmn-js draws the newest version of the process, read through `GET /api/engines/{id}/processes/{process}` as a process block does.
  - **Until it is drawn,** the picture shows through. If it cannot be drawn, the picture stays and says *As saved*, with the reason on pointing.
  - **The operations** are counts only, as in a process block (ADR-0129). They show at first when the drawing says so and the engine allows them, and the reader switches them with the region's *Operations* button. While on, they are read every 30 seconds while the page is in sight.
  - **A newer version.** When the counts belong to a newer version than the one drawn, the newer version is drawn first.
- **The region does not pan or zoom.** The page scrolls over it, and clicks reach the picture, so while editing a click still selects the drawing. Its button and the bpmn.io watermark take clicks. The watermark stays visible.

### Consequences

- **Positive:**
  - A drawing explains a process in context, its model is the BPMN model, and a process of an engine shows its newest version and its operations live.
  - Drawings stay pictures. Exports, search previews, the album and other clients show the process as it was pictured at the last save.
  - No change to the item model, the server or the storage. Access, encryption and history are those of the drawing's attachment.
- **Negative / trade-offs accepted:**
  - A model taken from a file is a copy. Changing the file later does not change the drawing, and changing the model in the drawing does not change the file.
  - The live view lies over the drawing. Something drawn over a process in the drawing is covered while the process is shown live.
  - A turned or mirrored process is shown as drawn, not live.
  - Each picture whose name is not known where it is shown costs one ranged request of at most 64 KiB to learn it has no regions.
  - The picture of an engine's process ages. It shows the version saved; only where the drawing is shown in this UI is it current.
  - The model is stored twice in the PNG's scene, as XML and as an SVG picture, compressed with the scene.
- **Follow-ups / risks to watch:**
  - The regions rest on how Excalidraw 0.18 lays out an exported picture (`getCommonBounds`, padding, frame names above frames). An update of Excalidraw re-checks them. The e2e test draws a process live over a saved drawing.
  - The open question on the watermark, alongside ADR-0129's.
  - Refreshing the pictures of engine processes on every save is left out: saving would then wait for the engines.

## Pros and cons of the options

### Option 2: embeddable elements rendered by Excalidraw

Strongest form: Excalidraw's own mechanism for live content inside a drawing (`renderEmbeddable`). The process would be live in the editor too, at the place and zoom of the canvas, with no measuring of regions.

- Good: one rendering for editing and showing; no layout to re-measure.
- Bad: pages would have to show drawings with Excalidraw, which needs inline styles. Every page that shows a drawing would need the looser policy or a frame per drawing.
- Bad: exports and other clients get Excalidraw's placeholder for an embeddable, not the process. The picture would stop being the drawing.

### Option 3: process blocks next to the drawing

Strongest form: nothing new; a process block already shows a process live with its operations.

- Good: no new code.
- Bad: it is not what was asked. The process is not in the drawing, so the drawing cannot place it among the systems and people it explains.

### Option 4: a BPMN file per process, named by the drawing

Strongest form: one file per model, listed with the page's files, downloadable, the same model a process block can show; changing it shows in the drawing.

- Good: one model, wherever it is shown.
- Bad: the drawing would depend on files beside it, against ADR-0053. A copy of the page renames its files (ADR-0067), and a picture pasted into another page brings none (ADR-0108). The drawing would point at nothing there.
- Bad: the server would have to learn that a picture refers to files, to keep them from being deleted and to rename them on copy. That is a relation hidden in an attachment's content (D4).

## Links

- relates to ADR-0053 (drawings), ADR-0126 (Excalidraw 0.18), ADR-0117 (palettes), ADR-0129 (BPMN diagrams and processes of engines), ADR-0055 and ADR-0108 (copies of pictures), ADR-0067 (copying pages)
