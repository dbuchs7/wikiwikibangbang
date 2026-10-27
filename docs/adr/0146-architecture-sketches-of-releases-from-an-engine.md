# ADR-0146: Architecture sketches of releases from an engine in the text

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-27
- **Deciders:** Project initiators (requested 2026-10-27: the architecture sketch of an atlas release inserted into pages and tasks as its processes, forms and decisions are since ADR-0129 and ADR-0144; atlas offers the two reads it needs to the token scope `diagrams` by the amendment of its ADR-0493 of the same day); proposed for the maintainer's acceptance
- **Open question:** Should the sketch of a fixed release, which never changes, be cached in the reader's browser, and is it acceptable that every reader of a page sees what the token's reach allows rather than what they may see in atlas themselves? (see "Open questions")
- **Question checked:** 2026-10

## Context and problem statement

Since ADR-0129 a page shows the newest version of a process of a process engine, and since ADR-0144 its forms and decisions, read through this server whenever the page is shown. atlas (github.com/pblumer/atlas), the engine the installation reads, also draws the **architecture sketch of a release** of an application (its ADR-0471 and ADR-0495): a picture, in layers, of what the release is and what it reaches. A page that documents an application wants that picture beside its processes, current for the newest release or as it was for a given one.

atlas offers the sketch to systems like this one through two reads of the scope `diagrams`, added by the amendment of its ADR-0493 of 2026-10-27:

- `GET /api/v1/architectures` lists, per application, the releases that carry a sketch, newest first: version, release ID, when the release was published and when its sketch was taken (Unix seconds), and the layers the sketch records. Applications are sorted by name; one without a sketch is left out. Nothing about people is listed. **Which applications appear is the token's reach:** a token minted with a reach sees the applications it names; a token of the scope `diagrams` minted without one, as every token minted for processes so far, lists none.
- `GET /api/v1/applications/{applicationId}/releases/{version}/architecture.embed.svg?layers=…` answers the sketch as `image/svg+xml`, with `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`. It is a **minimal view that is the same for every caller**: a worker's endpoints and configuration, instances, catalogue products, other applications and the owners of capabilities and value streams are withheld, because the wiki shows the picture to everyone who can open the page. The layers are `strategy`, `business`, `application`, `data` and `technology`; none asked means `application,technology`. It answers 400 for a version that is no positive number or an unknown layer, and 404 for an unknown release, a release recorded before sketches existed, a release that records none of the layers asked for, or an application outside the token's reach. The picture is about 1140 pixels wide, self-contained (no script, no external reference, no link) and the same for the same release and layers.

The question is how a page shows such a sketch: current where it follows the newest release, unchanged where it names one, under the rules ADR-0129 set for credentials and access, and under the UI's content security policy, `default-src 'self'`, which allows no picture from elsewhere and no `data:` address.

## Decision drivers

- What ADR-0129 holds for processes holds here: the UI's policy stays as it is, the credential stays on the server, the engine decides what the installation sees, the body holds no copy (D1, D4), and every new bound is a budget (ADR-0015).
- A block that follows the newest release shows it on every read; a block that names a release shows that release.
- A block shows only what the picker offers, as for forms (ADR-0144): the engine's list is the boundary.
- The engine's picture is shown as it is drawn; the wiki neither redraws it nor puts the engine's markup into its own pages.
- Nothing the engine answers runs in the UI, and a picture that holds a script, an embedded document or a reference outside itself is not shown, whatever the engine promises.

## Considered options

1. **The server reads the sketch with the engine's credential and serves it as a picture of this site**, which the block shows as an image.
2. **A copy of the picture as an attachment of the item**, made when the sketch is inserted.
3. **The browser draws the sketch** from atlas's architecture as JSON.
4. **atlas's own view in a frame**, or its picture straight from atlas.
5. **The picture inline in the page**, as SVG elements or a `data:` address.

## Decision outcome

Chosen option: **1**, because it keeps every rule of ADR-0129 and shows the engine's own picture without a copy and without an exception to the UI's policy.

### Reading from the engine

