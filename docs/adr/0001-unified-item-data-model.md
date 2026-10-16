# ADR-0001: Unified item data model for knowledge and work

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Is a space itself an item or a separate container? How are type schemas migrated once instances exist? (see "Open questions")
- **Question checked:** 2026-10

## Context and problem statement

wikiwikibangbang aims to combine the capabilities of Confluence (knowledge: pages, page tree, comments, versions) and Jira (work: issues, fields, workflows, boards, queries) in **one** product.

Broken down into their fundamental components, the two products look like this:

| Component | Confluence page | Jira issue |
|---|---|---|
| Stable identity and human-readable key | yes (title/ID) | yes (`PROJ-123`) |
| Container | space | project |
| Hierarchy | page tree | epic → story → sub-task |
| Free-text content | primary (rich document) | secondary (description) |
| Structured properties | few (labels, owner) | primary (fields) |
| Lifecycle | rare (draft/published) | primary (workflow) |
| Relations to other objects | links, mentions | issue links |
| Comments, attachments, watchers, history, permissions | yes | yes |

The two object kinds do not differ in **kind** but in the **weighting** of the same components. Atlassian nevertheless models them as two separate systems linked to each other. The result is duplicated implementations of search, permissions, history and comments, and an integration that stops at the system boundary: a checklist on a page does not become an issue, an issue has no workflow for its documentation, and a decision (ADR) has no home in either.

The question is: **which data model underlies all user-generated content?** This decision shapes search, permissions, history, import, API and UI, and is very expensive to change later.

## Decision drivers

- **Genuine unification:** cross-cutting capabilities (search, permissions, history, comments, relations) exist exactly once.
- **Structured queryability** at Jira level (fields, states, aggregation for boards and reports).
- **Collaborative editing** of documents at Confluence level, without putting structured data at risk.
- **Determinism and traceability:** every change is a named event with an author and a timestamp.
- **Comprehensibility for users:** the model must not surface in the UI as "everything is a generic block".
- **Migratability** from Confluence and Jira without loss of information for the core objects.

## Considered options

1. **Two domain models** (`Page`, `Issue`) with links between them (the Atlassian approach)
2. **Everything is a block** (the Notion approach): pages, paragraphs and database rows are blocks in one tree
3. **Generic graph** (triple/property graph without a fixed schema)
4. **A typed item model:** every object is an *item* with a type, properties, an optional document body, an optional lifecycle and typed relations

## Decision outcome

Chosen option: **option 4, a typed item model.**

### Structure of an item

```text
Item
├── id            immutable ID (ULID), globally unique
├── key           human-readable alias per space (e.g. "PLAT-42", "operations-handbook")
├── type          reference to a versioned type definition (page, task, bug, epic, decision, …)
├── space         exactly one space (container, permission boundary)
├── parent        at most one parent item (containment tree)
├── properties    typed field values, validated against the type's schema
├── state         current lifecycle state (only if the type has a workflow)
├── body          optional structured document (block document; not HTML/Markdown as storage format)
├── relations     typed, directed edges to other items (records of their own)
└── universal     comments, attachments, watchers, labels, history, permissions
```

### Core decisions within the model

1. **Types are data, not code.** A type definition specifies fields, required fields, workflow and presentation. The system ships built-in types (`page`, `task`, `bug`, `epic`, `decision`) with opinionated defaults. Custom types are built on the same mechanism. An item is therefore not "page or issue"; it carries a type that defines how much weight document, fields and workflow have.

2. **Two speeds of change.**
   - *Properties, state and relations* change discretely: each change is a single validated event.
   - *The document body* changes continuously (collaborative typing). Fine-grained edits are merged in a dedicated collaboration layer and committed to the authoritative history only at coarse intervals, as a **version**.

   The two paths are never mixed. A typing session therefore cannot alter a field value, and the event history is not inflated per keystroke.

3. **Structured data lives in properties, not in the document.** Anything that must be queried, filtered or aggregated is a property. The document body may *reference* items (embedded issue, live query, mention) but must not hold their authoritative data. Mentions in the document produce **derived relations**, which are recomputed deterministically for every new version.

