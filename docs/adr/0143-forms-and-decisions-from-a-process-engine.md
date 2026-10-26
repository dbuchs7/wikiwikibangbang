# ADR-0143: Forms and decisions from a process engine in the text

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested 2026-10-26: forms and decisions of an atlas instance embedded in a page, as its processes are since ADR-0129); proposed for the maintainer's acceptance
- **Open question:** dmn-js places its bpmn.io watermark, and the notice the watermark opens, with style attributes, which the UI's content security policy drops; the browser reports each drop on its console. This record keeps the policy as it is and puts the watermark in place with the UI's own stylesheet. Should the policy instead allow exactly the two style values of the watermark by their hashes (`style-src 'self' 'unsafe-hashes' 'sha256-…'`), so that dmn-js places it itself and nothing is reported? The licence condition on the watermark is ADR-0129's open question, which this record shares for form-js and dmn-js.
- **Question checked:** 2026-10

## Context and problem statement

Since ADR-0129 a page shows the newest version of a process of a process engine, read through this server whenever the page is shown. A process is rarely all a page about it explains. atlas (github.com/pblumer/atlas), the engine the installation reads, also keeps:

- **Forms**: form-js schemas that user tasks show. atlas keeps one version of a form, saved in place, with no history; its listing is narrowed to the forms the caller's account may see, while a form is answered by its ID to any signed-in caller, as a task needs it.
- **Decisions**: DMN models deployed as decisions. Each deployment has a key and holds one model, which may hold several decisions; each decision has versions. atlas lists a decision by its name when it has one, so IDs such as "Decision 1" and "Bonität" occur. Models are written as DMN 1.3 or DMN 1.5.

The question is how a page shows a form and a decision of an engine, as it shows a process: current, without a copy, and under the same rules for credentials and access.

## Decision drivers

- What ADR-0129 holds for processes holds here: the UI's content security policy stays as it is, the credential stays on the server, the engine decides what the installation sees, the body holds no copy (D1, D4), and every new bound is a budget (ADR-0015).
- A page shows what the engine has now: a form as it is saved, a decision in its newest deployed version.
- Nothing typed into a form on a page leaves the browser.
- The engine's data is taken as it is: IDs atlas accepts, DMN 1.5 models and components only atlas knows must not keep a block from showing.
- The bpmn.io watermark stays visible and unchanged.

## Considered options

For showing forms and decisions:

1. **form-js and dmn-js viewers, vendored and run in the UI under its policy**, as bpmn-js (ADR-0129).
2. **The server renders them**: a form as HTML and a decision table as an HTML table, written in Go.
3. **The engine's own pages in a frame.**
4. **atlas's own build of dmn-js**, a fork that reads DMN 1.5.

For reading them: the server reads the engine with the credential from the environment, as ADR-0129 decided for processes (its option 4); the options it rejected there are rejected here for the same reasons.

## Decision outcome

Chosen option: **1**, with reading as for processes.

### The viewers

- **form-js:** `@bpmn-io/form-js-viewer` 1.26.1, the newest 1.x, bundled from `internal/web/vendor` into `static/vendor/form-viewer.js` (about 600 KB) with its stylesheet in `static/vendor/form/`. atlas writes its forms with form-js 1.24.0; form-js 2.0, published on 2026-09-18, is a new major version and not taken until the forms are written with it.
- **dmn-js:** `dmn-js` 17.13.0, its navigated viewer (requirements graph, decision tables, literal expressions and boxed expressions, none editable), bundled into `static/vendor/dmn-viewer.js` (about 760 KB, CodeMirror included for FEEL) with its stylesheets and icon font in `static/vendor/dmn/`. 17.13.0 is taken so that every part of dmn-js comes from one release; 18.0, published on 2026-10-23, is not yet taken.
- A page loads a viewer only when its first block comes into sight. Both are under the bpmn.io licence, as bpmn-js; `form.LICENSE.txt` and `dmn.LICENSE.txt` carry the licence texts, and the watermark code is part of the bundles, unchanged.
- **The policy:** form-js draws with the DOM and sets styles through it; a test in Chromium under the UI's own policy recorded no violation. dmn-js draws the same way, except its watermark: the link and its logo carry style attributes (`position: absolute; bottom: 15px; right: 15px; z-index: 100;` and `vertical-align:middle`), as does the notice the link opens. The policy drops them and the browser reports each drop. `style.css` gives the watermark and the notice exactly those rules, so the watermark stands where dmn-js places it, at the bottom right; below a table or an expression the block leaves room for it, so that it covers none of it. The smoke test accepts the reports of these two values, named by their hashes, and no other.

### Reading from the engine

- `GET /api/engines/{id}/forms` lists the forms the credential may see (ID, name, when saved), read from `GET /api/v1/forms`.
- `GET /api/engines/{id}/forms/{form}` answers a form with its schema, read from `GET /api/v1/forms/{id}`, as JSON. The form is first looked up in the list: atlas answers any form by its ID to whoever is signed in, and a block shows only what the picker offers.
- `GET /api/engines/{id}/decisions` lists the newest deployed version of each decision (ID, model, version, when deployed), read from `GET /api/v1/decision-deployments`.
- `GET /api/engines/{id}/decisions/{decision}` answers that version with the DMN model it is deployed in, read from `GET /api/v1/decision-deployments/{key}/xml`, as JSON, so the model is never a document of this site. The deployment's key stays on the server.
- Every signed-in person and token reaches the four reads; on atlas every deployed decision is listed to every signed-in caller, and the forms are those the credential's account may see. Paths, timeout, redirects, private addresses, the answer budget `EngineResponseBytes` and the error messages are those of ADR-0129.
- **The token:** on atlas, a token of the scope `diagrams` reaches the processes only, not these four reads; forms and decisions need a token of the scope `full` until atlas offers a narrower one. The settings page says so.