- `GET /api/engines/{id}/architectures` lists the applications whose releases carry a sketch the credential may read, each with its ID, its name and those releases, newest first: version, when published, when the sketch was taken and the layers it records. The release's ID at atlas is not passed on; nothing else is dropped, as the list holds nothing about people.
- `GET /api/engines/{id}/architectures/{application}/{release}?layers=…` answers the sketch. `{release}` is `newest` or a version, a positive number of 32 bits; `layers` names some of the five layers, separated by commas, or none for the engine's default. Anything else is refused with 400. The layers are passed on each once, in the order the engine draws them.
- **Looked up first:** the application and the release are first found in the list, so that a block shows only what the picker offers and `newest` is the first release listed. A release the list does not name is answered with 404 and not asked for.
- **The answer** is the picture as `image/svg+xml`, with `X-Content-Type-Options: nosniff`, the engine's policy `default-src 'none'; style-src 'unsafe-inline'; sandbox` and `Cache-Control: private, no-store`; `X-Release-Version` and `X-Release-Taken-At` name the release read, which tells a caller of `newest` what it got. It is a picture of this site's own address, so the UI shows it as an image under its unchanged policy; opened on its own in a tab, its policy runs nothing.
- **Defence in depth:** an answer whose type is not `image/svg+xml` is refused, and so is a picture that is not well-formed XML with an `svg` element at its root, or that holds a `script` or a `foreignObject` element, an event attribute (`on…`), an animation of an event attribute or of a reference, a reference (`href`, `xlink:href`) that is not a fragment of the picture itself (`#…`), or a stylesheet named by a processing instruction. The engine is then answered as not available (502), naming what the picture held. atlas draws none of these; the check is there in case an engine does.
- **Errors** are those of ADR-0129: they name the engine and what failed, never the credential. When the list names no application at all, a read says that the credential lists none and points the administrator at the token's reach.
- Every signed-in person and token reaches both reads, as the process routes. Paths, timeout, redirects, private addresses and the answer budget `EngineResponseBytes` are those of ADR-0129; the picture is read whole within that budget, so no new bound is needed.
- **The token:** on atlas, the scope `diagrams` reaches both reads. Which applications are offered is the token's reach; a token minted for processes without a reach lists none. The settings page and the picker say so.

### The block kind `architecture`

- An `architecture` block names an engine (`Engine`), an application by its ID there (`Application`), a release (`Release`: 0 for the newest release with a sketch, followed on every read, or a version, which never changes) and the layers shown (`Layers`: some of the five, each once, in the engine's order; none for the engine's default), with a plain caption. It holds no copy.
- The application's ID is taken as the engine gives it, by the rule ADR-0144 set for forms and decisions: 1 to 255 characters without control characters or surrounding spaces. A release is 0 to 2³¹−1. An empty list of layers is written as none, so that a body read back from the log holds what was written; the API takes `"layers": []` as none.
- Every other block refuses the three fields, as it refuses `Engine`, `Process`, `Form` and `Decision`.
- **Codec version 34** adds the block kind and the block fields `Application`, `Release` and `Layers`. Versions 1 to 33 are still read.
- **Insert → Architecture sketch from an engine** asks for the engine, then the application, then *Newest release* (at first, naming its version and when it was recorded) or a fixed version with its date, then the layers that release records, application and technology checked at first, then the caption, which is the application's name until it is changed. **Change sketch**, while editing, opens the same dialog with the block's values.

### Showing it

- The block's head names the application, the release ("newest — currently v3", or "v3") and when its sketch was taken. Below it the sketch is an image scaled to the text column, drawn on white in either colour scheme as diagrams are; a click opens the picture on its own in a new tab.
- The head is read from the list. A release the list offers is then shown at its version, also for a block that follows the newest one: the head and the picture name the same release, and a browser that keeps pictures it has drawn never shows an older newest release under a new head. A release the list does not offer is asked for as the block names it, so that the server's words say why it is missing.
- When the engine fails, the block says so as a process block does ("The architecture sketch cannot be shown: …"), and no picture stays. **Reload** reads it again; the block is read when it first comes into sight.
- **Exports** name the sketch, as diagrams (ADR-0129): the block holds only the application's ID, so Word, PDF and Markdown name it by its caption, "[Architecture sketch: Bestellwesen v3 (atlas)]" or "[Architecture sketch: Bestellwesen, newest release (atlas)]", and do not repeat the caption below.
- **Not an MCP tool**, as for processes, forms and decisions (ADR-0012, ADR-0129); the description of the body format names the block kind.

### Consequences

- **Positive:**
  - A page about an application shows its architecture as atlas draws it, current or as released, beside its processes, forms and decisions.
  - No copy that ages, no exception to the UI's policy, no new library; the credential stays on the server and the item model grows by one block kind.
  - The engine's markup never becomes part of the UI's document; the picture runs nothing in either place it is shown.
- **Negative / trade-offs accepted:**
  - Each view of a block reads the engine three times: the list for the head, then the list and the picture for the image. A block whose release is not offered reads it once more for the reason. There is no cache.
  - The full text atlas puts in each box's `<title>` shows as a tooltip only when the picture is opened on its own; an image shows no tooltips.
  - Every reader of a page sees the sketches the token's reach allows, in atlas's minimal view, whatever they may see in atlas themselves (open question).
  - A block of a release the token no longer reaches, or of an engine that is down, shows an error instead of a picture; nothing is kept to fall back on.
  - The exports name the sketch instead of drawing it.
  - Sketches are not offered in drawings (ADR-0142).
