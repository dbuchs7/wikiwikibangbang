# ADR-0109: A design logo for the dark look, and the product's icon in the browser tab

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that the browser tab always show the product's icon, that an own logo stay possible, and that a logo for a dark background can be uploaded, 2026-10-23)

## Context and problem statement

The corporate design (ADR-0052) carries one logo. It replaced the product's logo in the top bar and was also set as the browser's icon. The dark look (ADR-0095) keeps the product's dark header colours, so a logo drawn for a light background is hard to read there. The product's own logo has a light and a dark variant (docs/brand) and switches with the look.

Two questions follow. Which icon does the browser tab show under a design logo? And how does an installation give its logo a variant for the dark look?

## Decision drivers

- The tab icon identifies the product among many open tabs. A design logo, usually a wide wordmark, is unreadable at 16 px.
- A design logo has to stay readable in both looks, as the product's logo is.
- The look can be chosen with the bulb as well as taken from the system, and the logo has to follow either.
- An installation that uploads no second logo should see no change.

## Considered options

1. A second design file, `logo-dark`, shown in the dark look in place of `logo`.
2. One logo, inverted or lightened by a CSS filter in the dark look.
3. One logo on a light plate in the dark header.

## Decision outcome

Chosen option: **1, a second design file `logo-dark`**, because only the organisation knows what its logo looks like on a dark background.

- `logo-dark` is a design asset like `logo`, with the same formats and limits, set and removed by administrators (`PUT`/`DELETE /api/branding/logo-dark`) and named in `/branding.json`.
- With both set, the top bar shows a `<picture>` whose `<source media="(prefers-color-scheme: dark)">` is the dark logo. A look chosen with the bulb rewrites that media query as it does for the product's logo (ADR-0095).
- Without `logo`, `logo-dark` is not used: the product's logo, with its own dark variant, shows.
- With the dark look turned off (ADR-0070), the dark source is dropped and `logo` shows.
- The browser's icon is always the product's (`favicon.svg`, `favicon.ico`), whatever the design.

### Consequences

- **Positive:** a design logo stays readable in the dark header. The tab is recognisable as the product's in every installation.
- **Negative / trade-offs accepted:** an organisation can no longer put its own icon in the tab. A logo uploaded only for the dark look has no effect until a logo for the light look is set too; the design form says so.
- **Follow-ups / risks to watch:** the start page's own picture (ADR-0095) still has a single variant.

## Pros and cons of the options

### Option 1: a second design file

Strongest form: the organisation supplies exactly the artwork its brand guide prescribes for dark backgrounds.

- Good: faithful to the brand; works for any colours and for raster formats.
- Bad: one more file to upload and keep in step with the first.

### Option 2: a CSS filter in the dark look

Strongest form: no upload at all; `filter: invert(1)` or a brightness change makes most monochrome wordmarks readable on dark.

- Good: nothing to configure.
- Bad: inverts brand colours into wrong ones; fails for multi-coloured logos and photographs; the result is not what any brand guide allows.

### Option 3: a light plate behind the logo

Strongest form: the original logo is shown unchanged, as many sites do with partner logos.

- Good: no second file, colours stay right.
- Bad: a white box in a dark header; looks like a foreign element in the product's own chrome.

## Links

- amends [ADR-0052](0052-corporate-design-in-a-journal-of-its-own.md) (the design's files)
- relates to [ADR-0095](0095-language-and-look-in-the-header-and-an-own-start-picture.md) (the chosen look), [ADR-0070](0070-an-own-stylesheet-without-a-dark-look-turns-the-dark-scheme-off.md) (the dark look turned off)
