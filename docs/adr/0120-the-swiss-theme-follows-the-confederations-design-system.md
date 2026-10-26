# ADR-0120: The Swiss theme follows the Confederation's design system

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked to align the Swiss theme with the style the Swiss Federal Administration publishes on GitHub for its external websites, 2026-10-26)

## Context and problem statement

The ready-made theme "Swiss" (ADR-0085) was a free interpretation: red, black and white, square corners and Helvetica. The Swiss Confederation publishes its own design system for the websites of the administration at github.com/swiss/designsystem, under the MIT license. Installations of the administration that pick "Swiss" expect that look, not an interpretation.

The design system defines, among others:

- **Colours:**
  - red as primary (600 `#d8232a`, used for links and outlined buttons);
  - a slate as secondary (500 `#46596b` for filled buttons, 600 `#2f4356` for the top bar);
  - grey text (800 `#1f2937`, 500 `#6b7280`).
- **Typeface:** Noto Sans (SIL Open Font License), with Verdana as fallback. The Confederation's Frutiger is licensed and not part of the public repository.
- **Shapes:** small radii (3 px, buttons 2 px, fields 1 px) and soft shadows.
- **Components:** fields with a grey frame and a small shadow; tables with a slate-tinted head in small capitals; a slate top bar, a white header with a fine rule, and a slate footer.
- **Dark scheme:** none.

## Decision drivers

- The theme looks like a website of the Confederation.
- Only material under open licences, without fees.
- Nothing is fetched at run time from outside the installation.
- Light and dark schemes, as before (ADR-0070, ADR-0095).

## Considered options

1. Take the design system's values and component looks into the theme stylesheet, and ship Noto Sans with the UI.
2. Ship the design system's compiled stylesheet and markup.
3. Keep the free interpretation.

## Decision outcome

Chosen option: **1**, because the UI has its own markup and variables (ADR-0065), and the design system's look maps onto them in one stylesheet.

### What the theme takes over

- **Colours and shapes:** the colour values, radii and shadows of the design system's default skin.
- **Links:** red and underlined in the content.
- **Buttons:** actions filled in slate, secondary actions outlined in red.
- **Fields and tables:** as in the design system.
- **Bars:** a slate strip above a white header with a fine rule, and a slate footer.
- **Page title:** the red mark under it stays, as the one accent.
- **Typeface:** Noto Sans, its Latin subset from @fontsource/noto-sans 5.3.0 (about 13 KB per style: regular, bold, italic and bold italic), served from `assets/fonts` like the comic theme's font (ADR-0085).

### Whom it is for

The look tells readers that the site belongs to an authority. In the theme choice of the corporate design, the Swiss theme therefore carries a notice:

- It is meant for bodies of the Swiss Confederation.
- Other organisations should not use it, so that their site is not taken for an official one.

The theme is not locked, as the installation cannot know who runs it.

### Dark scheme

The design system has none. The theme derives one from the same slate scale (`#131b22` to `#596978`) and a lighter red (`#fc656b`) that keeps contrast. This is the theme's own extension.

### Consequences

- **Positive:** the theme reads as a site of the Confederation, with its typeface and colours, and stays within open licences.
- **Negative / trade-offs accepted:**
  - It is not the Frutiger of the Confederation's printed corporate design, which needs a licence.
  - The federal coat of arms and the logo are not part of the theme. An installation of the administration sets its logo under the corporate design, as before (ADR-0052).
  - The dark scheme is not defined by the design system.
- **Follow-ups / risks to watch:**
  - A change of the design system's values needs a manual update; the pinned commit is named in `THIRD_PARTY_NOTICES.md`.
  - The look is checked by screenshots, not by a test.

## Pros and cons of the options

### Option 2: the compiled stylesheet and markup

Strongest form: exactly the design system, updated by replacing a file.

- Good: no translation between the two systems.
- Bad:
  - The design system styles its own classes and Vue components, not this UI's markup.
  - Shipping it would mean a second CSS framework and the rewriting of every view.

### Option 3: the free interpretation

Strongest form: a distinct look, inspired by Swiss graphic design, without any dependency.

- Good: nothing to maintain.
- Bad: it is not what the administration uses, and the maintainer asked for that.

## Links

- amends [ADR-0085](0085-ready-made-themes-under-the-corporate-design.md) (the ready-made themes)
- relates to [ADR-0052](0052-corporate-design-in-a-journal-of-its-own.md) (the corporate design) and [ADR-0070](0070-an-own-stylesheet-without-a-dark-look-turns-the-dark-scheme-off.md) (the dark scheme)