### The block kinds `form` and `decision`

- A `form` block names an engine (`Engine`) and a form by its ID there (`Form`); a `decision` block an engine and a decision by its ID there (`Decision`); both with a plain caption. Neither holds a copy.
- **IDs as the engine gives them:** a form's or a decision's ID is 1 to 255 characters without control characters or surrounding spaces. Spaces, slashes and letters of any script are allowed, since atlas takes them; an ID is one path segment of the routes above, escaped.
- **Codec version 32** adds the block kinds and the block fields `Form` and `Decision`. Versions 1 to 31 are still read.
- **Insert → Form from an engine** and **Insert → Decision from an engine** list the engines, then their forms or decisions, filtered as typed.

### Showing them

- **A form** is drawn by form-js as a preview: a reader may fill it in to see its conditions and checks; a submit says that nothing was sent, and nothing is. A component of a type form-js does not know, such as atlas's own e-mail view, would stop the whole form from being shown; it is replaced by a line that names its type and says that only the engine shows it. A field whose options atlas reads from a directory shows as a selection without options.
- **A decision** is opened at its own table or expression, found by its ID or, as atlas lists it, by its name. A list switches to the model's other decisions and to its requirements graph, which is fitted to the block and pans and zooms. dmn-js's own box with the model's name and ID is left out of the graph; the block's head names the model.
- **DMN 1.5:** dmn-js reads DMN 1.3. A model in the DMN 1.5 namespaces, as atlas writes them, is read with those namespaces renamed to the ones of 1.3, in the browser; the block says so. What both versions have, decisions, tables, expressions, the graph and decision services, reads as written; what only 1.5 has is reported by dmn-js as warnings, and the block says how many parts are not shown.
- **Reload** reads the block again; both are read when they first come into sight.
- **Exports** name them, as diagrams (ADR-0129): "[Form: travel (atlas)]" and "[Decision: Decision 1 (atlas)]", with the caption.
- **Not an MCP tool**, as for processes (ADR-0012, ADR-0129).

### Consequences

- **Positive:**
  - A page about a process shows the forms its tasks present and the rules its decisions apply, as the engine has them now.
  - No copy that ages; the credential stays on the server; the item model changes only by two block kinds.
- **Negative / trade-offs accepted:**
  - The binary grows by about 1.6 MB.
  - A form is shown as it is saved, which may be ahead of what a running task shows; atlas keeps no versions of a form.
  - A decision is shown in its newest deployed version; processes pinned to an earlier version evaluate that one.
  - A DMN 1.5 model is shown as far as DMN 1.3 reaches; what only 1.5 has is missing, and the block says so.
  - Readers who open the browser's console see the reports of the watermark's dropped styles.
  - Each view of a block reads the engine twice (the list, then the form or the model); there is no cache.
  - A full token on atlas reaches far more than these reads.
  - Forms and decisions are not offered in drawings (ADR-0142), and decisions have no operations view.
- **Follow-ups / risks to watch:**
  - Propose to atlas that the scope `diagrams`, or a scope of its own, reaches `GET /api/v1/forms`, `GET /api/v1/forms/{id}`, `GET /api/v1/decision-deployments` and `GET /api/v1/decision-deployments/{key}/xml`.
  - The watermark's styles (open question).
  - DMN 1.5 in dmn-js itself, or atlas's fork if it is published as a package.
  - An operations view of a decision, counts of evaluations per rule, needs an atlas route that answers counts without the evaluations' inputs and outputs, as `runtime/counts` does for processes.
  - Forms and decisions in drawings, as processes (ADR-0142).

## Pros and cons of the options

### Option 2: the server renders them

Strongest form: no JavaScript library to vendor and review, nothing loaded in the browser, the same output in the page, the exports and other clients, and no watermark condition.

- Good: small, and the exports could show forms and tables.
- Bad: form-js schemas carry FEEL expressions for conditions, options and computed values, which a server-side renderer would have to evaluate to show the form as atlas shows it; DMN tables and graphs would be a second renderer of the notation, kept in step with the engine's by hand. The result would look and behave unlike the engine's own forms and decisions.

### Option 3: the engine's own pages in a frame

Strongest form: exactly what the engine shows, with the reader's own rights there, and nothing to vendor.

- Good: fidelity and per-person access.
- Bad: the UI's policy would have to allow each engine as a frame source, the engine would have to allow being framed by this site and the reader would need a session at the engine, the problems ADR-0129 named for its option 5. Readers without an engine account would see nothing.

### Option 4: atlas's own build of dmn-js

Strongest form: the fork atlas uses reads DMN 1.5 and draws decision services as atlas does, so a decision would look exactly as in the engine.

- Good: no namespace renaming; DMN 1.5 shown whole.
- Bad: the fork is pinned to a commit of a repository, not published as a package; the vendor build would depend on a fork's history and on atlas's choices for its editor. When it is published, or when dmn-js reads DMN 1.5, the renaming goes.

### A form read-only

Strongest form: a page documents; fields that can be filled in suggest that something is submitted.

- Good: no doubt about what happens to what is typed.
- Bad: conditions hide and show fields only as a form is filled in, and checks show only then; a read-only form hides how it behaves. The preview says that nothing is sent, and a submit says it again.

## Links

- relates to [ADR-0012](0012-mcp-adapter-over-http-api.md) (MCP), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0031](0031-web-ui-shell.md) (the UI and its policy), [ADR-0032](0032-prosemirror-rich-text-editor.md) (vendored bundles), [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (processes from an engine), [ADR-0142](0142-processes-in-a-drawing.md) (processes in a drawing)
