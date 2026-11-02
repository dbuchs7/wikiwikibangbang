# ADR-0163: A process shows its design or its implementation, and a business rule task its decision

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-11-02
- **Deciders:** Project initiators. The maintainer asked (2026-11-02), looking at the description of the process "AOG P1: Einheit verarbeiten" from atlas below its diagram, for two things: to switch the view between design and implementation, and to show, at a business rule task in the description, the decision it calls. Asked what the implementation view means, they answered that it is what atlas's modeler shows: the icons of the Worker Types used, added to the tasks. They chose (2026-11-02):
  1. **Who switches:** every reader, for themselves, as the operations are switched (ADR-0129), not kept with the block.
  2. **The decision:** its table embedded below the step, in both views.
- **Open question:** the icons are taken from atlas's catalogue and kept here as data. Should atlas instead serve its catalogue of implementation types (IDs, names, icons) through its API, so that a Worker Type atlas adds is drawn here without a change to this project?
- **Question checked:** 2026-11

## Context and problem statement

A bpmn or process block (ADR-0129) draws a BPMN model with bpmn-js; ADR-0143 lists its tasks and decisions below the diagram or in its place. A service task is drawn with BPMN's gear whatever carries it out. atlas's modeler has two views of a model:

- **Design** shows names, labels and the flow, as a business reader needs them.
- **Implement** marks each element that has an *implementation type* with that type's icon, over BPMN's own marker: the Worker Type of a service or send task (Microsoft Entra ID, Microsoft SQL Server, REST and some twenty others, each an extension element such as `<atlas:entraConnector>`), the language of a job script (`<atlas:jobScript language="python">`), a send task's message and the agent that runs an ad hoc sub-process (`<atlas:agentConnector>`). It also shows the FEEL conditions of the ways out.

A business rule task names its decision in `<zeebe:calledDecision decisionId="…" resultVariable="…">`. Since ADR-0144 a page can show an engine's decision as a block of its own, but the description of a process names the task only, and the reader has to find the decision by its ID.

How does a page show what atlas's Implement view shows, and the decision of a business rule task, without changing the item model?

## Decision drivers

- One source: what the description and the marks say is read from the model the diagram draws (ADR-0143).
- The body's model stays as it is (D1, D4): a reader's view is not content.
- The UI creates elements, never markup from strings (ADR-0014), under its content security policy (ADR-0031).
- A decision is read as ADR-0144 reads it, through this server, with the engine's credential.
- A task looks the same here as in atlas.
- Two other products are named in this project only where the subject is import (AGENTS.md); atlas's catalogue holds a Worker Type named after one of them, which is therefore not listed here.

## Considered options

For the implementation view:

1. **A reader's switch; the implementation types read from the model's extension elements, their icons kept here as data taken from atlas's catalogue.**
2. **A block field set by the editor**, as `View` (ADR-0143).
3. **Icons served by the engine**, read through this server.
4. **A letter on a coloured tile per Worker Type**, drawn here, without atlas's icons.

For the decision:

5. **The decision's table embedded in the step**, read through the route of ADR-0144.
6. **A link to a decision block** the editor places elsewhere on the page.

## Decision outcome

Chosen options: **1** and **5**.

### Design and implementation

- **The switch.** Once a diagram is drawn, its head shows two buttons, *Design* and *Implementation*; *Design* is pressed at first. The choice belongs to the reader and to this showing of the block; it is not stored, and a reload shows the design again. It is offered to readers and editors, in every view of ADR-0143.
- **Design** is what the block showed before this record, except that the ways out of a decision are listed by name only, without their conditions.
- **Implementation:**
  - every element with an implementation type gets its icon over the top left of its shape, as a bpmn-js overlay, with the type's name as its tip;
  - in the description, the step's kind line shows the icon and names the type: "Service task · Microsoft Entra ID";
  - a way out shows its condition, as before this record: `anlegen (=regel.aktion = "anlegen") → Strukturgruppe anlegen`;
  - a business rule task names where its result goes: "Decision AogGruppenregel → result in regel".
- **Which elements, as atlas marks them** (`bpmndoc.js`, `implementationOf`):
  - a service task: its Worker Type; the plain job worker (`zeebe:taskDefinition` only) keeps BPMN's gear;
  - a send task: its message if it names one, else its Worker Type; a shop task keeps BPMN's envelope;
  - a script task: the language of its job script, PowerShell when it names none, as atlas runs it; a FEEL script runs in the engine and is not marked;
  - an ad hoc sub-process with an agent;
  - no other element.
