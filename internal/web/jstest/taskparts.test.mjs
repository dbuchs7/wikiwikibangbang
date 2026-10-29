// Unit tests of the task links' administration (taskparts.js, ADR-0151):
// which types and states a rule may name and how a rule is read from its
// form. They run in Node.js without a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The page itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { linkTypes, finished, endStates, dateFields, listOf, ruleOf, nextRuleID } from "../static/assets/taskparts.js";

const engineTask = {
  name: "engine-task",
  fields: [{ name: "due", kind: "date" }, { name: "note", kind: "text" }, { name: "decided", kind: "date" }],
  workflow: {
    initial: "open",
    states: ["open", "in progress", "done", "cancelled", "archived"],
    transitions: [
      { name: "start", from: ["open"], to: "in progress" },
      { name: "finish", from: ["open", "in progress"], to: "done" },
      { name: "cancel", from: ["open", "in progress"], to: "cancelled" },
      { name: "archive", from: ["done"], to: "archived" },
    ],
  },
};

test("only types with a workflow can hold linked tasks, by name", () => {
  const got = linkTypes([{ name: "page" }, engineTask, { name: "a-task", workflow: { states: ["x"] } }, { name: "empty", workflow: { states: [] } }]);
  assert.deepEqual(got.map((t) => t.name), ["a-task", "engine-task"]);
});

test("the end states are the finished ones: the last, and those no transition leaves", () => {
  assert.deepEqual(endStates(engineTask), ["cancelled", "archived"]);
  assert.equal(finished(engineTask.workflow, "done"), false, "archive leaves done");
  assert.equal(finished(engineTask.workflow, "gone"), false);
  assert.deepEqual(endStates({ name: "page" }), []);
  assert.deepEqual(endStates(undefined), []);
});

test("a due date goes into a date field", () => {
  assert.deepEqual(dateFields(engineTask), ["due", "decided"]);
  assert.deepEqual(dateFields({}), []);
});

test("a list is typed with commas or line breaks", () => {
  assert.deepEqual(listOf(" urlaub, onboarding\nurlaub ,, \n"), ["urlaub", "onboarding"]);
  assert.deepEqual(listOf(""), []);
  assert.deepEqual(listOf(undefined), []);
});

test("a rule is read from its form, without a due field when none is chosen", () => {
  const rule = ruleOf({ id: " holidays ", processes: "urlaub", elements: "", groups: "hr, Team Lead", space: "HR", type: "engine-task",
    labels: "atlas, {process}", dueField: "", completed: "done", cancelled: "cancelled" });
  assert.deepEqual(rule, { id: "holidays", processes: ["urlaub"], elements: [], groups: ["hr", "Team Lead"], space: "HR", type: "engine-task",
    labels: ["atlas", "{process}"], completed: "done", cancelled: "cancelled" });
  assert.equal(ruleOf({ dueField: "due" }).dueField, "due");
});

test("a new rule takes an ID no rule has", () => {
  assert.equal(nextRuleID([]), "rule");
  assert.equal(nextRuleID([{ id: "rule" }, { id: "rule-2" }]), "rule-3");
});
