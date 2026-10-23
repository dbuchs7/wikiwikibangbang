# ADR-0052: The corporate design is kept in a journal of its own and applied as a stylesheet over the product's

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that an organisation can show its corporate design: logo, background colour or image, fonts, by hand or with an own stylesheet, 2026-10-20)

## Context and problem statement

An installation is run by one organisation, whose people expect its logo, colours and typeface. The UI is a fixed stylesheet built on custom properties (`--accent`, `--bg`, `--font-sans`, …). Where are the design's settings and files kept, who may change them, and how do they reach the browser without loosening the content security policy?

## Decision drivers

- Administrators change the design in the UI; nothing is edited on the server's disk.
- The design is part of a backup and survives a restore.
- No inline styles, no external fonts or images: the policy stays `default-src 'self'`.
- The login page carries the design too, so the design is public. It is no secret.
- A design that is valid today still loads after an upgrade.

## Considered options

1. **A journal of its own** (`site.wal`): events `DesignSet`, `AssetSet`, `AssetRemoved`, replayed at start like the accounts journal (ADR-0027); the server renders a stylesheet `/branding.css` from them.
2. **Files in the data directory** (`branding/logo.png`, `branding/custom.css`) that an operator places by hand.
3. **Settings as an item** in a system space, using the item model's events and encryption.

## Decision outcome

Chosen option: **1**.

- **What can be set:** a name (shown beside the logo and in the browser's title), the accent colour, the page background, the header's background and text colour, a font (one of several system font stacks, an installed font by name such as "Frutiger", or an uploaded font file), and an own stylesheet. Files: a logo (PNG, JPEG, GIF, WebP or SVG), a background image (PNG, JPEG, GIF, WebP) and a font (WOFF2, WOFF, TTF, OTF), each at most `budget.BrandingBytes`.
- **By hand or as a stylesheet:** both, in the same section *Appearance* of the administration. The fields cover the usual case; the own stylesheet, typed or loaded from a `.css` file, is appended last and may override anything.
- **Rendering:** `GET /branding.css` is generated from the settings: custom properties at `:root`, then the light-scheme colours (background, header) inside `@media (prefers-color-scheme: light)`, so the dark scheme stays readable, then the background image and the own stylesheet. It is linked from `index.html` after the product's stylesheet; an ETag lets browsers keep it.
- **Readable text on the accent:** the accent applies in both schemes, so the text on accent fills (`--accent-ink`) follows from the accent alone: white or near black, whichever has the higher contrast ratio (WCAG 2.x); hovering moves the fill away from the text, so the contrast only grows. The product's own palette had a dark ink for its lighter dark-scheme blue, which made dark corporate accents unreadable in the dark scheme.
- **The dark scheme's accent:** most corporate colours are too dark to read as text on the dark scheme's cards; the dark scheme gets the accent mixed with white in steps of 5 % until it reaches 4.5 : 1 there, as the product's own palette pairs a lighter blue with its dark scheme. The hue stays; an accent that reads already is kept.
- **Contrast warnings:** the administration names the pairs of colours below WCAG AA (4.5 : 1): header text on its background, text on the page background, text on accent buttons, the accent as link text on cards, in the dark scheme and on the page background. It warns and saves: the colours are the organisation's decision, and an own stylesheet may change what the pairs are.
- **Public reads, administrators write:** `GET /branding.css`, `GET /branding/{asset}` and `GET /branding.json` need no session; `PUT`/`DELETE /api/branding…` need an administrator. Every change is audited (`design.change`).
- **Files are served inertly:** with `nosniff` and a policy of `default-src 'none'; sandbox`, so an SVG logo opened directly cannot run script. In the page it is shown through `<img>`, which never runs script.
- **Backups:** `site.wal` is an optional backup file: included when present, not required, so backups from before this record still restore.

### Consequences

- **Positive:** the policy is unchanged; an own stylesheet cannot run script and cannot load from elsewhere, as `default-src 'self'` applies to its `url()`s too.
- **Positive:** the design is restored with everything else, and replay is the same as for accounts.
- **Negative / trade-offs accepted:** an own stylesheet can hide or misplace parts of the UI; it is an administrator's tool, and removing it restores the product's look. Its selectors are not a stable interface across releases.
- **Negative / trade-offs accepted:** every version of an uploaded file stays in the journal; changes are rare and files small, so it is accepted.
- **Follow-ups / risks to watch:** a dark-scheme variant of the colours, if asked for.

## Pros and cons of the options

### Option 2: files in the data directory

Strongest form: nothing to build for storage; an operator who deploys with configuration management can keep the design in the same repository as the rest.

- Good: no journal, no upload endpoints.
- Bad: needs access to the server; not visible or changeable in the UI; not in the application's backup unless added by hand.

### Option 3: an item in a system space

Strongest form: history, access and encryption already exist for items.

- Good: versioning for free.
- Bad: the design must be readable without a session (login page), which an item is not; items are encrypted per space, the design is public by nature. It would bend the item model for something that is not content.

## Links

- amended by [ADR-0095](0095-language-and-look-in-the-header-and-an-own-start-picture.md) (an own picture on the start page)
- relates to ADR-0014 (no markup from data), ADR-0027 (accounts journal), ADR-0045 (files served inertly), ADR-0050 (profile pictures)
- amended by [ADR-0085](0085-ready-made-themes-under-the-corporate-design.md) (ready-made themes under the design)
