# ADR-0129: BPMN diagrams in the text, and processes from a process engine with their operations

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested 2026-10-26: a viewer for BPMN processes built on bpmn.io, as the atlas project uses; BPMN files stored with a page and new ones drawn; processes of an atlas instance shown in their newest version; and their operations view switched on. The requester chose a credential held by the server, counts only in the operations view, and a narrow token scope on the atlas side); proposed for the maintainer's acceptance
- **Open question:** bpmn-js is under the bpmn.io license, the MIT terms plus the condition that the bpmn.io watermark stays visible and unchanged in every rendered diagram. This record assumes that condition is a preserved author attribution as AGPL-3.0 section 7(b) allows; does the maintainer accept that reading, or should it be checked by counsel?
- **Question checked:** 2026-10

## Context and problem statement

Organisations describe their work as BPMN 2.0 processes. Pages that explain a process show it today as a picture: a screenshot, or a drawing made with the BPMN palette of the drawing editor (ADR-0117). Neither is a BPMN model: it cannot be opened in a modeler, it is not the file the process engine runs, and it falls behind the model as soon as the model changes.

Three needs follow:

1. **Files.** A BPMN file kept with a page, shown as a diagram that can be panned and zoomed, and new diagrams drawn and changed in the wiki.
2. **Processes from an engine.** A process deployed to an atlas instance (github.com/pblumer/atlas, a BPMN engine) shown in a page in its *newest* version, without anyone copying it after each deployment.
3. **Operations.** On such a process, where its instances are now: how many tokens wait on each element, and where incidents hold them.

## Decision drivers

- The UI's content security policy stays `default-src 'self'` (ADR-0031, ADR-0053); nothing is loaded from elsewhere (ADR-0024).
- Open source under a licence compatible with AGPL-3.0, vendored like the editor (ADR-0032).
- The body's model stays explicit (D1, D4): a diagram is a block of its own kind, not an image that happens to be XML.
- Access, encryption and history of a stored diagram are those of its item, as for drawings (ADR-0053).
- An engine's credential never reaches a browser, the log or the settings journal; it comes from the environment, like a connector's (ADR-0077).
- The engine decides which processes the installation may see; the wiki shows no more of its runtime than it needs.
- Every new bound is a budget (ADR-0015).

## Considered options

For drawing diagrams:

1. **bpmn-js in the UI itself, under the UI's policy.**
2. **bpmn-js on a page of its own in a frame, with a looser policy**, as the drawing editor (ADR-0053).
3. **Pictures rendered when saved**, as drawings are: a PNG that carries the BPMN XML.

For processes from an engine:

4. **The server reads the engine with a credential from the environment**; administrators name the engines.
5. **The browser reads the engine directly**, with the reader's own session there.
6. **A connector copies processes into items** on a schedule (ADR-0077).
7. **Each person connects their own engine account** by OAuth, and the server reads with that person's token.

## Decision outcome

Chosen options: **1 and 4.**

### Drawing diagrams (option 1)

- **The library:** bpmn-js 18.31.0, the modeler and viewer of bpmn.io, bundled from `internal/web/vendor` like the editor (ADR-0032) into `static/vendor/bpmn-viewer.js` (the navigated viewer, about 200 KB) and `static/vendor/bpmn-modeler.js` (the modeler, about 600 KB), with its stylesheets and its icon font in `static/vendor/bpmn/`. A page loads the viewer only when it shows a diagram, and the modeler only when one is drawn.
- **No exception to the policy:** bpmn-js draws with SVG attributes and sets styles through the DOM, which the policy allows; a test in Chromium under the UI's own policy recorded no violation, the watermark included.
- **The watermark** stays as bpmn-js draws it, at the bottom right of every diagram, and nothing of the UI covers it. `bpmn.LICENSE.txt` carries the licence text.

### Stored diagrams: the block kind `bpmn`

