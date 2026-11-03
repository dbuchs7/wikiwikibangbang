// The symbols of the page tree: a sheet with lines of text for a page and
// a ticked box for a task, as the start page's module symbols draw them.
// An item with items below it shows its symbol on a stack: two more
// outlines of the same shape lie behind it, each hidden where the one in
// front covers it, so only the top symbol is drawn whole. Nothing here
// needs a browser, so it is tested on its own.

// shapes are the symbols in a 24 by 24 box: the outline as a rounded
// rectangle [x, y, width, height] with corner radius r, the marks inside
// it as a path, and the scale the symbol takes on top of a stack.
const shapes = {
  page: { box: [5, 3, 14, 18], r: 2, marks: "M9 8h6M9 12h6M9 16h4", scale: 0.7 },
  task: { box: [4, 4, 16, 16], r: 2, marks: "M8 12.5l3 3 5-6", scale: 0.75 },
};

// step is how far each outline of a stack lies up and to the right of the
// one in front of it. It must exceed the corner radius, so that the part
// of an outline left visible starts on a straight edge.
const step = 3;

// num writes a coordinate without trailing zeros.
const num = (v) => String(Math.round(v * 100) / 100);

function outline(x, y, w, h, r) {
  const [a, b, c, d] = [x + r, x + w - r, y + r, y + h - r];
  return `M${num(a)} ${num(y)}H${num(b)}A${num(r)} ${num(r)} 0 0 1 ${num(x + w)} ${num(c)}V${num(d)}`
    + `A${num(r)} ${num(r)} 0 0 1 ${num(b)} ${num(y + h)}H${num(a)}A${num(r)} ${num(r)} 0 0 1 ${num(x)} ${num(d)}`
    + `V${num(c)}A${num(r)} ${num(r)} 0 0 1 ${num(a)} ${num(y)}Z`;
}

// behind is the visible part of an outline at x, y that lies step up and
// to the right of an outline of the same size: the end of its left edge,
// its top and right edges and the end of its bottom edge.
function behind(x, y, w, h, r) {
  const R = `${num(r)} ${num(r)} 0 0 1`;
  return `M${num(x)} ${num(y + step)}V${num(y + r)}A${R} ${num(x + r)} ${num(y)}H${num(x + w - r)}`
    + `A${R} ${num(x + w)} ${num(y + r)}V${num(y + h - r)}A${R} ${num(x + w - r)} ${num(y + h)}H${num(x + w - step)}`;
}

// treeIconKind tells which symbol an item of a type shows: the task's
// when the type is one of the task types, else the page's.
export function treeIconKind(type, taskTypes) {
  return taskTypes && taskTypes.has(type) ? "task" : "page";
}

// treeIconParts returns the paths of a symbol, back to front, each with the
// transform it is drawn with, if any. kind is "page" or "task"; stacked
// asks for the stack of an item with items below it.
export function treeIconParts(kind, stacked) {
  const s = shapes[kind] || shapes.page;
  const [x, y, w, h] = s.box;
  const front = outline(x, y, w, h, s.r) + s.marks;
  if (!stacked) return [{ d: front }];
  const [sw, sh, sr] = [w * s.scale, h * s.scale, s.r * s.scale];
  // The stack is centred in the box: the front symbol at the lower left,
  // the two outlines behind it a step further up and right each.
  const fx = (24 - sw - 2 * step) / 2;
  const fy = (24 + sh + 2 * step) / 2 - sh;
  return [
    { d: behind(fx + 2 * step, fy - 2 * step, sw, sh, sr) },
    { d: behind(fx + step, fy - step, sw, sh, sr) },
    { d: front, transform: `translate(${num(fx - x * s.scale)} ${num(fy - y * s.scale)}) scale(${s.scale})` },
  ];
}
