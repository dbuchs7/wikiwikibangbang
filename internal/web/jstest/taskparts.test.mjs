// Unit tests of the task links' administration and the task dialog
// (taskparts.js, ADR-0151): which types and states a rule may name, how a
// rule is read from its form, which moves open the dialog and which forms
// are worked in the engine's app. They run in Node.js without a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The page itself is in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { linkTypes, finished, endStates, dateFields, listOf, ruleOf, nextRuleID, finishing, engineOnly } from "../static/assets/taskparts.js";

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

test("moving a linked item to a finished state is the engine's, so it opens the task dialog", () => {
  const linked = { linked: "atlas", state: "open" };
  assert.equal(finishing(engineTask.workflow, linked, "cancelled"), true);
  assert.equal(finishing(engineTask.workflow, linked, "in progress"), false);
  assert.equal(finishing(engineTask.workflow, { state: "open" }, "cancelled"), false, "not a linked item");
  assert.equal(finishing(engineTask.workflow, { linked: "atlas", state: "cancelled" }, "cancelled"), false, "already there");
});

test("a form with atlas's own components is worked in the engine's app", () => {
  const known = (type) => ["textfield", "select", "group", "radio"].includes(type);
  assert.deepEqual(engineOnly({ components: [{ type: "textfield", key: "a", label: "A" }, { type: "radio", key: "b" }] }, known), []);
  assert.deepEqual(engineOnly({ components: [
    { type: "mailview", key: "mail", label: "The request" },
    { type: "group", components: [{ type: "select", key: "boss", label: "Manager", atlasDataSource: { worker: "ldap" } }, { type: "signature", key: "sig" }] },
  ] }, known), ["The request", "Manager", "sig"]);
  assert.deepEqual(engineOnly(undefined, known), []);
});