- **Follow-ups / risks to watch:**
  - A short private cache for fixed releases, if reads of the engine become a load (open question).
  - A copy kept on request for fixed releases (option 2, below), if engines that are down prove a problem; the exports could then draw it.
  - The sketch drawn into Word and PDF exports, read when the document is made.
  - Sketches in drawings, as processes (ADR-0142).

## Pros and cons of the options

### Option 2: a copy of the picture as an attachment

Strongest form: a fixed release never changes, so a copy made when the block is inserted is exact for ever. It shows when the engine is down or the token is withdrawn, costs the engine nothing on each read, is encrypted and versioned with the item (ADR-0026, ADR-0045), appears in the album and could be drawn by every export. For fixed releases it is the stronger choice in every respect but one.

- Good: resilience, no reads per view, exports that draw it.
- Bad: the one respect is the boundary ADR-0129 chose. A copy is content of the item, readable by everyone who reads the item from then on; when the administrator narrows the token's reach, or atlas withholds more in its minimal view in a later version, copies already made keep showing what the engine no longer offers. Live reads keep the engine as the boundary. A copy also cannot follow the newest release, so one block kind would have two behaviours, one with a file and one without, and inserting would need a path that writes the engine's answer into the item. Not adopted now; a copy made on request for a fixed release is a follow-up.

### Option 3: the browser draws the sketch from atlas's architecture JSON

Strongest form: an interactive sketch, with layers switched without a new read, tooltips, zoom, text that can be selected and colours that follow the UI's scheme, from a smaller answer.

- Good: interaction and theming.
- Bad: it would be a second renderer of atlas's layout (bands, boxes, edges and their rules from atlas's ADR-0471 and ADR-0495), kept in step with every change atlas makes by hand. And atlas answers the architecture JSON by who asks (its ADR-0495 §5), so it is not in the scope `diagrams`: the wiki would need a `full` token, which carries its minter's view, and would show that view to every reader of the page, which is what atlas's minimal view exists to prevent.

### Option 4: atlas's own view in a frame, or its picture straight from atlas

Strongest form: exactly atlas's own interactive view, with each reader's own rights in atlas and nothing passed through this server.

- Good: fidelity and per-person access.
- Bad: the UI's policy would have to allow each engine as a frame or image source; atlas would have to allow being framed by this site, and the reader would need a session at atlas, which browsers increasingly withhold from another site's frames and requests. Readers without an atlas account would see nothing, and two readers of one page would see different pictures. These are the reasons ADR-0129 rejected its option 5 and ADR-0144 its option 3. A picture taken straight from atlas would in addition need the credential in the browser.

### Option 5: the picture inline in the page

Strongest form: the boxes' tooltips show on hover, text can be selected, and no second request is made for the picture.

- Good: tooltips and text in place.
- Bad: a `data:` address is not allowed by the UI's policy. SVG elements in the page would put the engine's markup into the UI's own document, against ADR-0014 and `TestNoMarkupFromStrings`, and under the page's policy, which drops the style attributes the sketch is drawn with. A picture of its own, as an image, keeps its styles and runs nothing.

## Open questions

1. **Caching fixed releases.** A fixed release's sketch never changes on atlas, so `Cache-Control: private, max-age=…` on a fixed release would spare the engine the reads of a page that is read often. It is not adopted: the browser would keep the picture after its user signs out, after the administrator narrows the token's reach and after atlas changes its minimal view in an upgrade; and the list is still read for the head. If the reads prove a load, a short private cache, or a cache on the server per engine as ADR-0129 named, is the next step.
2. **The token's reach, not the reader's.** Every reader of a page sees the sketches of the applications the token's reach names, in atlas's minimal view, not what each of them may see in atlas, as ADR-0129 records for processes. atlas's minimal view withholds what it deems personal or sensitive, and the administrator chooses the reach. Whether that is enough for an organisation's architecture is the maintainer's question; per-person access is ADR-0129's option 7.

## Links

- relates to [ADR-0012](0012-mcp-adapter-over-http-api.md) (MCP), [ADR-0014](0014-closed-renderer-for-user-content.md) (no markup from strings), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0031](0031-web-ui-shell.md) (the UI and its policy), [ADR-0045](0045-attachments-encrypted-files-beside-the-log.md) (attachments), [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (processes from an engine), [ADR-0142](0142-processes-in-a-drawing.md) (processes in a drawing), [ADR-0144](0144-forms-and-decisions-from-a-process-engine.md) (forms and decisions from an engine)
