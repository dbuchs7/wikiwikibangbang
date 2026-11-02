// What a selection may protect (ADR-0088, ADR-0160): the words selected
// within one block of text, the block or blocks the selection touches, or,
// for a heading, the section it opens. Pure functions of the body's blocks
// as the model holds them, so they run without a browser.

// sectionEnd returns the index of the last block of the section that the
// heading at index i opens: every block up to the next heading of the same
// or a higher level, or to the end of the body. For a block that is no
// heading it is i itself.
export function sectionEnd(blocks, i) {
  const b = blocks[i];
  if (!b || b.kind !== "heading") return i;
  const level = b.level || 1;
  let end = i;
  for (let j = i + 1; j < blocks.length; j++) {
    const o = blocks[j];
    if (o.kind === "heading" && (o.level || 1) <= level) break;
    end = j;
  }
  return end;
}

// protectable reports whether blocks from to to may be protected whole:
// none is protected already and none holds a protected passage.
export function protectable(blocks, from, to) {
  if (from < 0 || to < from || to >= blocks.length) return false;
  for (let i = from; i <= to; i++) {
    const b = blocks[i];
    if (b.secret || (b.marks || []).some((m) => m.type === "secret")) return false;
  }
  return true;
}

// protectChoices lists what may be protected from a selection: passage is
// { block, start, end } when words of one block of text are selected (or
// null), range { from, to } the blocks the selection touches (or the one
// clicked). Each choice is { kind: "passage" | "blocks" | "section", … }
// with the request it sends; the first is the one to offer first.
export function protectChoices(blocks, passage, range) {
  const out = [];
  if (passage && passage.end > passage.start) {
    const b = blocks[passage.block];
    if (b && !b.secret) out.push({ kind: "passage", request: { block: passage.block, start: passage.start, end: passage.end } });
  }
  if (range && protectable(blocks, range.from, range.to)) {
    out.push({ kind: "blocks", from: range.from, to: range.to, request: { fromBlock: range.from, toBlock: range.to } });
  }
  if (range && range.from === range.to && blocks[range.from]?.kind === "heading") {
    const end = sectionEnd(blocks, range.from);
    if (end > range.from && protectable(blocks, range.from, end)) {
      out.push({ kind: "section", from: range.from, to: end, request: { fromBlock: range.from, toBlock: end } });
    }
  }
  return out;
}

// protectedBlocks returns the indexes of the blocks a protection covers.
export function protectedBlocks(blocks, secret) {
  const out = [];
  blocks.forEach((b, i) => { if (b.secret === secret) out.push(i); });
  return out;
}
