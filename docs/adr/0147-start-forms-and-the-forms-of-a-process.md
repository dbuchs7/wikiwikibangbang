# ADR-0147: The forms of a process's steps, and start forms that start a process from a page

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-27
- **Deciders:** Project initiators (requested 2026-10-27: the form picker of ADR-0144 offered forms that make no sense to the editor; the forms worth placing are those a process uses, documented with their fields as a process is with its tasks (ADR-0143), and the public start forms, which should start their process from a page, filled in beforehand with what the wiki knows of the reader); proposed for the maintainer's acceptance
- **Open question:** Should a start through this server be bound to a block of a page the reader may read, so that the start link never reaches the browser, and should the audit trail record who started what through a link the engine records as anonymous? (see "Open questions")
- **Question checked:** 2026-10

## Context and problem statement

Since ADR-0144 a page shows a form an engine keeps, chosen from `GET /api/v1/forms`. On atlas (github.com/pblumer/atlas) that list is the form library as the token's account may see it, scoped by projects and owners (atlas ADR-0071): drafts, forms of other teams' projects shared to the system project and forms that no process uses. An editor who documents a process finds there forms that belong to nothing, and not the ones the process shows. What the requester asked for instead are three uses:

1. **Documentation:** the form a step of a process shows, explained in the text, with the list of its fields and their descriptions below it or in its place, as ADR-0143 lists a process's tasks.
2. **Starting a process from the documentation:** a page about incidents or holidays carries the form that reports an incident or enters holidays, and sending it starts the process. Only public start forms for now; internal forms, for signed-in people or certain groups, once single sign-on reaches the engine.
3. **User tasks** listed by a filter, as cards or a list with a dialog. It is not decided here (see "Follow-ups").

What atlas offers for the first two:

- **Which forms a process uses** is in its model. A start event names its start form, a user task its work form, and a service, send, receive, script, business-rule or manual task the repair form an operator mends it with, each as `zeebe:formDefinition formId="…"` in its `extensionElements` (atlas ADR-0028, ADR-0169). `GET /api/v1/processes` names a version's start form as `startFormId` and says whether it is `executable`; `GET /api/v1/processes/{key}/xml` is the model. Both are in the token scope `diagrams`. `GET /api/v1/forms/{id}` answers any form by its ID to any signed-in account, as a task needs it; it is not in `diagrams`.
- **Public start links** (atlas ADR-0029): a modeler publishes a process's start form under a link whose token is 32 random bytes in lower-case hexadecimal. `GET /api/v1/public-links` lists them with their process and form, for the role Modeler. Two routes answer **anyone, without a credential**: `GET /public/forms/{token}/schema` gives the process's name and the form's schema, and `POST /public/forms/{token}/start` with `{"variables": {…}}` starts the newest version of the process, recording no initiator. atlas refuses values the form has no field for (400), an unknown or revoked link (404), a process that is not executable (409), and more than 20 requests at once or one a second per client address (429), the address read from forwarding headers only when they come from a proxy atlas trusts. Cross-origin calls are allowed only for origins its operator lists (atlas ADR-0186).
- **Public wizards** (atlas ADR-0449): a process may lead its anonymous filler through further steps after the start, in atlas's own pages. The start then answers a pass, which atlas's page keeps in the browser tab's storage of atlas's origin; no other page can hand it over.

The wiki's UI runs under `default-src 'self'`: the browser reaches no engine. The question is how a page offers the forms a process uses, describes them, and starts a process through a public link, under the rules ADR-0129 set: the credential stays on the server, the engine decides what the installation sees, the body holds no copy, and the policy stays as it is.

## Decision drivers

- The picker offers what a process uses, not the library.
- A form's description is what the engine says of its fields, read when shown, as a process's description is read from its model (ADR-0143).
- A start through a public link is anonymous at the engine, and stays so: the engine's credential never travels with it, and nothing of the reader is sent that the reader did not see in the form and could change.
- The UI's policy, the credential's place and the body's content are those of ADR-0129 (D1, D4); every new bound is a budget or classified (ADR-0015); unreadable stays non-existent (D10).
- Internal starts with the reader's identity stay possible later, without being faked now.

## Considered options

1. **This server relays the engine's public routes**, without the credential, and the block shows the form and sends it there.
2. **The browser calls the engine's public routes itself**, with the policy and the engine's cross-origin rules allowing it.
3. **A link to the engine's own page of the start link**, opened in a new tab.
4. **A start with the engine's credential through its authenticated API**, naming the reader.
5. **The engine's page of the start link in a frame.**

## Decision outcome

Chosen option: **1**, because it starts the process from the page as anyone with the link may, keeps every rule of ADR-0129 and sends the engine nothing that identifies the reader beyond what the reader enters. Option 3 is kept for what option 1 cannot do: a process with a public wizard.

### The forms of a process's steps

