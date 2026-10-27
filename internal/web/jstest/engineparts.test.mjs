// Unit tests of forms and decisions in a text (engineparts.js, ADR-0144):
// how a DMN 1.5 model is made readable, which view shows a decision, and
// how a form with components the viewer does not know is shown whole; and
// of start forms (ADR-0148): a form's fields, what is proposed and filled
// in beforehand, and what is a start link. They run in Node.js without a
// browser and without dependencies:
//
//   node --test "internal/web/jstest/*.test.mjs"
//
// The blocks themselves are in e2e/smoke.cjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { asDMN13, viewOf, previewSchema, fieldsOf, suggestPrefill, prefillData, prefillSources, linkOf } from "../static/assets/engineparts.js";

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

// A form as atlas keeps one: fields in a group with a path, a list of
// entries, texts and a button, and a field with a key that would reach a
// prototype.
const incident = {
  components: [
    { type: "text", text: "# Report an incident" },
    { type: "textfield", key: "reporter", label: "Reported by", validate: { required: true } },
    { type: "textfield", key: "mail", label: "E-Mail", description: "Where we answer" },
    { type: "select", key: "severity", label: "Severity", values: [{ label: "Low", value: "low" }, { value: "high" }] },
    { type: "group", label: "Context", path: "context", components: [
      { type: "textfield", key: "page", label: "Seite" },
      { type: "textarea", key: "pageUrl", label: "URL" },
    ] },
    { type: "dynamiclist", label: "Affected", path: "affected", components: [{ type: "textfield", key: "name", label: "Name" }] },
    { type: "textfield", key: "__proto__.polluted", label: "x" },
    { type: "checkbox", key: "urgent", label: "Urgent", conditional: { hide: "=severity = \"low\"" } },
    { type: "button", key: "submit", label: "Send" },
    { type: "textfield", label: "No key" },
  ],
};

test("a form's fields are listed with their paths, groups and lists", () => {
  const fields = fieldsOf(incident);
  assert.deepEqual(fields.map((f) => f.group ? `[${f.type} ${f.label}]` : f.key), [
    "reporter", "mail", "severity", "[group Context]", "context.page", "context.pageUrl", "[dynamiclist Affected]", "affected.name", "urgent",
  ]);
  const by = Object.fromEntries(fields.filter((f) => !f.group).map((f) => [f.key, f]));
  assert.equal(by.reporter.required, true);
  assert.equal(by.mail.description, "Where we answer");
  assert.deepEqual(by.severity.options, ["Low", "high"]);
  assert.equal(by["context.page"].depth, 1);
  assert.equal(by["affected.name"].repeated, true);
  assert.equal(by.urgent.hidden, true);
  assert.equal(by.reporter.repeated, false);
  assert.deepEqual(fieldsOf(null), []);
  assert.deepEqual(fieldsOf({ components: "none" }), []);
});

test("what a start form fills in is proposed by its fields' keys and labels", () => {
  const fields = fieldsOf(incident);
  assert.deepEqual(suggestPrefill(fields), [
    { field: "mail", from: "email" }, { field: "context.pageUrl", from: "url" }, { field: "context.page", from: "page" }, { field: "reporter", from: "username" },
  ]);
  // A field in a list of entries is not filled in, nor one that is no text.
  assert.deepEqual(suggestPrefill(fieldsOf({ components: [{ type: "number", key: "email" }, { type: "dynamiclist", path: "l", components: [{ type: "textfield", key: "user" }] }] })), []);
  assert.deepEqual(prefillSources, ["username", "email", "page", "url"]);
});

test("a start form opens with what it fills in, at the fields' paths", () => {
  const fields = fieldsOf(incident);
  const known = { username: "anna", email: "", page: "Runbook", url: "https://wiki.example/#/item/OPS-1" };
  assert.deepEqual(prefillData([
    { field: "reporter", from: "username" }, { field: "mail", from: "email" }, { field: "context.page", from: "page" },
    { field: "context.pageUrl", from: "url" }, { field: "affected.name", from: "username" }, { field: "severity", from: "page" },
    { field: "gone", from: "page" }, { field: "__proto__.polluted", from: "username" }, { field: "urgent", from: "password" },
  ], fields, known), { reporter: "anna", context: { page: "Runbook", pageUrl: "https://wiki.example/#/item/OPS-1" } });
  assert.equal({}.polluted, undefined);
  assert.deepEqual(prefillData(undefined, fields, known), {});
});

test("a start link is its token or the engine's page of it", () => {
  const token = "8f14e45fceea167a5a36dedd4bea2543a4c1b5e2f0d6c7b8a9e0f1d2c3b4a596";
  assert.equal(linkOf(token), token);
  assert.equal(linkOf(`  https://atlas.example.org/public/forms/${token}  `), token);
  assert.equal(linkOf(`https://atlas.example.org/base/public/forms/${token}?x=1#y`), token);
  assert.equal(linkOf(token.slice(0, 31)), null);
  assert.equal(linkOf(token.toUpperCase()), null);
  assert.equal(linkOf(`https://atlas.example.org/api/v1/processes/${token}`), null);
  assert.equal(linkOf(`javascript:alert(1)//public/forms/${token}`), null);
  assert.equal(linkOf(null), null);
});
