# ADR-0070: An own stylesheet without a dark look turns the dark scheme off

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that an own stylesheet applies to the whole installation and that the dark mode is switched off when the stylesheet defines none, 2026-10-20)

## Context and problem statement

An administrator can add an own stylesheet to the corporate design (ADR-0052). It is applied after the UI's own styles, on every page of the UI, the sign-in page included. The UI follows the system's dark mode, so a reader with a dark system sees the product's dark palette under the installation's own rules. An own stylesheet written for the light look — a white background, dark text in a header — then mixes with dark surfaces and light text and can become unreadable. Which scheme applies when an installation brings its own stylesheet?

## Decision drivers

- An own stylesheet is meant for the whole installation; its author decides the look.
- A stylesheet written only for the light look must not end up unreadable in the dark.
- An author who wants a dark look must be able to keep the dark scheme.
- No flash of the wrong scheme while a page loads.

## Considered options

1. Keep following the system; the author must handle the dark scheme.
2. A setting "dark mode on/off" in the design form.
3. Derive it from the stylesheet: no `@media (prefers-color-scheme: dark)` rule in it means no dark scheme.

## Decision outcome

Chosen option: **3**, as the maintainer asked, because the stylesheet itself already says whether its author thought of the dark look.

- The dark scheme is on while the own stylesheet is empty or contains a `@media (prefers-color-scheme: dark)` rule; otherwise it is off for every reader (`Design.DarkMode`).
- The UI's dark rules live in `assets/dark.css`. `index.html` links them as `/theme-dark.css` for `(prefers-color-scheme: dark)`; the server sends them, or an empty stylesheet when the dark scheme is off. The server decides, so a page opens in the right scheme without flashing the other.
- When it is off, `/branding.css` sets `color-scheme: light`, applies the corporate colours meant for the light scheme always, and leaves out the lightened dark accent. `/branding.json` carries `dark`; the UI then keeps the product's dark logo, and the drawing editor stays light.
- The design form says whether the dark scheme is on and how to keep it: with `@media (prefers-color-scheme: dark) { … }` rules in the own stylesheet.

### Consequences

- **Positive:** an own stylesheet looks as its author wrote it for everyone; an author who wants a dark look keeps one by writing it.
- **Negative / trade-offs accepted:** the rule is implicit; a stylesheet that mentions the dark media query only in a comment keeps the dark scheme on. Readers lose the dark scheme as soon as any own stylesheet without dark rules is saved, even a small one.
- **Follow-ups / risks to watch:** an explicit switch could be added if the implicit rule surprises administrators.

## Pros and cons of the options

### Option 1: keep following the system

Strongest form: the system setting is the reader's choice, and the product should not override it.

- Good: no new rule.
- Bad: a stylesheet written for the light look becomes unreadable in the dark — the case the maintainer met.

### Option 2: an explicit switch

Strongest form: explicit beats implicit; the administrator sees and sets it.

- Good: no guessing.
- Bad: one more setting to understand; an administrator who adds a light-only stylesheet and forgets the switch gets the unreadable mix again.

### Option 3: derive it from the stylesheet

Strongest form: the stylesheet is the only place that knows whether a dark look was designed.

- Good: right by default; no extra setting.
- Bad: implicit, and a mention in a comment counts.
