// Unit tests of the timeline of a roadmap and a Gantt chart (timeline.js,
// ADR-0164): reading dates, the fields guessed, the bars and milestones
// items become, the frame and its ticks at each scale, a bar's place, the
// rows bars are packed into, the lanes by label and the address. They run
// in Node.js without a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The view itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { NO_LABEL, address, autoScale, dateFields, dayString, frame, ganttRows, guessFields, isoWeek, lanes, pack, parseDay, place, readState,
  schedule, span, todayDay } from "../static/assets/timeline.js";

const d = (s) => parseDay(s);
const it = (key, props, labels = []) => ({ id: "id-" + key, key, title: key, properties: props, labels });

test("dates are read as whole days, and impossible ones are none", () => {
  assert.equal(d("1970-01-02"), 1);
  assert.equal(dayString(d("2026-11-03")), "2026-11-03");
  assert.equal(d("2026-11-03T10:00:00Z"), d("2026-11-03"));
  for (const bad of ["", "2026-02-30", "03.11.2026", null, undefined, 5]) assert.equal(parseDay(bad), null, String(bad));
  assert.equal(dayString(todayDay(new Date(2026, 10, 3, 23, 59))), "2026-11-03");
});

test("ISO weeks start on Monday, the first holding a Thursday", () => {
  assert.deepEqual(isoWeek(d("2026-01-01")), { year: 2026, week: 1 });
  assert.deepEqual(isoWeek(d("2027-01-01")), { year: 2026, week: 53 });
  assert.deepEqual(isoWeek(d("2026-11-03")), { year: 2026, week: 45 });
});

test("the date fields of the types are offered once each, and start and end are guessed by their names", () => {
  const types = [{ name: "task", fields: [{ name: "due", kind: "date" }, { name: "assignee", kind: "user" }] },
    { name: "epic", fields: [{ name: "start", kind: "date" }, { name: "due", kind: "date" }] }, { name: "page" }];
  assert.deepEqual(dateFields(types), ["due", "start"]);
  assert.deepEqual(guessFields(["due", "start"]), { start: "start", end: "due" });
  assert.deepEqual(guessFields(["decided"]), { start: "", end: "decided" });
  assert.deepEqual(guessFields(["begin"]), { start: "begin", end: "" });
  assert.deepEqual(guessFields([]), { start: "", end: "" });
});

test("items become bars, milestones or unscheduled items", () => {
  const items = [
    it("A", { start: "2026-11-02", due: "2026-11-06" }),
    it("B", { due: "2026-11-10" }),
    it("C", { start: "2026-11-12" }),
    it("D", { start: "2026-11-20", due: "2026-11-15" }),
    it("E", {}),
    it("F", { start: "not a date" }),
  ];
  const { bars, unscheduled } = schedule(items, { start: "start", end: "due" });
  const by = Object.fromEntries(bars.map((b) => [b.item.key, b]));
  assert.deepEqual([by.A.from, by.A.to, by.A.milestone], [d("2026-11-02"), d("2026-11-06"), false]);
  assert.deepEqual([by.B.from, by.B.to, by.B.milestone], [d("2026-11-10"), d("2026-11-10"), true]);
  assert.deepEqual([by.C.from, by.C.milestone], [d("2026-11-12"), true]);
  // An end before the start: a milestone on the promised end, marked.
  assert.deepEqual([by.D.from, by.D.milestone, by.D.inverted], [d("2026-11-15"), true, true]);
  assert.deepEqual(unscheduled.map((x) => x.key), ["E", "F"]);
  assert.deepEqual(span(bars), { from: d("2026-11-02"), to: d("2026-11-15") });
  assert.equal(span([]), null);
  // Without a start field, every item with an end is a milestone.
  assert.ok(schedule(items, { start: "", end: "due" }).bars.every((b) => b.milestone));
});

