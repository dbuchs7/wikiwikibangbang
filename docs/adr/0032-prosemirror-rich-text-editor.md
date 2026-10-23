# ADR-0032: The rich-text editor is ProseMirror, vendored as one pre-built bundle

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17: "take an open-source component if available"); accepted by the maintainer 2026-10-17

## Context and problem statement

ADR-0009 leaves open whether a rich-text editor can be shipped as one vendored asset without making npm part of the Go build. ADR-0031 edits bodies as one textarea per block and cannot create mentions or embeds. The body model knew only paragraphs, headings without levels, embeds and imported unsupported blocks: no lists, quotes, code or inline formatting. This record decides the editor, how it is shipped, and how the body model grows to carry what it edits.

## Decision drivers

- Reuse a mature, maintained open-source editor instead of writing one (ADR-0009).
- `go build` stays the whole build; Node is needed only to update the editor.
- The stored body stays structured data, never HTML (ADR-0014); what the editor can express is exactly what the model can store and validate.
- Mentions and embeds stay references to items, so relations, deletion checks and permission filtering keep working (ADR-0020, ADR-0021, ADR-0022).
- The editor's licence is compatible with AGPL-3.0 and recorded.

## Considered options

1. **ProseMirror**, vendored as one bundle built with esbuild
2. **Tiptap**, ProseMirror with a higher-level extension API
3. **Lexical** or **Quill**
4. **contenteditable written by hand**

## Decision outcome

Chosen option: **1**.

- **Shipping:** `internal/web/vendor/` pins the ProseMirror packages (model, state, view, commands, history, keymap, inputrules; all MIT) and esbuild in `package.json` and `package-lock.json`. `build.sh` writes `internal/web/static/vendor/prosemirror.js` (one minified ES module, about 200 KB) and `prosemirror.LICENSE.txt` (packages, versions, licence text). Both are committed and embedded; `go build` never runs Node. `THIRD_PARTY_NOTICES.md` lists every bundled package, and `TestEveryVendoredPackageIsNoticed` compares it with the lock file. The bundle is served under the public route `/vendor/`.
- **Editor:** `assets/editor.js` defines a ProseMirror schema that mirrors the body model one to one, converts blocks to a document and back, and adds a toolbar (block kind, bold, italic, code, link, mention, embed, undo, redo), keyboard shortcuts and Markdown-like input rules (`#`, `-`, `1.`, `>`, three backticks). Mentions and embeds are created by entering an item key; the UI resolves it through the API, so only items the user can read can be referenced. Imported unsupported blocks are shown and can be kept or removed, never created.
- **Model (codec version 3):** new block kinds `bullet`, `ordered`, `quote` and `code`; `Level` (1–3) on headings; `Marks` on a block — `bold`, `italic`, `code`, `link` (with `Href`) and `mention` (with `Ref`) — as ranges in Unicode code points over the block's text. The store validates every block: known kind, level only on headings, no marks on code or unsupported blocks, ranges within the text, link targets on the allowlist (`http` and `https` with a host, `mailto`), and every mention mark's item listed in the block's mentions. Versions 1 and 2 still replay; golden files pin version 3.
- **Import:** the Confluence storage-format converter (ADR-0023) maps `ul`, `ol` and `blockquote` to the new kinds and keeps heading levels (capped at 3).
- **Closed rendering:** the editor builds DOM only from schema specs, never from strings. Pasted HTML is parsed by ProseMirror into a detached document, where nothing runs, and only what the schema allows survives; link marks additionally pass the allowlist. `TestNoMarkupFromStrings` exempts `vendor/` for that reason; `editor.js` is checked like every other UI file. The stored form is validated on the server, so a hostile client gains nothing by bypassing the editor.
- **Test:** `e2e/smoke.cjs` writes a body with a level-2 heading, a list, bold text, a hostile `<script>` string, a mention and an embed, saves it, reloads and checks that the body is unchanged and that the mention created a relation.

This answers the open questions of ADR-0009 (yes, as one vendored asset) and ADR-0031 (ProseMirror; mentions and embeds through the toolbar by item key).

**Implementation is Partial:** no tables, images, nested lists or task lists; no `@` autocomplete; no collaborative editing (ADR-0010); the bundle is not rebuilt in CI to prove it matches the lock file.

### Consequences

- **Positive:** a real editor with undo, shortcuts and paste handling, without a bundler in the product build; the body model gains lists, quotes, code and inline formatting with server-side validation; ProseMirror's transaction model is the usual basis for collaborative editing (ADR-0010).
- **Negative / trade-offs accepted:** a 200 KB third-party artifact in the repository that must be rebuilt and reviewed on every update; the schema exists twice (Go validation and the editor) and must be kept in step; code-point offsets must be converted from JavaScript's UTF-16 strings.
- **Follow-ups / risks to watch:** a CI job that rebuilds the bundle and fails on a difference; nested lists and tables need a model change, not only an editor change; security advisories for the pinned packages.

## Pros and cons of the options

### ProseMirror

- Good: mature, widely used (it is the base of Tiptap and of many Confluence-class editors), MIT, schema-driven — the schema enforces the same closed set of blocks the server accepts; modular, so only the needed packages are bundled.
- Bad: low-level; toolbar and conversions are written here.

### Tiptap

Strongest form: ProseMirror with ready-made extensions for lists, tables, mentions and collaboration, and a much smaller amount of own code.

- Good: faster to add features; same engine underneath.
- Bad: a larger dependency tree and bundle; parts of the extension ecosystem are commercially licensed; the extension abstraction hides the schema that must match the Go model exactly. It can be adopted later without changing the stored format, because the schema is ProseMirror's.

### Lexical or Quill

Strongest form: Lexical is modern, fast and backed by a large company; Quill is simple and has a stable delta format.

- Good: both are permissively licensed and capable.
- Bad: Lexical is designed around React for most of its ecosystem; Quill's flat delta format fits block-structured bodies with embeds less well; neither is the basis of a collaboration stack as established as ProseMirror's.

### contenteditable by hand

Strongest form: no dependency at all, full control, no vendored artifact.

- Good: nothing to vendor.
- Bad: browser `contenteditable` behaviour differs between engines (selection, paste, IME input); getting it right is what editor libraries exist for, and ADR-0009 says to reuse them.

## Links

- amended by [ADR-0096](0096-links-mentions-and-embeds-as-in-a-word-processor.md) (links, mentions and embeds as in a word processor)
- answers the open questions of [ADR-0009](0009-single-binary-with-embedded-web-ui.md) and [ADR-0031](0031-web-ui-shell.md); keeps [ADR-0014](0014-closed-renderer-for-user-content.md); extends the body model of ADR-0001 and ADR-0020 and the import of ADR-0023; prepares ADR-0010
