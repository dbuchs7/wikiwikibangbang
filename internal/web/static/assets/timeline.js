// The timeline of a roadmap and a Gantt chart (ADR-0164) without a
// browser: which date fields an item is scheduled by, the bar or milestone
// each item becomes, the frame of time they span at a scale, the ticks of
// that frame, the lanes of a roadmap by label with their bars packed into
// rows, the rows of a Gantt chart, where a bar stands in percent of the
// frame, and the view's state as an address.
//
// Days are whole days counted from 1970-01-01 in UTC, so no time zone and
// no daylight saving time moves a bar. The API answers a date field
// (item.KindDate) as midnight UTC in RFC 3339; a date is read by its first
// ten characters, YYYY-MM-DD.
//
// Plain functions over plain data, so node --test runs them
// (internal/web/jstest/timeline.test.mjs).

const DAY = 86400000;

// MODES are the layouts of the view, SCALES the units of its axis.
export const MODES = ["roadmap", "gantt"];
export const SCALES = ["week", "month", "quarter"];

// NO_LABEL is the lane of the items without a label.
export const NO_LABEL = "";

// parseDay reads a date as a day number, or null when it is none.
export function parseDay(value) {
  if (typeof value !== "string") return null;
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(value);
  if (!m) return null;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const ms = Date.UTC(y, mo - 1, d);
  const back = new Date(ms);
  // 2026-02-30 is no day; Date.UTC would roll it over to March.
  if (back.getUTCFullYear() !== y || back.getUTCMonth() !== mo - 1 || back.getUTCDate() !== d) return null;
  return ms / DAY;
}

// dayString writes a day number as YYYY-MM-DD.
export function dayString(day) {
  return new Date(day * DAY).toISOString().slice(0, 10);
}

// todayDay is the day number of a moment, by the calendar of the place
// the browser is in: what its people call today.
export function todayDay(now = new Date()) {
  return Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / DAY;
}

// parts splits a day number into its calendar parts.
function parts(day) {
  const d = new Date(day * DAY);
  return { year: d.getUTCFullYear(), month: d.getUTCMonth(), date: d.getUTCDate(), weekday: (d.getUTCDay() + 6) % 7 };
}

const dayOf = (year, month, date = 1) => Date.UTC(year, month, date) / DAY;

// isoWeek is the ISO 8601 week of a day: the year it belongs to and its
// number, weeks starting on Monday, the first holding a Thursday.
export function isoWeek(day) {
  const thursday = day - parts(day).weekday + 3;
  const year = parts(thursday).year;
  return { year, week: Math.floor((thursday - dayOf(year, 0, 1)) / 7) + 1 };
}

// dateFields lists the date fields of the types, each name once, in the
// order of the alphabet.
export function dateFields(types) {
  const names = new Set();
  for (const ty of types) for (const f of ty.fields || []) if (f.kind === "date") names.add(f.name);
  return [...names].sort();
}

const startNames = /^(start|starts|begin|begins|from|since|kickoff|start_?date|begin_?date|planned_?start)$/i;
const endNames = /^(due|end|ends|until|deadline|finish|finishes|target|end_?date|due_?date|planned_?end|release|delivery)$/i;

// guessFields picks the fields a view is scheduled by when the address
// names none: a start among the names that mean a beginning, an end among
// those that mean an end, else the only or the first other date field.
// A start is never the end's field.
export function guessFields(fields) {
  const end = fields.find((f) => endNames.test(f)) || fields.find((f) => !startNames.test(f)) || "";
  const start = fields.find((f) => startNames.test(f) && f !== end) || "";
  return { start, end };
}

// schedule turns items into bars by the start and end fields. An item with
// both dates is a bar from the start to the end, both days included; one
// with only one of them, or with an end before its start, is a milestone
// on the day it has (the end, when it has both: the date that is promised).
// An item with neither is unscheduled.
export function schedule(items, fields) {
  const bars = [];
  const unscheduled = [];
  for (const it of items) {
    const props = it.properties || {};
    const from = fields.start ? parseDay(props[fields.start]) : null;
    const to = fields.end ? parseDay(props[fields.end]) : null;
    if (from === null && to === null) {
      unscheduled.push(it);
    } else if (from !== null && to !== null && from <= to) {
      bars.push({ item: it, from, to, milestone: false, inverted: false });
    } else {
      const at = to ?? from;
      bars.push({ item: it, from: at, to: at, milestone: true, inverted: from !== null && to !== null });
    }
  }
  return { bars, unscheduled };
}

// span is the first and the last day the bars cover, or null for none.
export function span(bars) {
  if (!bars.length) return null;
  return { from: Math.min(...bars.map((b) => b.from)), to: Math.max(...bars.map((b) => b.to)) };
}

// autoScale is the scale that shows a span of days without crowding:
// weeks up to about three months, months up to two years, else quarters.
export function autoScale(s) {
  const days = s ? s.to - s.from + 1 : 0;
  if (days <= 92) return "week";
  if (days <= 731) return "month";
  return "quarter";
}

