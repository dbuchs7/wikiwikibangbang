# ADR-0009: A single binary with an embedded web UI

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17

## Context and problem statement

The product needs a server (API, persistence) and a web UI (editor, boards, search). How they are packaged decides how hard the product is to install, operate and contribute to.

Adapted from atlas [ADR-0011](https://github.com/pblumer/atlas/blob/main/docs/adr/0011-single-binary-distribution-and-web-ui.md), [ADR-0012](https://github.com/pblumer/atlas/blob/main/docs/adr/0012-web-ui-app-shell.md) and [ADR-0013](https://github.com/pblumer/atlas/blob/main/docs/adr/0013-embed-bpmn-js-modeler.md).

## Decision drivers

- One artifact and one command from download to a running server.
- `go build ./...` stays the whole build for contributors who do not touch the editor.
- Heavy, solved widgets (rich-text editor) are reused, not written by hand.
- Nothing is fetched from a CDN at runtime; the product works offline and on isolated networks.

## Considered options

1. **Separate front-end application** (SPA built with a framework and bundler, served separately or by a reverse proxy)
2. **Single binary; UI built with a framework and bundler, output embedded**
3. **Single binary; buildless UI shell in plain ES modules, with heavy widgets vendored as pre-built assets** (atlas)

## Decision outcome

Chosen option: **3.**

- The server, the API and the UI ship as **one binary**; web assets are embedded with `go:embed`.
- The UI shell (routing, navigation, views for pages, issues, boards, search) is **plain HTML, CSS and ES-module JavaScript** without a bundler or framework.
- The **rich-text editor** is the exception: a pre-built bundle of an established editor is **vendored** into the repository as one asset, with its license, and updated in a dedicated step. npm is a prerequisite only for that step, never for `go build`.
- Should the UI outgrow the buildless shell, a bundler may be introduced by a new ADR; the output is still embedded, so the single-binary property holds.

**Implementation so far** ([ADR-0024](0024-release-targets-and-container-image.md), [ADR-0031](0031-web-ui-shell.md)): one binary for every platform; a buildless UI shell over the item API; the rich-text editor, ProseMirror, vendored as one pre-built bundle ([ADR-0032](0032-prosemirror-rich-text-editor.md)), which answers this record's former open question.

### Consequences

- **Positive:** trivial installation and upgrades; no runtime dependencies; the Go build stays self-contained; consistent with atlas.
- **Negative / trade-offs accepted:** more manual DOM code than with a framework; the vendored editor bundle is an opaque artifact that must be rebuilt and reviewed on updates; content-security policy and asset embedding must be maintained.
- **Follow-ups / risks to watch:** ADR-0032 showed that option 3 is feasible for the editor; the editor UI is likely the largest front-end component and the strongest pressure towards a bundler.

## Pros and cons of the options

### Separate front-end application

Strongest form: the industry standard for rich web applications; frameworks, component libraries and tooling make a Jira/Confluence-class UI much faster to build, and front-end contributors expect it.

- Good: fastest UI development; large ecosystem.
- Bad: two artifacts to deploy and version; npm in every build; contradicts the single-binary goal.

### Single binary, bundled UI

Strongest form: keeps one artifact while still using a framework.

- Good: framework productivity and single binary.
- Bad: every contributor needs the Node toolchain; build complexity and dependency churn enter the Go repository.

### Single binary, buildless shell, vendored widgets

- Good: simple build and deployment; reuse of the editor.
- Bad: more hand-written UI code; vendored bundles to maintain.

## Links

- atlas [ADR-0011](https://github.com/pblumer/atlas/blob/main/docs/adr/0011-single-binary-distribution-and-web-ui.md), [ADR-0012](https://github.com/pblumer/atlas/blob/main/docs/adr/0012-web-ui-app-shell.md), [ADR-0013](https://github.com/pblumer/atlas/blob/main/docs/adr/0013-embed-bpmn-js-modeler.md)
- relates to ADR-0004 (Go without CGO), ADR-0010 (real-time collaboration), ADR-0014 (rendering)
