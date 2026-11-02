// Unit tests of the description of a BPMN model (bpmndoc.js, ADR-0143):
// its processes and, in the order the flow reaches them, their tasks and
// decisions with what the model says about them. They run in Node.js
// without a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The model is given as bpmn-js reads it (moddle objects: $type, and
// references as the objects they name); the page that shows it is in
// e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { describe as describeModel, kindOf, implementationOf, calledDecisionOf } from "../static/assets/bpmndoc.js";
import { docFromBlocks, blocksFromDoc } from "../static/assets/editor.js";

const doc = (text) => [{ $type: "bpmn:Documentation", text }];

// model builds an order process: a review in the clerks' lane, a decision
// on the amount with a default way, a booking with a note, an approval
// with a timer on it, a sub-process, a merge and two ends.
function model() {
  const el = (type, id, more = {}) => ({ $type: "bpmn:" + type, id, incoming: [], outgoing: [], ...more });
  const start = el("StartEvent", "start", { name: "Order received" });
  const review = el("UserTask", "review", { name: "Review order", documentation: doc("Check the customer and\nthe items.") });
  const amount = el("ExclusiveGateway", "amount", { name: "Amount over 1000?" });
  const book = el("ServiceTask", "book", { name: "Book order" });
  const approve = el("UserTask", "approve", { name: "Approve order" });
  const late = el("BoundaryEvent", "late", { name: "Two days", attachedToRef: approve, documentation: doc("Reminds the approver.") });
  const ship = el("SubProcess", "ship", { name: "Ship" });
  const pack = el("ManualTask", "pack", { name: "Pack" });
  ship.flowElements = [pack];
  const merge = el("ExclusiveGateway", "merge");
  const done = el("EndEvent", "done", { name: "Done" });
  const reminded = el("EndEvent", "reminded", { name: "Reminded", documentation: doc("The order waits.") });
  const flows = [];
  const flow = (id, from, to, more = {}) => {
    const f = { $type: "bpmn:SequenceFlow", id, sourceRef: from, targetRef: to, ...more };
    from.outgoing.push(f);
    to.incoming.push(f);
    flows.push(f);
    return f;
  };
  flow("f1", start, review);
  flow("f2", review, amount);
  const small = flow("f3", amount, book, { name: "no" });
  flow("f4", amount, approve, { name: "yes", conditionExpression: { $type: "bpmn:FormalExpression", body: "=amount > 1000" } });
  amount.default = small;
  flow("f5", approve, merge);
  flow("f6", book, merge);
  flow("f7", merge, ship);
  flow("f8", ship, done);
  flow("f9", late, reminded);
  const note = { $type: "bpmn:TextAnnotation", id: "note", text: "Books in SAP." };
  const process = {
    $type: "bpmn:Process", id: "order", name: "Order to cash (process)", documentation: doc("How an order is handled."),
    flowElements: [done, reminded, merge, ship, late, approve, book, amount, review, start, ...flows],
    artifacts: [note, { $type: "bpmn:Association", id: "a1", sourceRef: book, targetRef: note }],
    laneSets: [{ $type: "bpmn:LaneSet", lanes: [
      { $type: "bpmn:Lane", id: "clerks", name: "Clerks", flowNodeRef: [start, review, amount, book] },
      { $type: "bpmn:Lane", id: "heads", name: "Heads", flowNodeRef: [approve, late, reminded], childLaneSet: { lanes: [{ $type: "bpmn:Lane", id: "cfo", name: "CFO", flowNodeRef: [approve] }] } },
    ] }],
  };
  return {
    $type: "bpmn:Definitions",
    rootElements: [
      { $type: "bpmn:Collaboration", id: "c", participants: [{ $type: "bpmn:Participant", id: "pool", name: "Order to cash", processRef: process }] },
      process,
    ],
  };
}

test("the processes of a model, by their pools' names, with their own description", () => {
  const [p, ...rest] = describeModel(model());
  assert.equal(rest.length, 0, "a process is described once, though its pool names it too");
  assert.equal(p.id, "order");
  assert.equal(p.name, "Order to cash");
  assert.equal(p.documentation, "How an order is handled.");
});