- A `bpmn` block names an attachment of its item that holds BPMN 2.0 XML, with a plain caption. It is valid only for a file whose bytes read as text (`item.BPMNTypes`); the viewer says so when the text is no BPMN model. As for images, a new item gets its diagrams after it is created.
- **Insert → BPMN diagram** offers *New diagram*, which opens the modeler with a start event, and *Upload file* for a `.bpmn` file. A drawn diagram is saved as `….bpmn`.
- **Edit diagram** (while editing) opens the modeler on the file; saving uploads a new version of it (ADR-0103) and points the block at it. The former file stays, as earlier versions of the body show it (ADR-0043).
- **Reading:** the diagram is drawn in the text, panned by dragging and zoomed with the wheel or by pinching; *Fit* shows it whole, and *Download* sends the file. Diagrams are drawn light in either scheme, as drawings are.
- **Copies and restores** carry the file with the block, as for images.

### Processes from an engine (option 4): the block kind `process`

- **Engines:** an administrator names each engine under *Administration → Process engines*: an ID that never changes, a name, the kind (`atlas`, the only one so far), the base address, and whether its operations may be shown. They are settings of the installation, kept in `site.wal`.
- **The credential** is the environment variable `WWBB_ENGINE_<ID>_TOKEN` (the ID in capitals, other characters as `_`, as for connectors), sent as a bearer token. It is never written to a journal, logged or returned; the settings page says only whether it is set. On atlas, a token of the scope `diagrams` reaches the three reads below and nothing else; that scope is proposed in pblumer/atlas as ADR-draft-diagrams-scope. Until it is there, a `full` token works, with far more reach than needed.
- **A `process` block** names an engine (`Engine`) and a process by its BPMN process ID (`Process`), with a plain caption. It holds no copy of the model.
- **Insert → Process from an engine** lists the engines, then their processes (name, ID, newest version).
- **Reading** a process block asks the server, which reads the engine at that moment:
  - `GET /api/engines` lists the engines by ID, name and whether operations may be shown;
  - `GET /api/engines/{id}/processes` lists the newest version of each process the credential may read;
  - `GET /api/engines/{id}/processes/{process}` answers the newest version's BPMN XML, its version, key, name and when it was deployed, as JSON, so the XML is never a document of this site.
  - Every signed-in person and token reaches the three reads. Atlas lets every signed-in caller read every deployed definition (its ADR-0447 §5), so the credential's account does not narrow which processes are seen: every process of the engine is visible to every signed-in person of the wiki.
- **Toward the engine:** fixed paths under the base address (`/api/v1/processes`, `/api/v1/processes/{key}/xml`, `/api/v1/processes/{key}/runtime`), a timeout of 15 seconds, no redirects followed, and at most `EngineResponseBytes` (8 MiB) read from an answer. As for connectors (ADR-0127), every request is checked before it is sent, redirects excluded as they are not followed: an engine must be on the public internet unless the operator sets `WWBB_ENGINE_ALLOW_PRIVATE=on`, which an engine in the organisation's own network needs. It is the operator's decision, not a setting of the UI.
- **Errors** say which engine failed and how (not configured, no credential, unreachable, refused, no such process), never the credential.

### Operations

- `GET /api/engines/{id}/processes/{process}/operations` answers, for the newest version, how many instances are running and have finished, and per element how many tokens wait there, how many are held by an incident and how many have passed. Nothing else is passed on: no incident messages, no instance keys, no variables. It answers only when the administrator allowed operations for that engine. Atlas sends a page of incident details with the counts; the server drops them before answering.
- **The reader switches the operations view on** at the diagram; it is not stored in the block. Badges then mark the elements: tokens in green, incidents in red, passes in grey. The view refreshes every 30 seconds while it is on and the page is in sight, and says which version it counts.
- **Codec version 29** adds the block kinds `bpmn` and `process` and the block fields `Engine` and `Process`. Versions 1 to 28 are still read.
- **Not an MCP tool:** the new routes are not offered as tools in this change; whether agents should read processes through the wiki is a decision of its own (ADR-0012).
- **Audit:** adding, changing and removing an engine are recorded (ADR-0029).