- `GET /api/engines/{id}/processes/{process}/forms` reads the newest version's model and answers the steps that name a form, in the order of the model: the element's ID, its kind (`startEvent`, `userTask` or the task a repair form belongs to), its name and the form's ID; and whether the process holds a public wizard (`atlas:wizard="public"` on a sub-process). The model is read by the local names of its elements and attributes, as atlas writes them under prefixes of its own; a `formDefinition` belongs to the innermost step around it, and one without `formId` or outside a step names nothing.
- `GET /api/engines/{id}/forms/{form}?process=…` reads a form as the form of that process's steps: it is answered only when a step of the newest version names it, and `steps` lists those steps. Without `process`, a form is looked up in the credential's listing as before (ADR-0144), so blocks made before stay as they are.
- **Insert → Form from an engine** lists the engine's processes first, then the steps of the one chosen that name a form; choosing a step places a form block with the process (block field `Process`) and the step's name as its caption.
- **The view** of a form block (block field `View`, as for processes, ADR-0143): the form, the form and the list of its fields below it (`both`), or the list alone (`text`). The list says at which steps the form is used, then each field with its label, kind, key, whether it is required or shown on a condition, its description and its options; a group or a list of entries heads its fields. It is read from the form-js schema in the browser (`assets/engineparts.js`, `fieldsOf`); texts, pictures, separators and buttons are no fields.
- On atlas the model is in the scope `diagrams`; the form itself still needs the scope `full`, as ADR-0144 records.

### Start forms

- **Block kind `startform`:** an engine (`Engine`), a public start link by its token (`Link`, 32 to 128 lower-case hexadecimal digits, which keeps anything else from the engine's path), the BPMN ID of the process it starts when known (`Process`), the fields filled in beforehand (`Prefill`, at most 20, each a field's key once and one of the sources below) and a plain caption. It holds no copy of the form. Every other block refuses `Link` and `Prefill`. **Codec version 35** adds the block kind, the fields `Link` and `Prefill`, and a form block's `View` and `Process`; versions 1 to 34 are still read.
- `GET /api/engines/{id}/public-links` lists the links the credential may list, with their process's name, to those who may edit in a space (as people and groups are looked up); a reader of a page reads the link its block names. On atlas this needs a token of the scope `full` whose account holds the role Modeler; otherwise the picker says why and the editor pastes the link or the engine's page of it. A pasted link is matched against the list when there is one, to learn its process.
- `GET /api/engines/{id}/public-links/{link}?process=…` reads the link's form through the engine's public route **without the credential**: the process's name, the schema and the engine's page of the link. With `process` and a credential, the process's model is read to say whether it holds a public wizard.
- `POST /api/engines/{id}/public-links/{link}/start` with `{"variables": {…}, "process": …}` sends the values through the engine's public route **without the credential**. The address of the person who sends it goes along as `X-Forwarded-For`, which an engine that trusts this server as a proxy counts its limits by. The engine's refusals are answered as 404 (unknown or revoked link), 422 with the engine's reason in one line (values refused, process not executable) and 503 with `Retry-After` (busy). A process that holds a public wizard is refused before anything is sent, when the request names it.
- **What the reader sees:** the block's head names the process to be started; the form opens filled in beforehand where the editor chose, and a line names those fields and what they were filled in with; below the button a line says that the engine learns what is entered, not who sends it. **Send** checks the form as form-js does and sends only a form without errors; the block then thanks the reader and offers to fill it in again. While the text is edited, the form is shown as it opens and is not sent.
- **Filling in beforehand:** a field a person types text into (a text field or a text area, not a field of a list of entries) may be filled in with the reader's username (`username`), the reader's own mail address (`email`), or the title (`page`) or address (`url`) of the page the block is on. The editor chooses per field, in a dialog that opens when the block is placed, proposing what a field's key or label hints at (`assets/engineparts.js`, `suggestPrefill`), and later through *Fill in beforehand …*. The values are read in the reader's browser from the reader's own account and the page, only those asked for, and become the form's starting data: the reader sees them and may change them, and nothing is sent before *Send*.
- **A process with a public wizard** is not started from the page: its filler would be led on in the engine's own pages with a pass this page cannot hand over. The block says so and links to the engine's page of the link, which opens in a new tab. Where the block names no process and no credential can tell, the start's answer says it, and the block says that the process carries on in the engine's pages, which it cannot continue.
- **The audit trail** records each start for the person who sent it, with the engine and the link's first eight digits, which start nothing (`engine.start`). The engine does not learn who it was.
- **Exports** name a start form by its process, or by its caption when it names none, "[Start form: incident (atlas)]", and never write the link, since anyone who holds it starts the process. **Not an MCP tool**, as for processes (ADR-0012, ADR-0129); the description of the body format names the block kind and the form block's new fields.

### Consequences

- **Positive:**
  - The form picker offers what processes use, and a form is documented with its fields as a process is with its tasks.
  - A page about a service starts its processes: reporting an incident or entering holidays happens where the instructions are, with the reader's name and the page filled in.
  - The engine's credential never travels with a start, and the UI's policy, the item model's rules and the engine's own checks stay as they are; the start is what anyone with the link may do.