test("tasks and decisions in the order the flow reaches them, with what the model says of them", () => {
  const steps = describeModel(model())[0].steps;
  assert.deepEqual(steps.map((s) => s.id), ["review", "amount", "book", "approve", "late", "reminded", "ship", "pack"]);
  const by = Object.fromEntries(steps.map((s) => [s.id, s]));
  assert.deepEqual(by.review, { id: "review", type: "UserTask", kind: "task", name: "Review order", lane: "Clerks", documentation: "Check the customer and\nthe items.", notes: [], paths: [], implementation: null, decision: null, depth: 0 });
  assert.equal(by.book.notes[0], "Books in SAP.");
  assert.equal(by.approve.lane, "CFO", "the innermost lane names who does it");
  assert.deepEqual(by.amount.paths, [
    { name: "no", condition: "", target: "Book order", targetId: "book", isDefault: true },
    { name: "yes", condition: "=amount > 1000", target: "Approve order", targetId: "approve", isDefault: false },
  ]);
  assert.equal(by.amount.kind, "decision");
  // An event is described when the model describes it; a timer follows the task it sits on.
  assert.equal(by.late.kind, "event");
  assert.equal(by.reminded.documentation, "The order waits.");
  // A sub-process is a step, and what it holds follows it, one level deeper.
  assert.equal(by.ship.kind, "task");
  assert.equal(by.pack.depth, 1);
  // A merge that decides nothing, and an end the model says nothing about, are left out.
  assert.ok(!by.merge && !by.done && !by.start);
});

test("a model without a collaboration, without names and without flows still reads", () => {
  const t1 = { $type: "bpmn:Task", id: "t1", incoming: [], outgoing: [] };
  const t2 = { $type: "bpmn:ScriptTask", id: "t2", name: "Second", incoming: [], outgoing: [] };
  const [p] = describeModel({ $type: "bpmn:Definitions", rootElements: [{ $type: "bpmn:Process", id: "bare", flowElements: [t1, t2] }] });
  assert.equal(p.name, "");
  assert.deepEqual(p.steps.map((s) => [s.id, s.name, s.lane]), [["t1", "", ""], ["t2", "Second", ""]]);
  assert.deepEqual(describeModel(null), []);
  assert.deepEqual(describeModel({ rootElements: [] }), []);
});

test("every kind of element has a kind the page can name", () => {
  assert.equal(kindOf("bpmn:BusinessRuleTask"), "BusinessRuleTask");
  assert.equal(kindOf("bpmn:IntermediateCatchEvent"), "IntermediateCatchEvent");
  assert.equal(kindOf(undefined), "");
});

test("what a diagram shows is kept with its block (ADR-0143)", () => {
  const blocks = [
    { kind: "process", engine: "atlas", process: "order", text: "Live", view: "both" },
    { kind: "bpmn", ref: "0123456789abcdef0123456789abcdef", text: "", view: "text" },
    { kind: "bpmn", ref: "0123456789abcdef0123456789abcdef", text: "" },
  ];
  assert.deepEqual(blocksFromDoc(docFromBlocks(blocks)), blocks);
});

test("a loop is described from where the flow enters it", () => {
  const el = (type, id) => ({ $type: "bpmn:" + type, id, name: id, incoming: [], outgoing: [] });
  const [s, a, b, g, c] = [el("StartEvent", "s"), el("Task", "a"), el("Task", "b"), el("ExclusiveGateway", "g"), el("Task", "c")];
  const flow = (from, to) => {
    const f = { $type: "bpmn:SequenceFlow", sourceRef: from, targetRef: to };
    from.outgoing.push(f);
    to.incoming.push(f);
    return f;
  };
  const fs = [flow(s, a), flow(a, b), flow(b, g), flow(g, a), flow(g, c)];
  const [p] = describeModel({ rootElements: [{ $type: "bpmn:Process", id: "p", flowElements: [c, g, b, a, s, ...fs] }] });
  assert.deepEqual(p.steps.map((x) => x.id), ["a", "b", "g", "c"]);
  assert.deepEqual(p.steps[2].paths.map((x) => x.targetId), ["a", "c"]);
});

