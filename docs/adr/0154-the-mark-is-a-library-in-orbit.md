# ADR-0154: The mark is a library in orbit

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer asked for a new mark for the new name, 2026-11-01)

## Context and problem statement

With the rename to Orbithek (ADR-0153) the wordmark changed, but the mark stayed: a question mark above a ticked box, made for a wiki that holds questions and answers. The name now speaks of a library (*-thek*) in orbit, after the product's rooms and the star map of atlas. The mark is also the favicon and the app icon, so it has to read at 16 px.

What does the mark show under the new name?

## Decision drivers

- **Says the name:** library and orbit, without words.
- **Reads small:** a clear silhouette at 16 and 32 px, in a browser tab and on a phone's home screen.
- **Stays in the product's colours:** black and dark petrol blue on light ground, white and light blue on dark ground, as before.
- **Reproducible:** generated from code like the wordmark, so a change of proportion is a change of a number.

## Considered options

1. **A planet of book spines with a ring and a moon:** the classic ringed planet, its body split into three spines.
2. **Books on a shelf, circled by an orbit:** three spines standing side by side, an ellipse around them.
3. **Keep the question mark above the ticked box.**

## Decision outcome

Chosen option: **1, "a planet of book spines with a ring and a moon"**, because the ringed planet is recognised at any size and the spines in its body add the library without making it busy.

The mark is drawn by `docs/brand/source/build.py` from circles and ellipses with shapely: a circle cut into three spines by two narrow gaps, a ring tilted by 24 degrees that passes in front of the spines with a gap cut into them, the ring's far side visible left of the planet, and a moon on the ring's right end. Books are black, ring and moon dark petrol blue (`#0E2A38`); on dark ground white and light blue (`#8FB8CC`). The traced template of the former mark, its tracer and its paths are removed. Because the mark is wider than tall, the icons fit it by its width, and the logo in the top bar is set lower so it keeps its former width.

### Consequences

- **Positive:** mark and name tell the same story; the mark is generated, so it can be adjusted precisely.
- **Negative / trade-offs accepted:** the favicon changes for everyone who has the product open; browsers may show the old one from their cache for a while.
- **Follow-ups / risks to watch:** the bands of a ringed planet are common in logos of space and science; if a confusingly similar mark turns up in the product's field, the proportions or the spines are changed.

## Pros and cons of the options

### Option 1: a planet of book spines with a ring and a moon

Strongest form: the silhouette that everyone reads as "orbit", carrying the library inside it.

- Good: reads at 16 px; square enough for icons; uses the existing colours.
- Bad: at very small sizes the spines merge, and the library is then not seen.

### Option 2: books on a shelf, circled by an orbit

Strongest form: the library is the subject, the orbit its frame.

- Good: the books are unmistakable at large sizes.
- Bad: a draft read as a bar chart; it is wide, so the icons become small.

### Option 3: keep the question mark above the ticked box

Strongest form: no change; the mark was introduced only a week before.

- Good: nothing to redo.
- Bad: it speaks of a wiki of questions and answers, not of the new name.

## Links

- amends [ADR-0153](0153-the-product-is-renamed-orbithek.md) (the mark stayed there)
- the logo: [`docs/brand/README.md`](../brand/README.md)