// unitStart is the first day of the unit of a scale a day falls in.
function unitStart(day, scale) {
  if (scale === "week") return day - parts(day).weekday;
  const { year, month } = parts(day);
  return dayOf(year, scale === "quarter" ? month - (month % 3) : month);
}

// nextUnit is the first day of the unit after the one starting on day.
function nextUnit(day, scale) {
  if (scale === "week") return day + 7;
  const { year, month } = parts(day);
  return dayOf(year, month + (scale === "quarter" ? 3 : 1));
}

// frame is the span widened to whole units of the scale, with one unit of
// room on either side, and its ticks: one per unit, each with the parts a
// label needs. to is the first day after the frame. today widens nothing;
// it only says whether the line of today falls in the frame.
export function frame(s, scale, today) {
  const from = unitStart(unitStart(s.from, scale) - 1, scale);
  let to = nextUnit(unitStart(s.to, scale), scale);
  to = nextUnit(to, scale);
  const ticks = [];
  for (let at = from; at < to; at = nextUnit(at, scale)) {
    const p = parts(at);
    ticks.push({ day: at, end: nextUnit(at, scale), year: p.year, month: p.month, quarter: Math.floor(p.month / 3) + 1, week: isoWeek(at).week });
  }
  return { from, to, days: to - from, scale, ticks, today: today !== undefined && today >= from && today < to ? today : null };
}

// place is where a stretch of days stands in a frame, in percent of its
// width: its left edge and its width, a day at least.
export function place(fr, from, to) {
  const left = ((from - fr.from) / fr.days) * 100;
  const width = ((to - from + 1) / fr.days) * 100;
  return { left, width };
}

// byStart orders bars by their start, then their end, then their key, so
// the same items always come in the same order.
export function byStart(a, b) {
  return a.from - b.from || a.to - b.to || String(a.item.key).localeCompare(String(b.item.key));
}

// pack lays bars into as few rows as they need without overlapping: each
// bar, in the order of its start, goes into the first row that is free on
// its first day. A milestone takes its day like a bar of one day.
export function pack(bars) {
  const rows = [];
  for (const bar of [...bars].sort(byStart)) {
    const row = rows.find((r) => r[r.length - 1].to < bar.from);
    if (row) row.push(bar);
    else rows.push([bar]);
  }
  return rows;
}

// lanes are the lanes of a roadmap: one per label, in the order chosen, or
// in the order of the alphabet when none is chosen, then one for the items
// without a label. An item with several of the labels stands in each of
// their lanes. A lane without bars is left out unless it was chosen.
export function lanes(bars, chosen = []) {
  const names = chosen.length ? chosen : [...new Set(bars.flatMap((b) => b.item.labels || []))].sort();
  const out = names.map((label) => ({ label, rows: pack(bars.filter((b) => (b.item.labels || []).includes(label))) }))
    .filter((lane) => chosen.length || lane.rows.length);
  if (!chosen.length) {
    const none = bars.filter((b) => !(b.item.labels || []).length);
    if (none.length) out.push({ label: NO_LABEL, rows: pack(none) });
  }
  return out;
}

// ganttRows are the rows of a Gantt chart: one per item, in the order of
// their start.
export function ganttRows(bars) {
  return [...bars].sort(byStart);
}

// normalLabel is a label as typed, as the server keeps it: trimmed, in
// lower case, inner spaces as dashes; empty when nothing is left.
export function normalLabel(typed) {
  return String(typed || "").trim().toLowerCase().replace(/\s+/g, "-");
}

// suggest picks the labels to offer while one types: those that start with
// what was typed first, then those that hold it, each group in the order
// given (the most used first), none already chosen, at most n. Without
// anything typed it offers the most used.
export function suggest(labels, typed, chosen = [], n = 12) {
  const q = normalLabel(typed);
  const free = labels.map((l) => (typeof l === "string" ? l : l.label)).filter((l, i, all) => !chosen.includes(l) && all.indexOf(l) === i);
  if (!q) return free.slice(0, n);
  return [...free.filter((l) => l.startsWith(q)), ...free.filter((l) => !l.startsWith(q) && l.includes(q))].slice(0, n);
}

// readState reads the view's state from the address's parameters; what
// is missing or unknown takes its default.
export function readState(params) {
  const get = (k) => params.get(k) || "";
  const list = (k) => get(k).split(",").map((s) => s.trim()).filter(Boolean);
  return {
    mode: MODES.includes(get("mode")) ? get("mode") : "roadmap",
    scale: SCALES.includes(get("scale")) ? get("scale") : "",
    space: get("space"),
    type: get("type"),
    labels: [...new Set(list("labels"))],
    start: get("start"),
    end: get("end"),
  };
}

// address writes the view's state as its address, leaving out what holds
// its default, so a shared link stays short.
export function address(state) {
  const p = new URLSearchParams();
  if (state.mode && state.mode !== "roadmap") p.set("mode", state.mode);
  for (const k of ["space", "type", "start", "end", "scale"]) if (state[k]) p.set(k, state[k]);
  if (state.labels?.length) p.set("labels", state.labels.join(","));
  const q = p.toString();
  return "#/roadmap" + (q ? "?" + q : "");
}