test("the flow is read from the sequence flows, as a model without incoming and outgoing lists has it", () => {
  // Tools other than the bpmn.io modeler write no <incoming> and <outgoing>;
  // the sequence flows' sourceRef and targetRef are what BPMN requires.
  const el = (type, id, more = {}) => ({ $type: "bpmn:" + type, id, name: id, ...more });
  const [s, check, gw, book, approve] = [el("StartEvent", "s"), el("UserTask", "check"), el("ExclusiveGateway", "gw"), el("ServiceTask", "book"), el("UserTask", "approve")];
  const flow = (id, from, to, more = {}) => ({ $type: "bpmn:SequenceFlow", id, sourceRef: from, targetRef: to, ...more });
  const no = flow("no", gw, book, { name: "no" });
  gw.default = no;
  const [p] = describeModel({ rootElements: [{ $type: "bpmn:Process", id: "p", flowElements: [s, check, gw, book, approve,
    flow("f1", s, check), flow("f2", check, gw), no, flow("yes", gw, approve, { name: "yes" }), flow("f5", approve, book)] }] });
  assert.deepEqual(p.steps.map((x) => x.id), ["check", "gw", "approve", "book"]);
  assert.deepEqual(p.steps[1].paths.map((w) => [w.name, w.target, w.isDefault]), [["no", "book", true], ["yes", "approve", false]]);
});

// task builds an element with extension elements as the viewer reads them
// without atlas's and Zeebe's descriptors: each is typed by the name the
// model writes, with the model's own prefix.
const task = (type, ...values) => ({ $type: "bpmn:" + type, id: "t", extensionElements: { $type: "bpmn:ExtensionElements", values } });

test("a step names how atlas carries it out, as atlas's Implement view marks it (ADR-0163)", () => {
  const entra = implementationOf(task("ServiceTask", { $type: "atlas:entraConnector", connector: "aog_entra", operation: "create-group" }));
  assert.equal(entra.id, "entra");
  assert.equal(entra.name, "Microsoft Entra ID");
  assert.ok(Array.isArray(entra.glyph) && entra.glyph.length, "a Worker Type has an icon");
  assert.equal(implementationOf(task("ServiceTask", { $type: "a:MssqlConnector" })).id, "mssql", "the prefix and the case the model writes do not matter");
  assert.equal(implementationOf(task("SendTask", { $type: "atlas:mailConnector" })).id, "mail", "a send task can carry a Worker Type");
  assert.equal(implementationOf({ $type: "bpmn:SendTask", id: "m", messageRef: { id: "msg" } }).id, "message");
  assert.equal(implementationOf(task("SendTask", { $type: "atlas:shopTask" })), null, "a shop task keeps BPMN's envelope");
  assert.equal(implementationOf(task("ScriptTask", { $type: "atlas:jobScript", language: "python" })).id, "python");
  assert.equal(implementationOf(task("ScriptTask", { $type: "atlas:jobScript" })).id, "powershell", "atlas runs a script without a language as PowerShell");
  assert.equal(implementationOf({ $type: "bpmn:ScriptTask", id: "feel" }), null, "a FEEL script runs in the engine");
  assert.equal(implementationOf(task("AdHocSubProcess", { $type: "atlas:agentConnector" })).id, "agent");
  assert.equal(implementationOf({ $type: "bpmn:AdHocSubProcess", id: "plain" }), null);
  assert.equal(implementationOf(task("ServiceTask", { $type: "zeebe:taskDefinition", type: "payment" })), null, "the plain job worker keeps BPMN's gear");
  const unknown = implementationOf(task("ServiceTask", { $type: "atlas:ledgerConnector" }));
  assert.deepEqual([unknown.id, unknown.name], ["other", "ledger"], "a Worker Type the list does not know is named as the model names it");
  assert.equal(implementationOf(task("UserTask", { $type: "atlas:entraConnector" })), null, "only the kinds atlas marks are marked");
  assert.equal(implementationOf(null), null);
});

test("a business rule task names the decision it calls and where its result goes (ADR-0163)", () => {
  const called = { $type: "zeebe:calledDecision", decisionId: " AogGruppenregel ", resultVariable: "regel" };
  assert.deepEqual(calledDecisionOf(task("BusinessRuleTask", { $type: "zeebe:ioMapping" }, called)), { id: "AogGruppenregel", result: "regel" });
  assert.deepEqual(calledDecisionOf(task("BusinessRuleTask", { $type: "z:calledDecision", decisionId: "d" })), { id: "d", result: "" });
  assert.equal(calledDecisionOf(task("BusinessRuleTask", { $type: "zeebe:taskDefinition", type: "rules" })), null, "a rule a job worker applies calls no decision");
  assert.equal(calledDecisionOf(task("ServiceTask", called)), null);
  const [p] = describeModel({ rootElements: [{ $type: "bpmn:Process", id: "p", flowElements: [{ ...task("BusinessRuleTask", called), name: "Apply rule" }] }] });
  assert.deepEqual(p.steps[0].decision, { id: "AogGruppenregel", result: "regel" });
  assert.equal(p.steps[0].implementation, null);
});