4. **Exactly one containment tree.** `parent` determines navigation and permission inheritance. All other hierarchies (epic → story, "documents", "blocks", "implements") are typed relations. A story can thus be created inside a page and still belong to an epic elsewhere, without two competing trees.

5. **Relations are records of their own** with type, direction, author and history, indexed from both ends. Relations across space boundaries are allowed. If the target is not visible to the viewer, only "restricted item" is shown, without title or key.

6. **State changes only through transitions.** If a type has a workflow, `state` is not a freely writable property. Transitions may have conditions and required fields. Pages can thus have workflows too (draft → review → published) without special-case logic.

7. **Cross-cutting capabilities exist once.** Comments, attachments, watchers, history, permission checks, full-text and field search, and notifications are implemented for `Item`, never for a single type.

8. **ID and key are separate.** The `id` never changes. The `key` is an alias and may change when an item moves between spaces. Old keys are kept as redirects.

### Derived invariants

These rules move into `docs/architecture/invariants.md` once this ADR is accepted:

- **D1 – One model for content:** user-generated content consists exclusively of items. No parallel domain object is introduced for a single content type. System objects (users, groups, spaces, type definitions) are exempt.
- **D2 – Stable identity:** references within the system (relations, document references, events) always use `id`, never `key`.
- **D3 – Validated changes:** properties are written only through schema-validated change commands, `state` only through transitions.
- **D4 – No shadow data in the document:** queryable data lives in properties. Relations derived from the document are a deterministic function of the document version.
- **D5 – One containment parent:** every item has at most one `parent`. Permission inheritance follows containment only.
- **D6 – Type-agnostic cross-cutting capabilities:** no type-specific code in search, permissions, history or comments.

### Consequences

- **Positive:**
  - Search, permissions, history, comments and API each exist once, for all content.
  - New object kinds (decisions, risks, runbooks, test cases) are created through configuration rather than new code.
  - Seamless transitions: a task on a page *is* an item and appears on the board without being copied. An issue can have a review workflow for its documentation.
  - One query language for all content, e.g. `type = decision AND space = PLAT AND relation(implements) -> status != done`.
  - The model fits an event-based store (every change is a named event) without committing to one yet.

- **Negative / trade-offs accepted:**
  - A generic model is harder to optimise than two specialised ones. Queries over type-specific fields require carefully designed indexing.
  - Type definitions must be versioned and migratable. This is a non-trivial subsystem of its own.
  - UX risk: the product must not feel "too generic". Mitigation: strong built-in types with dedicated views (page view, issue view, board) on top of the same model.
  - Separating the document body (collaboration layer) from properties (events) results in two persistence paths that must be reconciled consistently.

- **Follow-ups / risks to watch:**
  - ADR: persistence and event model (evaluate adopting event sourcing following the atlas pattern).
  - ADR: document body format and choice of CRDT/OT approach, including the availability of a mature Go implementation.
  - ADR: query language and indexing.
  - ADR: permission model (space roles, item restrictions, visibility of relations).
  - ADR: mapping of Confluence and Jira objects to types during import.

## Pros and cons of the options

### Option 1: Two domain models

Strongest form: specialised models are easy to understand, easy to optimise and match the mental model of existing users. Import from Atlassian is a 1:1 mapping. Jira and Confluence have shown for more than two decades that the approach works.

- Good: simple, specifically optimisable data structures; low modelling risk; direct import.
- Bad: every cross-cutting capability exists twice or needs a translation layer. Integration stops at the model boundary, which is exactly the state this project sets out to overcome. Without a unified model there would be no substantive reason for a new product.

### Option 2: Everything is a block

Strongest form: maximum flexibility. Notion shows that users accept documents and databases in a single model. Transclusion and arbitrary nesting come for free.