### Consequences

- **Positive:**
  - BPMN models are kept and shown as what they are, can be drawn in the wiki and opened again in any modeler.
  - A page about a process shows what the engine runs now, and, where allowed, where its instances are, without a copy that ages.
  - No exception to the content security policy; credentials stay on the server.
- **Negative / trade-offs accepted:**
  - The binary grows by about 1 MB.
  - Every signed-in person may list and read every process the engine lets the credential read; on atlas that is every deployed process. The engine, not the page, is the boundary; an administrator adds an engine with that in mind.
  - Each view of a process block reads the engine twice (the list, then the model); there is no cache.
  - The operations view counts the newest version only; instances still running on an earlier version are not shown.
  - The Word and PDF exports write a diagram's caption and a note in its place, not the diagram.
  - Changes made in the wiki are not deployed to the engine; the wiki shows, the engine runs.
  - An engine in the organisation's own network, the usual case, needs the operator to set `WWBB_ENGINE_ALLOW_PRIVATE=on` beside its token.
  - Each saved change of a stored diagram adds a file, as for drawings.
- **Follow-ups / risks to watch:**
  - The watermark condition (open question).
  - A short cache per engine, if many readers make the reads a load.
  - Diagrams in exports, e.g. a picture rendered when the diagram is saved.
  - Per-person access (option 7), if the engine as the boundary proves too coarse.

## Pros and cons of the options

### Option 2: a page of its own in a frame

Strongest form: the pattern the drawing editor proved (ADR-0053); whatever bpmn-js does now or in a later version, the main UI's policy never has to bend, and a fault in the library stays in the frame.

- Good: isolation by construction; the same message protocol as drawings.
- Bad: every diagram in a text is a frame with its own page load, which a page with several diagrams multiplies, and the reading view would host frames for what is only a picture with pan and zoom. The test showed no need for a looser policy, so the isolation buys nothing today; if a later version needs one, this option is the fallback.

### Option 3: pictures that carry their model

Strongest form: a diagram is a picture everywhere, in the album, in every export and in any client, exactly like a drawing, with no new block kind and no codec change.

- Good: exports and other clients show diagrams at once.
- Bad: the stored file is no longer the BPMN file a modeler or an engine opens, a large process becomes an unreadable thumbnail without pan and zoom, and a process from an engine cannot be a stored picture at all, since it is meant to be the newest. The model, not its picture, is what people keep.

### Option 5: the browser reads the engine

Strongest form: every reader is held to their own rights on the engine; the wiki holds no credential and makes no outbound calls.

- Good: per-person access without a shared account.
- Bad: the UI's policy would have to allow each engine's address for `connect-src`, the engine would have to answer cross-origin requests with credentials, and browsers increasingly withhold the engine's session cookie from another site's requests. Readers without an engine account would see nothing.

### Option 6: a connector copies processes

Strongest form: the connectors exist (ADR-0077); copies are searchable, versioned items that survive the engine being down.

- Good: search, history and resilience.
- Bad: a copy is as old as the last run, which is what the request rules out ("always the newest"), and runtime counts are not content to keep in history.

### Option 7: each person's own engine account

Strongest form: the engine's own permissions decide per reader, which is the finest boundary there is.

- Good: no shared account; the engine sees who reads.
- Bad: the wiki would become an OAuth client of each engine and keep a refresh token per person, a store and a flow of their own; readers without an engine account would see nothing. The requester chose the server's credential for now.

## Links

- relates to [ADR-0031](0031-web-ui-shell.md) (the UI and its policy), [ADR-0032](0032-prosemirror-rich-text-editor.md) (vendored bundles), [ADR-0045](0045-attachments-encrypted-files-beside-the-log.md) (attachments), [ADR-0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) (drawings), [ADR-0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md) (credentials from the environment), [ADR-0103](0103-the-versions-of-a-file-form-one-stack-in-the-album.md) (versions of a file)
- extended by [ADR-0142](0142-processes-in-a-drawing.md) (processes in a drawing)
