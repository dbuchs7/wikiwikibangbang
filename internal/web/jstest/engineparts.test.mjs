// Unit tests of forms and decisions in a text (engineparts.js, ADR-0144):
// how a DMN 1.5 model is made readable, which view shows a decision, and
// how a form with components the viewer does not know is shown whole. They
// run in Node.js without a browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The blocks themselves are in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { asDMN13, viewOf, previewSchema } from "../static/assets/engineparts.js";

test("a DMN 1.5 model is read in the namespaces of DMN 1.3", () => {
  const v15 = `<definitions xmlns="https://www.omg.org/spec/DMN/20230324/MODEL/" xmlns:dmndi="https://www.omg.org/spec/DMN/20230324/DMNDI/" xmlns:dc="http://www.omg.org/spec/DMN/20180521/DC/"/>`;
  const { xml, upgraded } = asDMN13(v15);
  assert.equal(upgraded, true);
  assert.equal(xml, `<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" xmlns:dmndi="https://www.omg.org/spec/DMN/20191111/DMNDI/" xmlns:dc="http://www.omg.org/spec/DMN/20180521/DC/"/>`);
  const v13 = `<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/"/>`;
  assert.deepEqual(asDMN13(v13), { xml: v13, upgraded: false });
});

test("a decision is found by its ID or, as atlas lists it, by its name", () => {
  const views = [
    { type: "drd", element: { id: "defs", name: "Credit" } },
    { type: "decisionTable", element: { id: "dec_bonitaet", name: "Bonität" } },
    { type: "literalExpression", element: { id: "Tragbarkeit", name: "Affordability" } },
    { type: "decisionTable", element: { id: "x", name: "Tragbarkeit" } },
  ];
  assert.equal(viewOf(views, "dec_bonitaet"), views[1]);
  assert.equal(viewOf(views, "Bonität"), views[1]);
  assert.equal(viewOf(views, "Tragbarkeit"), views[2], "an ID wins over a name");
  assert.equal(viewOf(views, "Credit"), null, "the graph is not a decision");
  assert.equal(viewOf(views, "nothing"), null);
  assert.equal(viewOf([], "x"), null);
});

test("components the viewer does not know are named, not dropped, at every depth", () => {
  const known = (type) => ["text", "textfield", "group", "select"].includes(type);
  const note = (type) => `[${type}]`;
  const schema = { type: "default", id: "travel", components: [
    { type: "textfield", key: "to" },
    { type: "mailview", key: "mail" },
    { type: "group", components: [{ type: "select", key: "who", atlasDataSource: { version: 1 } }, { type: "<b>odd</b> type!" }, null] },
    { key: "untyped" },
  ] };
  const out = previewSchema(schema, known, note);
  assert.deepEqual(out, { type: "default", id: "travel", components: [
    { type: "textfield", key: "to" },
    { type: "text", text: "[mailview]" },
    { type: "group", components: [{ type: "select", key: "who", atlasDataSource: { version: 1 } }, { type: "text", text: "[boddbtype]" }, null] },
    { type: "text", text: "[]" },
  ] });
  assert.equal(schema.components[1].type, "mailview", "the schema read is not changed");
  assert.deepEqual(previewSchema({ type: "default" }, known, note), { type: "default", components: undefined });
});