- **How the model is read.** The viewer reads the model without atlas's and Zeebe's descriptors, so an extension element is typed by the name the model writes, with the prefix the model chose (`a:entraConnector`). An element is therefore recognised by its local name, in any case and under any prefix.
- **The icons** are in `workertypes.js`: for each type its ID, the local name of its extension element, atlas's English name and its icon as data, `[tag, attributes, …children]` of an SVG in a 16 × 16 box, built with `createElementNS`. They are taken from atlas's catalogue (`api/web/editor.js`, AGPL-3.0-only, as this project), so a task looks the same in both. Names are translated under `bpmn.impl.<id>`.
- **A Worker Type not in the list** — one atlas adds later, and the one this project does not name — is recognised by an extension element whose local name ends in `Connector`. It gets a neutral icon, a grey tile with a gear, and is named as the model names it: "Worker Type ledger".

### The decision of a business rule task

- Below a business rule task that calls a decision, the description says "Decision {ID}".
- For a process of an engine, the decision's table follows: the newest deployed version, read through `GET /api/engines/{id}/decisions/{decision}` (ADR-0144), drawn by dmn-js at the decision's own table or expression, with the line "Newest deployed version: {n}" and, as a decision block says, whether the model was read as DMN 1.3 or parts of it are not shown. dmn-js's button to the requirements graph is hidden here; the graph is the decision block's.
- It is read when the description is first shown, once per step, and kept while the same model is shown; a reload of the block reads it again.
- A BPMN file of the page has no engine. The step says that the engine keeps the decision and that a diagram from a file does not show its table.
- On atlas a decision needs a token of the scope `full`, as ADR-0144 records. With a narrower token the step shows the engine's refusal in place of the table; the rest of the description is unaffected.

### Consequences

- **Positive:**
  - A reader sees at a glance which system each task touches, as in atlas, and a business reader sees the design without FEEL.
  - The rule a business rule task applies is read where the task is described, from the engine, never as a copy.
  - Nothing is stored: no codec version, no change to the API or to MCP.
- **Negative / trade-offs accepted:**
  - The icons are a copy of atlas's catalogue. A Worker Type atlas adds gets the neutral icon until `workertypes.js` is updated, and an icon atlas redraws stays as it was here.
  - The design no longer shows the conditions of the ways out, which ADR-0143 listed always; a reader who wants them switches to the implementation.
  - A decision is shown in its newest deployed version, while a process may be bound to an earlier one (`bindingType` and `versionTag` of `zeebe:calledDecision`, not read here).
  - Every described business rule task of an engine's process reads the engine twice (ADR-0144) and loads dmn-js when its description is first shown.
  - The choice of a level is not kept; a page meant for implementers opens at the design every time.
- **Follow-ups / risks to watch:**
  - The open question on atlas serving its catalogue.
  - Showing the decision in the version the process is bound to, when atlas's API can answer it.
  - Exports (ADR-0071, ADR-0131) still name the diagram only (ADR-0143).
  - Processes in a drawing (ADR-0142) do not show the implementation.

## Pros and cons of the options

### Option 2: a block field set by the editor

Strongest form: a page written for operators opens at the implementation for every reader, as a page chooses its view (ADR-0143).

- Good: the page decides what its readers see first.
- Bad: the maintainer chose a reader's switch; a field needs a codec version and golden files for something a reader can switch in one click. It can be added later as the block's default without changing the switch.

### Option 3: icons served by the engine

Strongest form: atlas is the source of its Worker Types; a type it adds would be drawn here at once, and nothing would be copied.

- Good: no copy that ages; the icon always matches.
- Bad: atlas has no such route. The icons are SVG markup, which this UI does not insert from strings (ADR-0014); they would have to be parsed into elements or shown as pictures of this site. It is the open question.

### Option 4: a letter on a coloured tile

Strongest form: nothing copied, a handful of lines, and every Worker Type, known or not, gets a mark.

- Good: small and independent of atlas.
- Bad: the request is for the icons atlas shows. A reader who knows atlas's modeler would see different marks for the same task.

### Option 6: a link to a decision block

Strongest form: the decision is shown once on a page, with its graph and its other decisions, and the description only points at it.

- Good: no second viewer of the same decision on a page.
- Bad: the editor has to place and keep the block, and the description says nothing of the rule where the task is described. The request is to see the decision at the task.

## Links

- extends [ADR-0143](0143-a-diagram-describes-its-tasks-and-decisions.md) (the description of a process) and [ADR-0144](0144-forms-and-decisions-from-a-process-engine.md) (decisions from an engine)
- relates to [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (BPMN diagrams and the operations a reader switches), [ADR-0014](0014-closed-renderer-for-user-content.md) (no markup from strings), [ADR-0142](0142-processes-in-a-drawing.md) (processes in a drawing)
