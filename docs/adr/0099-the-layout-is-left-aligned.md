# ADR-0099: The layout is left-aligned

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer asked that the layout of the whole application be left-aligned instead of centred, 2026-10-23)

## Context and problem statement

The content of every view was a column of at most 1680 px, centred in the window (ADR-0065). The sign-in card and the footer were centred too. On a wide screen the views started far from the left edge, while the header with the logo and the navigation starts at it. The eye had to jump between the header and the content.

## Decision drivers

- One edge to read from: the header, the content and the footer start at the same place.
- Nothing else of the system changes: widths, spacing and columns stay (ADR-0065, ADR-0091).

## Considered options

1. **Left-align the content column, the sign-in card and the footer; keep the maximum width.**
2. **Let the content fill the whole width.**
3. **Keep the centred column.**

## Decision outcome

Chosen option: **1**.

- **Content:** the content column starts at the left edge, with the same padding as the header. It keeps its maximum width of 1680 px, so lines and tables stay readable on very wide screens.
- **Sign-in and other single cards:** the card (`.narrow`) stands at the left as well.
- **Footer:** it is left-aligned with the same padding.
- **Text the author aligns** (centred paragraphs, pictures; ADR-0059) stays as written: this is about the layout, not about content.

### Consequences

- **Positive:** header and content share one left edge on every screen.
- **Negative / trade-offs accepted:** on screens wider than about 1760 px, the free space is on the right instead of on both sides.

## Pros and cons of the options

### Option 2: the full width

Strongest form: no empty space at all.

- Bad: text lines and tables become very long on wide screens and harder to read.

### Option 3: centred

Strongest form: the usual look of web pages.

- Bad: it is what the maintainer asked to change; the content does not line up with the header.

## Links

- amends [ADR-0065](0065-a-uniform-airy-ui-system.md) (the frame of a view)
- relates to ADR-0091 (rules every view follows)
- amended by [ADR-0102](0102-owner-and-details-beside-the-text-sign-in-centred.md) (single cards are centred)
