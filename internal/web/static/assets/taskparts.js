// The task links of a process engine (ADR-0151): what their administration
// needs to offer a rule's choices and to read a rule from its form, and
// nothing that needs a browser, so it is tested on its own.

// linkTypes are the types whose items can be linked tasks: those with a
// workflow, by name.
export function linkTypes(types) {
  return types.filter((ty) => ty.workflow && ty.workflow.states?.length).sort((a, b) => a.name.localeCompare(b.name));
}

// finished reports whether a state of a workflow is one an item is done
// with (ADR-0082): the last state, or one no transition leaves.
export function finished(workflow, state) {
  const states = workflow?.states || [];
  if (!states.includes(state)) return false;
  if (state === states[states.length - 1]) return true;
  return !(workflow.transitions || []).some((tr) => (tr.from || []).includes(state));
}

// endStates are the finished states of a type, the states an ended task's
// item may go to.
export function endStates(type) {
  const w = type?.workflow;
  return (w?.states || []).filter((s) => finished(w, s));
}

// dateFields are the names of a type's date fields, which a task's due
// date may go into.
export function dateFields(type) {
  return (type?.fields || []).filter((f) => f.kind === "date").map((f) => f.name);
}

// listOf splits what an administrator typed into a list of values: at
// commas and line breaks, trimmed, empty ones and repeats left out.
export function listOf(text) {
  const out = [];
  for (const v of String(text || "").split(/[,\n]/)) {
    const x = v.trim();
    if (x && !out.includes(x)) out.push(x);
  }
  return out;
}

// ruleOf reads a rule from the values of its form, by name.
export function ruleOf(v) {
  const rule = {
    id: String(v.id || "").trim(),
    processes: listOf(v.processes),
    elements: listOf(v.elements),
    groups: listOf(v.groups),
    space: String(v.space || "").trim(),
    type: String(v.type || "").trim(),
    labels: listOf(v.labels),
    completed: String(v.completed || "").trim(),
    cancelled: String(v.cancelled || "").trim(),
  };
  const due = String(v.dueField || "").trim();
  if (due) rule.dueField = due;
  return rule;
}

// nextRuleID is an ID no rule has yet: rule, rule-2, rule-3 …
export function nextRuleID(rules) {
  const taken = new Set(rules.map((r) => r.id));
  if (!taken.has("rule")) return "rule";
  let n = 2;
  while (taken.has("rule-" + n)) n++;
  return "rule-" + n;
}
