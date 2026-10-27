// Forms and decisions of process engines (ADR-0144), the fields of a form
// and what a start form fills in (ADR-0147): what their views in a text
// need and nothing that needs a browser, so it is tested on its own.

// The DMN 1.5 namespaces of a model and of its diagram, as atlas writes
// them, and the DMN 1.3 ones the vendored dmn-js reads.
const DMN15 = "https://www.omg.org/spec/DMN/20230324/MODEL/";
const DMNDI15 = "https://www.omg.org/spec/DMN/20230324/DMNDI/";
const DMN13 = "https://www.omg.org/spec/DMN/20191111/MODEL/";
const DMNDI13 = "https://www.omg.org/spec/DMN/20191111/DMNDI/";

// asDMN13 makes a DMN 1.5 model readable for dmn-js, which reads DMN 1.3:
// it names the model's and the diagram's namespaces as those of 1.3. What
// 1.5 adds is then unknown to dmn-js, which says so in its warnings; what
// both versions have, decisions, their tables and expressions, the graph
// and decision services, reads as written. A model of another version is
// returned as it is. upgraded says whether the namespaces were changed.
export function asDMN13(xml) {
  if (!xml.includes(DMN15)) return { xml, upgraded: false };
  return { xml: xml.replaceAll(DMN15, DMN13).replaceAll(DMNDI15, DMNDI13), upgraded: true };
}

// viewOf returns the view of a decision among a DMN viewer's views, or
// null: the view of its table or expression, found by the decision's ID or,
// as atlas lists a decision by its name, by its name.
export function viewOf(views, decision) {
  const own = views.filter((v) => v.type !== "drd");
  return own.find((v) => v.element?.id === decision) || own.find((v) => v.element?.name === decision) || null;
}

// previewSchema returns a copy of a form-js schema that the viewer shows
// whole: a component of a type it does not know, such as atlas's own
// e-mail view, would stop the whole form from being shown, so it is
// replaced by a text that says what is there. known(type) says whether the
// viewer knows a type, note(type) is the text, given the type's name
// reduced to letters, digits, "-" and "_". Components inside groups and
// lists are checked as well.
export function previewSchema(schema, known, note) {
  const walk = (components) => (Array.isArray(components) ? components.map((c) => {
    if (!c || typeof c !== "object") return c;
    if (!known(c.type)) return { type: "text", text: note(String(c.type ?? "").replace(/[^\w-]/g, "").slice(0, 40)) };
    return Array.isArray(c.components) ? { ...c, components: walk(c.components) } : c;
  }) : components);
  return { ...schema, components: walk(schema?.components) };
}

// The components of a form-js schema that hold nothing a person enters:
// texts, pictures, separators and buttons. groupKinds hold components.
const unkeyed = new Set(["text", "html", "image", "spacer", "separator", "button", "iframe", "table", "documentPreview"]);
// Path segments that would reach an object's prototype are never used.
const unsafeSegment = new Set(["__proto__", "prototype", "constructor"]);

// fieldsOf lists the fields of a form-js schema in its order, for the list
// that describes a form and for what a start form fills in beforehand
// (ADR-0147). A field is {key, label, type, description, required,
// options, hidden, repeated, depth}: key is the path its value is sent
// under, the keys of its groups' paths before its own, joined by "."; a
// group or a list of entries is {group: true, label, type, repeated,
// depth}, before its fields. repeated says that a field belongs to a list
// of entries, whose values are sent per entry.
export function fieldsOf(schema) {
  const out = [];
  const walk = (components, prefix, depth, repeated) => {
    if (!Array.isArray(components)) return;
    for (const c of components) {
      if (!c || typeof c !== "object") continue;
      const type = String(c.type ?? "");
      if (Array.isArray(c.components)) {
        const list = repeated || type === "dynamiclist";
        out.push({ group: true, label: String(c.label ?? ""), type, repeated: list, depth });
        walk(c.components, c.path ? [...prefix, ...String(c.path).split(".")] : prefix, depth + 1, list);
        continue;
      }
      if (c.key === undefined || c.key === null || c.key === "" || unkeyed.has(type)) continue;
      const path = [...prefix, ...String(c.key).split(".")];
      if (path.some((p) => unsafeSegment.has(p))) continue;
      out.push({
        key: path.join("."), label: String(c.label ?? ""), type, description: String(c.description ?? ""),
        required: Boolean(c.validate?.required), options: Array.isArray(c.values) ? c.values.map((v) => String(v?.label ?? v?.value ?? "")) : [],
        hidden: Boolean(c.conditional?.hide), repeated, depth,
      });
    }
  };
  walk(schema?.components, [], 0, false);
  return out;
}

// prefillSources are what a start form's field may be filled in with
// beforehand, as the model names them: the reader's username, the
// reader's own mail address, and the title and the address of the page.
export const prefillSources = ["username", "email", "page", "url"];

// fillable says whether a field may be filled in beforehand: one value a
// person types as text, not one per entry of a list.
export const fillable = (f) => !f.group && !f.repeated && (f.type === "textfield" || f.type === "textarea");

// The hints by which a field is proposed for a source, tried in this order
// on its key's last part and on its label, in lower case.
const hints = [
  ["email", /(^|[^a-z])(e-?mail|mail|mailadresse|e-mail-adresse)($|[^a-z])/],
  ["url", /(^|[^a-z])(url|page_?url|seiten_?url|source_?url)($|[^a-z])/],
  ["page", /^(page|seite|wikiseite|wiki_?page|quelle|source|context|kontext)$/],
  ["username", /^(user|username|user_?name|benutzer|benutzername|login|reporter|reported_?by|gemeldet von|melder|melderin|requester|requested_?by|antragsteller|antragstellerin|initiator|name|your name|ihr name)$/],
];

// suggestPrefill proposes what a start form fills in beforehand when it is
// first placed: for each source, the first fillable field whose key or
// label hints at it, each field once. The editor confirms or changes it.
export function suggestPrefill(fields) {
  const out = [];
  const taken = new Set();
  for (const [from, hint] of hints) {
    const f = fields.find((x) => fillable(x) && !taken.has(x.key) &&
      [x.key.split(".").at(-1), x.label].some((s) => hint.test(String(s).trim().toLowerCase())));
    if (f) {
      out.push({ field: f.key, from });
      taken.add(f.key);
    }
  }
  return out;
}

// prefillData is the data a start form opens with: for each field to fill
// in that the form has and that may be filled in, the value of its source
// in known, set at the field's path; a source without a value fills in
// nothing.
export function prefillData(prefill, fields, known) {
  const keys = new Set(fields.filter(fillable).map((f) => f.key));
  const data = {};
  for (const { field, from } of prefill || []) {
    const v = known?.[from];
    if (!keys.has(field) || typeof v !== "string" || !v) continue;
    const path = field.split(".");
    let o = data;
    for (const p of path.slice(0, -1)) o = o[p] !== null && typeof o[p] === "object" ? o[p] : (o[p] = {});
    o[path.at(-1)] = v;
  }
  return data;
}

// linkOf returns the token of a public start link, given the token or the
// address of the engine's page of it (…/public/forms/<token>), or null.
// atlas makes a token of lower-case hexadecimal digits, as the model
// requires.
export function linkOf(text) {
  const m = /^(?:[a-z][a-z0-9+.-]*:\/\/[^\s]*\/public\/forms\/)?([0-9a-f]{32,128})(?:[/?#][^\s]*)?$/.exec(String(text ?? "").trim());
  return m ? m[1] : null;
}
