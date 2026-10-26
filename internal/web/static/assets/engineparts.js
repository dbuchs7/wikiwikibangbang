// Forms and decisions of process engines (ADR-0143): what their views in a
// text need and nothing that needs a browser, so it is tested on its own.

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
