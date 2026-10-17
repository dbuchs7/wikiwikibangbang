# ADR-0014: User content is rendered by one closed renderer, never as raw HTML

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17

## Context and problem statement

Every page, issue description and comment is text written by users — and by agents that may have read untrusted input. Rendering that text in the browser of other users is the main cross-site scripting risk of a wiki. Confluence-class products also import foreign content (Confluence storage format, Jira descriptions, pasted HTML).

Adapted from atlas [ADR-0250](https://github.com/pblumer/atlas/blob/main/docs/adr/0250-documentation-is-markdown.md). Atlas renders Markdown; here the stored format is a structured block document (ADR-0001), but the safety principle carries over unchanged.

## Decision drivers

- Safety must be a structural property of the renderer, not a sanitizer configuration.
- One renderer for every surface (page view, preview, notifications, export), so a fix applies everywhere.
- Imported and pasted content cannot smuggle markup through.
- The renderer is small enough for a reviewer to understand completely.

## Considered options

1. **Store and render HTML, sanitise on output** (e.g. an allowlist sanitizer)
2. **A general Markdown library plus a sanitizer**
3. **One closed renderer that builds every element itself from the structured document**

## Decision outcome

Chosen option: **3.**

- The renderer walks the block document of ADR-0001 and **creates every element itself**. Text is always inserted as text; no fragment of user content is ever interpreted as markup. Raw HTML in content renders as visible text.
- The only place user text reaches an attribute is a link target, guarded by an **allowlist**: `http`, `https`, `mailto` and same-origin paths. Everything else, including `javascript:` and `data:`, loses its link and renders as its label.
- **Import and paste** convert foreign formats into the block document on the server; unknown constructs become plain text or a visible "unsupported content" block, never pass-through markup.
- The same renderer (or its server-side twin for exports and notifications) is used on every surface; its safety properties are tested against a real DOM.
- A strict content-security policy is the second line of defence, not the first.

**Implementation so far** ([ADR-0031](0031-web-ui-shell.md)): the UI creates every element through `assets/dom.js`, inserts text only as text and allowlists link targets; a test forbids markup-from-string APIs; a browser test saves hostile titles and bodies and fails on any dialog. Not built: a server-side renderer for exports and notifications.

### Consequences

- **Positive:** cross-site scripting through content is impossible by construction; the security argument fits in a few paragraphs and is reviewable.
- **Negative / trade-offs accepted:** every supported construct (tables, images, macros, embeds) must be added to the renderer deliberately; importers must map foreign content explicitly, and some formatting is lost on import.
- **Follow-ups / risks to watch:** embedded content (images, live queries, item embeds) needs its own rules for what may be fetched and from where.

## Pros and cons of the options

### Store HTML, sanitise on output

Strongest form: HTML is what browsers and importers speak; mature sanitizers exist and are battle-tested.

- Good: maximum fidelity of imported content.
- Bad: safety rests on a configuration that is a blocklist by nature; every new browser feature is a potential bypass.

### Markdown library plus sanitizer

Strongest form: established libraries cover the full CommonMark surface with little effort.

- Good: fast to adopt.
- Bad: large vendored code whose correctness we depend on but do not own; raw HTML is part of CommonMark and must be removed again by configuration.

### Closed renderer

- Good: structural safety; small and owned.
- Bad: every construct is work.

## Links

- atlas [ADR-0250](https://github.com/pblumer/atlas/blob/main/docs/adr/0250-documentation-is-markdown.md)
- relates to [ADR-0001](0001-unified-item-data-model.md) (block document), ADR-0009 (UI)
