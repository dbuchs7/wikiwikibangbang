# ADR-0101: A reading mode

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked for a reading mode that shows the content alone, by F11 or another key in common use, 2026-10-23)

## Context and problem statement

The page tree and the side column fold away one by one (ADR-0100). To read or write a long text, one wants everything else gone at once: the top bar too. The maintainer asked for F11.

F11 is the browser's own key for its full screen. A page cannot rely on taking it over: browsers keep it for themselves. How does the reading mode start with a key people know?

## Decision drivers

- A key people know, and F11 working as they expect.
- One way out that cannot be missed.
- Nothing stored: reading mode is a moment, not a setting.

## Considered options

1. **Reading mode follows the browser's full screen (F11), and has its own button and Ctrl+Shift+F; Esc ends it.**
2. **Take F11 over from the browser.**
3. **Only a button.**

## Decision outcome

Chosen option: **1**.

- **What it does:** it hides the top bar, the page tree, the side column, their fold buttons and the footer. The content keeps its place on the left (ADR-0099), up to 1100 px wide.
- **Starting it:**
  - **F11:** when the browser enters its full screen, reading mode starts (`display-mode: fullscreen`); when it leaves it, reading mode ends.
  - **Button:** the button beside the bulb in the top bar.
  - **Keys:** Ctrl+Shift+F (Cmd+Shift+F on a Mac), the key of Google Docs' compact mode.
  - Started from the page, it leaves the browser's window as it is: the page does not ask for the full screen, so the keys and the browser never contradict each other.
- **Ending it:** Esc (unless a dialog is open), the same keys, or "Leave reading mode (Esc)", a quiet button at the top right that stays while reading.
- **Narrow screens:** below 480 px the button gives way, since the screen holds the content already. The keys still work.

### Consequences

- **Positive:** F11 does what people expect, and more: the page shows its content alone.
- **Negative / trade-offs accepted:**
  - Whether `display-mode: fullscreen` reports F11 differs between browsers. Where it does not, F11 gives the browser's full screen only, and the button or the keys give reading mode.
  - The navigation, the search and the bell are out of sight while reading.

## Pros and cons of the options

### Option 2: take F11 over

Strongest form: exactly the key asked for.

- Bad: browsers do not reliably pass F11 to a page, and where they do, the user loses the browser's full screen.

### Option 3: only a button

Strongest form: the simplest.

- Bad: no key, where the maintainer asked for one.

## Links

- amends [ADR-0100](0100-a-tidy-top-bar-and-columns-that-fold-away.md) (folding the columns)
- relates to ADR-0099 (the left-aligned layout), ADR-0095 (the switches in the top bar)