- Good: very flexible; uniform editing model; pages and records can be converted into each other without loss.
- Bad: the granularity is too fine. Paragraphs and work objects become peers. This makes Jira-level permissions, workflows and queries hard to achieve, and history is inflated per keystroke. The workflow side in particular is considered a weakness of Notion. This approach contributes the idea that the document body can embed and reference items. The paragraph itself, however, does not become an item.

### Option 3: Generic graph

Strongest form: any structure can be represented, relations are first-class, and the schema can evolve freely.

- Good: maximum expressiveness; relations at the core.
- Bad: without a schema there is no validation, no reliable forms and no efficient field queries. Complexity moves into every application layer. Option 4 adopts the first-class relations, not the absence of a schema.

### Option 4: Typed item model

- Good: genuine unification while preserving structure; cross-cutting capabilities implemented once; extensible types; fits event storage and a query language.
- Bad: higher up-front design effort (type system, schema migration, two speeds of change); optimising generic queries is more demanding.

## Open questions

1. **Is a space an item?** For: space home page, comments and history would come for free. Against: the space is the permission boundary and the key namespace, and recursion at this point complicates the permission model. Provisionally, a space is a separate container with an associated home item.
2. **Schema evolution:** how are existing items handled when a field is renamed, removed or changes type (lazy migration on read or explicit migration runs)?
3. **Type changes:** may an item change its type (page → decision, task → bug)? If so, by which rules are fields mapped?
4. **Granularity of document versions:** at what intervals is the state of the collaboration layer committed as a version (time, inactivity, explicit save)?

## Prototype findings (2026-10-16)

An in-memory prototype in [`internal/item`](../../internal/item) implements the model: types as data with fields and workflows, items with properties, state, block bodies and typed relations, every change as an event and the state as a replayable fold (ADR-0008). Its acceptance test creates a page in one space, a task inside it, a decision in another space, embeds the task and mentions the decision in the page body, and works the task through a board query — one item, no copy.

What the prototype confirmed:

- One `Item` type carries pages, tasks and decisions; history, relations and queries needed no type-specific code (D1, D6).
- Making `state` a reserved name and changing it only through transitions is simple to enforce and test (D3); transitions with required fields (`start` needs an `assignee`) fit naturally.
- Derived relations from the body work as a pure function of the body version (D4), and the replay test proves the whole state is reproducible from the log.

What it surfaced — decisions this record did not make explicit:

1. **Moving between spaces moves the subtree.** Because a parent and its children share a space, moving a page to another space moves all its descendants and gives each a new key; old keys keep resolving (D2). The prototype does this; it should be stated as a rule.
2. **Derived relations need deterministic identities.** A relation computed while applying an event cannot take a random ID, or replay diverges. The prototype derives it from `(from, type, to)`; hand-made relations get an ID frozen into their event.
3. **History includes relation changes.** A body commit on a page appears in the history of the task it embeds and of the decision it mentions. Useful ("mentioned in …"), but it means an item's history can grow through edits to other items.
4. **Embedding and containment overlap.** The task is both a child of the page (`parent`) and embedded in its body (`embeds`). If the task moves elsewhere, the embed remains. Whether embedding should imply or require containment is open.
5. **Schema evolution is now concrete.** Items record the type version they were created with, but validation uses the latest version; a field that becomes required in v2 is not enforced on items from v1 until they are next changed. This sharpens the open question above.
6. **Deletion is unmodelled.** What happens to embeds, mentions and relations pointing at a deleted item is undecided; the prototype has no delete.
7. **Title is intrinsic.** Every item has a title outside its properties, and names such as `title`, `state`, `parent` are reserved for fields.

A space as a plain container (not an item), as provisionally decided above, was sufficient for the prototype.

## Links

- Prototype: [`internal/item`](../../internal/item), acceptance test `TestScenarioPageWithEmbeddedTask`
- Reference project for working method and ADR discipline: [pblumer/atlas](https://github.com/pblumer/atlas) (in particular ADR-0001, event sourcing)
- Follow-up ADRs: persistence, document format/CRDT, query language, permission model, import (not yet written)
