# ADR-0143: A diagram describes its tasks and decisions below it, or in its place

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (requested 2026-10-26: for an embedded process, besides switching on its operations, to set whether the whole description of the process is listed below the diagram, above all the tasks and decisions with their documentation. A click on a task's title in the description highlights the task in the diagram, and a click on the task in the diagram jumps to its description. Three views: the diagram only, the diagram and the description, or the description only); proposed for the maintainer's acceptance
- **Open question:** The view is set by whoever edits the page, and readers see what it chose. Should readers be able to switch the view for themselves as well, as they switch the operations on and off (ADR-0129)?
- **Question checked:** 2026-10

## Context and problem statement

A bpmn or process block (ADR-0129) shows a diagram. The prose that explains a process is in its model:
- the `documentation` of each element;
- the text annotations associated with an element.

atlas's process documentation (atlas ADR-0143) is made of exactly this prose. A reader of the wiki sees none of it. An auditor, a new colleague or the business owner wants the tasks and decisions written out:
- who does what;
- what each step means;
- where each way out of a decision leads.

Diagram and text should point at each other, and some pages want the text without the picture.

## Decision drivers

- One source: the description is read from the same model the diagram draws, so the two cannot drift apart.
- The page decides how it shows a process, as it decides a picture's width (ADR-0056).
- The body's model stays explicit (D1, D4): how a block shows is a field of the block, validated; the description is derived when shown, not stored.
- Nothing new is asked of an engine. The newest version's model, which the wiki already reads, holds the prose.
- The UI's content security policy stays as it is (ADR-0031).

## Considered options

1. **A view per block, and the description read from the model in the browser.** The block field `View` is empty (the diagram), `both` or `text`. The description comes from the model bpmn-js has just read.
2. **A switch for readers only**, like the operations, with no field in the block.
3. **Descriptions written in the wiki**, per task, kept with the page.
4. **atlas's published process documentation**, read from the engine.

## Decision outcome

Chosen option: **1**.

### The view

- **The field.** A bpmn or a process block may carry `View`:
  - empty: the diagram only, as before;
  - `both`: the diagram and below it its description;
  - `text`: the description only.

  Other values, and a view on any other block, are refused. Codec version 32; versions 1 to 31 are still read.
- **Who sets it.** Whoever edits the page chooses the view with a select in the block's head. Readers see what was chosen (the open question).
- **The API and MCP** carry `view` on such a block, and MCP's description of the body format names it.

### The description

- **Where it comes from.** It is read from the model bpmn-js has just imported (`bpmndoc.js`, `describe`), so it always belongs to the version drawn: for an engine's process, the newest.
- **Processes.** Each process is named by its pool, or else by its own name, followed by its documentation.
- **Steps.**
  - Which steps:
    - every activity (tasks of each kind, call activities, sub-processes);
    - every decision, that is a gateway with more than one way out;
    - any other element whose model says something about it, e.g. a documented event.
  - What each step shows:
    - its name;
    - its kind, e.g. user task or exclusive gateway, named in the UI's language;
    - its innermost lane;
    - its documentation, with its line breaks;
    - the text annotations associated with it;
    - for a decision, every way out: its name and condition, where it leads, and which way is the default.

  A task the model does not describe says so.
- **Reading order.** The steps follow the flow, read from the sequence flows' `sourceRef` and `targetRef`. BPMN requires those, while the nodes' `incoming` and `outgoing` lists are written by some tools only.
  - Each branch is listed up to where it joins, then the join and what follows.
  - An event on a task comes right after the task.
  - A loop is entered where the flow first reaches it.
  - What a sub-process holds follows the sub-process, indented.
- **Operations.** While the operations are on (ADR-0129), each step shows its counts too. With the description only, the operations are still offered.

### Diagram and description point at each other (view `both`)

- A step's title is a link. It marks the element in the diagram (a stroke in the accent colour), brings the element into the diagram's view and the diagram into sight.
- A click on an element in the diagram marks it and scrolls its step into sight, which flashes briefly.
- With the description only, the titles are plain text.

### Consequences

- **Positive:**
  - A process is readable as text, from the prose its modellers wrote, beside its picture or instead of it; for an engine's process, always in its newest version.
  - The description needs nothing stored and nothing new on the server or the engine.
- **Negative / trade-offs accepted:**
  - A model without documentation gives a list of names and kinds. The description is as good as the model's prose.
  - The reading order is a heuristic of the flow. In a model with many joins and loops it can differ from the order an author would choose.
  - Exports (Word, HTML, Markdown; ADR-0071, ADR-0131) still name the diagram only, without its description. Processes in a drawing (ADR-0142) have no description.
  - One more codec version, with its golden files.
- **Follow-ups / risks to watch:**
  - The open question on readers switching the view.
  - Exporting the description with the page.
  - A collapsed sub-process is drawn by bpmn-js on a plane of its own. Its steps are described, but marking them does not change the plane.

## Pros and cons of the options

### Option 2: a switch for readers only

Strongest form: no change to the persisted format, and every reader chooses what they need, as with the operations.

- Good: no codec version; nothing to decide for the author.
- Bad: the request is for the page to define how a process is shown. A page meant as a process description would open as a bare diagram for every reader, who must find the switch first.

### Option 3: descriptions written in the wiki

Strongest form: the wiki's editor, mentions and history for each task's text; prose a modeller never wrote can be added by those who know the work.

- Good: rich text, and authors without the modeler.
- Bad: a second source beside the model, keyed to element IDs that change when the model is redrawn. It drifts from the model, and for an engine's process it would describe a version that is no longer the newest.

### Option 4: atlas's process documentation

Strongest form: the description the modellers published deliberately, versioned, the same document auditors receive.

- Good: one published text, with a history.
- Bad: it is a PDF of one published version, not the newest deployment, and reading it needs another route in the `diagrams` scope. A BPMN file of the page has none. The element prose it is made of is in the model the wiki already reads.

## Links

- relates to ADR-0129 (BPMN diagrams and processes of engines), ADR-0056 (a block decides how it shows), ADR-0142 (processes in a drawing), ADR-0071 and ADR-0131 (exports); atlas ADR-0143 (process documentation export) describes the same prose
