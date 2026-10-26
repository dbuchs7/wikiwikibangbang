// Processes in a drawing (ADR-0142). A drawing shows a BPMN model as a
// picture element whose customData names it: a model kept in the drawing
// itself, or a process of a process engine. The saved PNG shows each as it
// was drawn; a process of an engine is also listed, with where it lies in
// the picture, in a private chunk of the PNG, so a page can lay the newest
// version of the process and its operations over it while the picture is
// shown. This module holds what both sides need and nothing that needs a
// browser: the drawing page writes the chunk, a page reads it.

// KEY names this installation's data in an element's customData.
export const KEY = "wikiwikibangbang";

// PADDING is the margin around a drawing in its picture, Excalidraw's own
// default, passed explicitly so the regions are measured with it.
export const PADDING = 10;

// CHUNK is the PNG chunk that lists the regions: ancillary, private, safe
// to copy (lower, lower, upper, lower), so a viewer skips it and keeps it.
// It is no text chunk: Excalidraw reads its scene from the first text
// chunk of a picture, and a picture must still open in the drawing editor.
const CHUNK = "wkPr";

// KEYWORD named the text chunk that listed the regions in pictures saved
// before CHUNK. Those are still read, and lose it when opened for editing.
const KEYWORD = "wikiwikibangbang.processes";

// MAX_MARGIN bounds the margin around a process's model in its picture, in
// the model's own units.
export const MAX_MARGIN = 200;

// MARGIN is the margin a process gets when it is placed.
export const MARGIN = 40;

// maxRegions bounds how many live processes one picture lists.
const maxRegions = 50;

// The IDs a process block accepts (internal/item/store.go).
const engineID = /^[a-z0-9][a-z0-9.-]{0,39}$/;
const processID = /^[\p{L}_][\p{L}\p{N}_.-]{0,254}$/u;

const text = (v, max) => (typeof v === "string" && v.length <= max ? v : null);
const marginOf = (v) => (Number.isInteger(v) && v >= 0 && v <= MAX_MARGIN ? v : 0);

// processOf returns what a drawing's element shows, checked, or null:
// {kind: "bpmn", name, xml, margin}, a model kept in the drawing, or
// {kind: "process", engine, process, name, operations, margin}, a process
// of an engine, its operations shown at first when operations is set.
// margin is the room around the model in its picture, in the model's
// units; one that is not a whole number up to MAX_MARGIN is none.
export function processOf(element) {
  const d = element?.type === "image" ? element.customData?.[KEY] : null;
  if (!d || typeof d !== "object") return null;
  const name = text(d.name ?? "", 200);
  if (name === null) return null;
  const margin = marginOf(d.margin);
  if (d.kind === "bpmn") {
    const xml = text(d.xml, 8 << 20);
    return xml ? { kind: "bpmn", name, xml, margin } : null;
  }
  if (d.kind === "process" && engineID.test(d.engine ?? "") && processID.test(d.process ?? "")) {
    return { kind: "process", engine: d.engine, process: d.process, name, operations: d.operations === true, margin };
  }
  return null;
}

// rootsOf leaves out what lies in a frame, which Excalidraw measures by its
// frame when it makes the picture.
export function rootsOf(elements) {
  const frames = new Set(elements.filter((e) => e.type === "frame" || e.type === "magicframe").map((e) => e.id));
  return elements.filter((e) => frames.has(e.id) || !e.frameId || !frames.has(e.frameId));
}

const round = (n) => Math.round(n * 1e5) / 1e5;
const fits = (r) => [r.x, r.y, r.w, r.h].every(Number.isFinite) && r.x >= 0 && r.y >= 0 && r.w > 0 && r.h > 0 &&
  r.x + r.w <= 1.001 && r.y + r.h <= 1.001;