- **Negative / trade-offs accepted:**
  - The token is in the body, so every reader of the page, and the page's history, holds it. That is a public link's nature; revoking it at the engine is the control.
  - Every signed-in person may send any well-formed link of a configured engine through this server, not only those placed on pages they read: the server adds nothing the link does not give anyone (open question 1).
  - When the engine does not trust this server as a proxy, all its readers share one budget of the engine's limit, 20 at once and one a second; a busy page is then answered 503 and asks to try again.
  - A prefilled username is not an identity: the reader may change it, and the engine cannot trust it.
  - A pasted link the credential cannot place in a process is not checked for a wizard before it is sent; a wizard started from it waits for a pass nobody holds until its time runs out.
  - Each view of a form of a process reads the process list, the model and the form; a start form reads the form and, with its process, the list and the model; a start reads them again for the wizard. There is no cache.
- **Follow-ups / risks to watch:**
  - **Internal start forms** for signed-in readers or certain groups, with the reader as initiator, need the engine to accept the reader's identity: per-person credentials (atlas ADR-0200 binds its OAuth tokens to its MCP endpoint today) or a start on behalf of a named person from a trusted system. That is option 4, with the engine's side decided first.
  - **User tasks** (use 3): a list of the tasks a filter selects, opened in a dialog, and tasks as cards of a kanban, need the engine's tasks as a reader may see them, which atlas filters per viewer; a credential's account would show its own tasks to everyone. A record of its own.
  - The wizard's first step could be shown here if the engine offered to hand its pass to the page that started it.

## Pros and cons of the options

### Option 2: the browser calls the engine's public routes itself

Strongest form: no relay at all. The engine sees each reader's own address and counts its limits per reader without trusting any proxy; this server carries no load and no responsibility for starts; atlas already allows cross-origin calls for listed origins (atlas ADR-0186).

- Good: the engine's limits work as designed; nothing passes through this server.
- Bad: the UI's policy would have to allow each engine in `connect-src`, a per-installation exception the policy of ADR-0031 does not make for anything; the engine's operator would have to list this site's origin; an engine in the server's own network (`WWBB_ENGINE_ALLOW_PRIVATE`) is often not reachable from readers' browsers at all. And the browser would build the engine's path from the body without this server's check of the token's form. Not adopted.

### Option 3: a link to the engine's own page

Strongest form: no new code path at all; the engine's page is the engine's own and handles everything, wizards included, exactly as the engine means it; nothing of the reader passes through the wiki.

- Good: complete, simple, and always current with the engine.
- Bad: the reader leaves the page; nothing is filled in beforehand; the form cannot be read beside the instructions that explain it. Kept for processes with a public wizard, where it is the only way that works.

### Option 4: a start with the credential, naming the reader

Strongest form: the start would carry who started it, internal forms and forms for certain groups would be possible, and the engine's records would be complete. It is what the requester wants in the long run.

- Good: identity and access at the engine.
- Bad: with the installation's credential, the engine would record the credential's account as the initiator of every start, or trust a name this server asserts, which the engine has no way to check. Per-person credentials do not reach the engine's start routes today. Faking identity now would be worse than staying anonymous. A follow-up, once the engine decides how it accepts a person from another system.

### Option 5: the engine's page in a frame

Strongest form: exactly the engine's page, wizards included, inside the wiki's page.

- Good: fidelity, and the wizard works.
- Bad: the UI's policy would have to allow each engine as a frame source and the engine would have to allow being framed; the frame's storage is partitioned by browsers, which may break the wizard's pass; nothing could be filled in beforehand. ADR-0129 rejected its option 5 and ADR-0144 its option 3 for the same reasons.

## Open questions

1. **Binding a start to a block.** The start route takes the link from the request: any signed-in person may send any well-formed link of a configured engine, and the link is in the body every reader receives. Taking the item and the block instead, and reading the link from the body the reader may read, would keep the link from the browser and let readers start only what is placed on a page they read. It is not adopted: the link is public by design, the engine's page of it is shown for wizards anyway, and the route would depend on the item store. If links are meant to be less public than their name says, this is the change.
2. **Recording starts.** The audit trail records who sent a start the engine records as anonymous, so that an administrator can trace abuse through this server. That re-identifies, for the installation's administrators, what the reader may believe anonymous; the hint below the button speaks of the engine only. Whether to keep the record, and whether to say so in the hint, is the maintainer's decision.

## Links

- extends [ADR-0144](0144-forms-and-decisions-from-a-process-engine.md) (forms and decisions from an engine)
- relates to [ADR-0012](0012-mcp-adapter-over-http-api.md) (MCP), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0027](0027-local-accounts-sessions-and-api-tokens.md) (API tokens), [ADR-0031](0031-web-ui-shell.md) (the UI and its policy), [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) (processes from an engine), [ADR-0143](0143-a-diagram-describes-its-tasks-and-decisions.md) (the description of a process)
