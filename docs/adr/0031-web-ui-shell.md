# ADR-0031: The first web UI is a buildless shell over the item API, tested in a real browser

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17); accepted by the maintainer 2026-10-17

## Context and problem statement

ADR-0009 proposes a single binary whose UI is plain HTML, CSS and ES modules without a bundler. ADR-0014 requires that user content is never interpreted as markup. Until now the binary served a placeholder; everything was reachable only through the API (ADR-0028). This record decides the shape of the first UI and how it is tested.

## Decision drivers

- Everything the API offers to a person is usable without curl.
- User content never runs as code: not through markup, not through links, not through attributes.
- `go build` stays the whole build; no Node toolchain is needed to build or run the product.
- The UI is tested where it runs: in a browser, against the real binary.

## Considered options

1. **Server-rendered HTML** from Go templates, with forms
2. **A buildless ES-module shell** calling the JSON API
3. **A framework application** (React, Svelte) built with a bundler and embedded

## Decision outcome

Chosen option: **2**, as ADR-0009 proposes.

- **Shape:** `index.html`, `assets/app.js` (hash routes and views), `assets/api.js` (the API client), `assets/dom.js` (the only element factory), `assets/style.css`. Views: login (password and, when configured, single sign-on), spaces, a space's items with paging and an item form, an item (title, properties by field kind, transitions, body blocks, relations, history, trash, restore and delete), account (password, API tokens), administration (spaces, types, users, space roles, audit trail).
- **Closed rendering (ADR-0014):** `dom.js` creates every element and inserts text only as text; link targets pass an allowlist (`#`, same-origin paths, `http`, `https`, `mailto`). A Go test fails the build if any UI file uses `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `eval`, `new Function`, inline event attributes or inline scripts. The content-security policy (`default-src 'self'`) is the second line.
- **Body editing:** paragraphs and headings are edited as text; embeds are shown as links (or as "an item you cannot see") and can be removed; mentions are kept on their blocks and shown as links. Creating mentions and embeds is left to the rich-text editor, which [ADR-0032](0032-prosemirror-rich-text-editor.md) has since added; it replaces the block textareas. Bodies are now edited in a live session ([ADR-0010](0010-real-time-collaboration-with-block-locks.md)) and saved when a block is left, so there is no save button.
- **Browser test:** `e2e/smoke.cjs` drives Chromium through Playwright against the built binary — log in, administration, create an item with a hostile title, edit properties, transition, save a body containing a `<script>`, create a token, log in as another user — and fails on any page error, console error, CSP violation or dialog. CI runs it as the job `e2e (browser)`. Node and Playwright are test tools only; `e2e/package-lock.json` pins them.

**Implementation is Partial:** history shows user IDs, not names, for non-administrators; no live updates; no search.

### Consequences

- **Positive:** the product is usable in a browser from the single binary; the UI cannot drift from the API because it has no other data source; XSS defences are enforced by a test and exercised by a browser.
- **Negative / trade-offs accepted:** hand-written DOM code; textareas instead of a real editor (until ADR-0032); the browser test adds a Node toolchain to CI (not to the build) and about a minute per run.
- **Follow-ups / risks to watch:** showing names instead of IDs needs a user lookup readable by non-administrators; accessibility review; the test is not yet a required check.

## Pros and cons of the options

### Server-rendered HTML

Strongest form: no JavaScript needed, works in any browser, simplest security model (templates escape by default), progressive enhancement later.

- Good: robust and fast to first paint.
- Bad: a second interface next to the JSON API, with its own CSRF handling for forms; collaborative editing (ADR-0010) will need client-side code anyway.

### Buildless ES-module shell

- Good: one interface (the API); no build step; consistent with ADR-0009 and atlas.
- Bad: more manual DOM code.

### Framework with a bundler

Strongest form: the fastest way to a rich, componentised UI; the editor ecosystem (ProseMirror, Tiptap) is built for it.

- Good: productivity, components, the editor ecosystem.
- Bad: Node in every build; ADR-0009 rejected it for now.

## Links

- implements part of [ADR-0009](0009-single-binary-with-embedded-web-ui.md) and [ADR-0014](0014-closed-renderer-for-user-content.md); uses ADR-0027, ADR-0028, ADR-0029, ADR-0030