// regionsOf lists where the live processes lie in the picture, in shares of
// its width and height. bounds are the scene's [minX, minY, maxX, maxY] by
// Excalidraw's getCommonBounds of rootsOf(elements); size is the picture's
// width and height before scaling, as Excalidraw reports it. A frame's name
// is drawn above the frame, so it can make the picture taller at the top
// but not wider at the left: the top is measured from the bottom.
export function regionsOf(elements, [minX, , , maxY], { width, height }) {
  const top = maxY - (height - 2 * PADDING);
  const out = [];
  for (const el of elements) {
    const p = processOf(el);
    // A turned or mirrored picture is shown as drawn.
    if (!p || p.kind !== "process" || el.isDeleted || Math.abs(el.angle || 0) > 1e-6 || (el.scale && (el.scale[0] !== 1 || el.scale[1] !== 1))) continue;
    const r = { x: (el.x - minX + PADDING) / width, y: (el.y - top + PADDING) / height, w: el.width / width, h: el.height / height };
    if (!fits(r)) continue;
    out.push({ x: round(r.x), y: round(r.y), w: round(r.w), h: round(r.h), engine: p.engine, process: p.process, name: p.name, operations: p.operations, margin: p.margin });
  }
  return out.slice(0, maxRegions);
}

let table = null;
// crc32 is the checksum of a PNG chunk.
export function crc32(bytes) {
  table ||= Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  let c = 0xffffffff;
  for (const b of bytes) c = table[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

const signature = [137, 80, 78, 71, 13, 10, 26, 10];
const isPNG = (b) => b.length >= 33 && signature.every((v, i) => b[i] === v);
const latin1 = (b) => String.fromCharCode(...b);

// chunks walks a PNG's chunks as far as bytes reach: [{type, start, end,
// data}], start and end spanning the whole chunk.
function chunks(bytes) {
  const out = [];
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  for (let at = 8; at + 12 <= bytes.length;) {
    const length = view.getUint32(at);
    const end = at + 12 + length;
    if (end > bytes.length) break;
    out.push({ type: latin1(bytes.subarray(at + 4, at + 8)), start: at, end, data: bytes.subarray(at + 8, at + 8 + length),
      valid: view.getUint32(at + 8 + length) === crc32(bytes.subarray(at + 4, at + 8 + length)) });
    at = end;
  }
  return out;
}

const legacy = (c) => c.type === "tEXt" && latin1(c.data.subarray(0, KEYWORD.length + 1)) === KEYWORD + "\0";
const ours = (c) => c.type === CHUNK || legacy(c);

// withRegions returns the PNG with the regions in its chunk, directly after
// its header, where a page finds them in the picture's first bytes. A chunk
// of an earlier save is replaced, one in the earlier text form too; no
// regions remove it, which is how a picture is opened for editing.
export function withRegions(png, regions) {
  const all = isPNG(png) ? chunks(png) : [];
  if (all[0]?.type !== "IHDR") throw new Error("not a PNG picture");
  const kept = all.filter((c) => !ours(c));
  const parts = [png.subarray(0, 8), png.subarray(kept[0].start, kept[0].end)];
  if (regions.length) {
    // ASCII only, so the chunk reads the same in any decoder.
    const json = JSON.stringify({ v: 1, regions }).replace(/[\u007f-\uffff]/g, (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"));
    const data = new TextEncoder().encode(json);
    const c = new Uint8Array(12 + data.length);
    const view = new DataView(c.buffer);
    view.setUint32(0, data.length);
    c.set(new TextEncoder().encode(CHUNK), 4);
    c.set(data, 8);
    view.setUint32(8 + data.length, crc32(c.subarray(4, 8 + data.length)));
    parts.push(c);
  }
  for (const k of kept.slice(1)) parts.push(png.subarray(k.start, k.end));
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

// regionsIn reads the regions a PNG lists, as far as bytes reach; what is
// not a region a process block could show is left out. A picture is
// anyone's upload, so nothing in it is believed unchecked.
export function regionsIn(bytes) {
  if (!isPNG(bytes)) return [];
  const c = chunks(bytes).find((x) => ours(x) && x.valid);
  if (!c) return [];
  let data;
  try {
    data = JSON.parse(latin1(legacy(c) ? c.data.subarray(KEYWORD.length + 1) : c.data));
  } catch {
    return [];
  }
  if (data?.v !== 1 || !Array.isArray(data.regions)) return [];
  const out = [];
  for (const r of data.regions.slice(0, maxRegions)) {
    const p = processOf({ type: "image", customData: { [KEY]: { ...r, kind: "process" } } });
    if (!p || ![r.x, r.y, r.w, r.h].every((n) => typeof n === "number") || !fits(r)) continue;
    out.push({ x: r.x, y: r.y, w: r.w, h: r.h, engine: p.engine, process: p.process, name: p.name, operations: p.operations, margin: p.margin });
  }
  return out;
}