test("the scale follows the span, and the frame is whole units with room on either side", () => {
  assert.equal(autoScale({ from: 0, to: 30 }), "week");
  assert.equal(autoScale({ from: 0, to: 400 }), "month");
  assert.equal(autoScale({ from: 0, to: 1000 }), "quarter");

  const s = { from: d("2026-11-04"), to: d("2026-11-18") }; // a Wednesday to a Wednesday
  const weeks = frame(s, "week", d("2026-11-10"));
  assert.equal(dayString(weeks.from), "2026-10-26");
  assert.equal(dayString(weeks.to), "2026-11-30");
  assert.equal(weeks.ticks.length, 5);
  assert.deepEqual(weeks.ticks.map((x) => x.week), [44, 45, 46, 47, 48]);
  assert.equal(weeks.today, d("2026-11-10"));
  assert.equal(frame(s, "week", d("2027-01-01")).today, null);

  const months = frame(s, "month");
  assert.deepEqual([dayString(months.from), dayString(months.to)], ["2026-10-01", "2027-01-01"]);
  assert.deepEqual(months.ticks.map((x) => x.month), [9, 10, 11]);

  const quarters = frame({ from: d("2026-02-10"), to: d("2027-05-01") }, "quarter");
  assert.deepEqual([dayString(quarters.from), dayString(quarters.to)], ["2025-10-01", "2027-10-01"]);
  assert.deepEqual(quarters.ticks.map((x) => `${x.year}Q${x.quarter}`), ["2025Q4", "2026Q1", "2026Q2", "2026Q3", "2026Q4", "2027Q1", "2027Q2", "2027Q3"]);
});

test("a bar stands in percent of the frame, both of its days included", () => {
  const fr = { from: 0, to: 100, days: 100 };
  assert.deepEqual(place(fr, 10, 19), { left: 10, width: 10 });
  assert.deepEqual(place(fr, 50, 50), { left: 50, width: 1 });
});

test("bars are packed into as few rows as they need, in a stable order", () => {
  const b = (key, from, to) => ({ item: { key }, from, to });
  const rows = pack([b("C", 5, 9), b("A", 0, 4), b("B", 2, 6), b("D", 7, 8), b("E", 10, 10)]);
  assert.deepEqual(rows.map((r) => r.map((x) => x.item.key)), [["A", "C", "E"], ["B", "D"]]);
  assert.deepEqual(ganttRows([b("B", 1, 2), b("A", 1, 2), b("C", 0, 9)]).map((x) => x.item.key), ["C", "A", "B"]);
});

test("a roadmap has a lane per label, items in each of their lanes, and one for the unlabelled", () => {
  const { bars } = schedule([
    it("A", { due: "2026-11-02" }, ["q1", "platform"]),
    it("B", { due: "2026-11-03" }, ["q1"]),
    it("C", { due: "2026-11-04" }),
  ], { start: "", end: "due" });
  const all = lanes(bars);
  assert.deepEqual(all.map((l) => l.label), ["platform", "q1", NO_LABEL]);
  assert.deepEqual(all[1].rows.flat().map((x) => x.item.key), ["A", "B"]);
  // Chosen labels keep their order and their lane even when empty, and
  // leave out what carries none of them.
  const chosen = lanes(bars, ["q1", "later"]);
  assert.deepEqual(chosen.map((l) => [l.label, l.rows.flat().length]), [["q1", 2], ["later", 0]]);
});

test("the state lives in the address, defaults left out", () => {
  const state = readState(new URLSearchParams("mode=gantt&space=PLAT&labels=q1,%20q2,q1&start=start&end=due&scale=month&type=epic"));
  assert.deepEqual(state, { mode: "gantt", scale: "month", space: "PLAT", type: "epic", labels: ["q1", "q2"], start: "start", end: "due" });
  assert.equal(address(state), "#/roadmap?mode=gantt&space=PLAT&type=epic&start=start&end=due&scale=month&labels=q1%2Cq2");
  assert.deepEqual(readState(new URLSearchParams(address(state).split("?")[1])), state);
  const plain = readState(new URLSearchParams("mode=pie&scale=decade"));
  assert.deepEqual([plain.mode, plain.scale], ["roadmap", ""]);
  assert.equal(address(plain), "#/roadmap");
});
