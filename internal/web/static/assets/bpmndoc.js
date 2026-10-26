// The description of a BPMN model (ADR-0143): its processes and, in the
// order the flow reaches them, their tasks and decisions with what the
// model says of them — the type, the lane, the documentation, the notes
// attached to them and, for a decision, its ways out. It reads the model
// as bpmn-js does (moddle objects: $type, references as the objects they
// name), so the description and the diagram come from one reading. The
// same prose is what atlas's process documentation is made of.

const activities = new Set(["Task", "UserTask", "ServiceTask", "ScriptTask", "SendTask", "ReceiveTask", "ManualTask",
  "BusinessRuleTask", "CallActivity", "SubProcess", "Transaction", "AdHocSubProcess"]);
const containers = new Set(["SubProcess", "Transaction", "AdHocSubProcess"]);

// kindOf names an element's type without its namespace: "UserTask".
export const kindOf = (type) => (typeof type === "string" ? type.replace(/^bpmn:/, "") : "");

const isFlowNode = (e) => {
  const k = kindOf(e?.$type);
  return activities.has(k) || k.endsWith("Gateway") || k.endsWith("Event");
};
const textOf = (docs) => (Array.isArray(docs) ? docs.map((d) => (d?.text || "").trim()).filter(Boolean).join("\n\n") : "");

// lanesOf maps each flow node's ID to the name of the innermost lane that
// holds it.
function lanesOf(laneSets, out = new Map()) {
  for (const set of laneSets || []) {
    for (const lane of set?.lanes || []) {
      for (const node of lane.flowNodeRef || []) if (node?.id) out.set(node.id, lane.name || "");
      if (lane.childLaneSet) lanesOf([lane.childLaneSet], out);
    }
  }
  return out;
}

// notesOf maps each element's ID to the text annotations associated with
// it, whichever way the association points.
function notesOf(artifactLists) {
  const out = new Map();
  for (const list of artifactLists) {
    for (const a of list || []) {
      if (kindOf(a?.$type) !== "Association") continue;
      for (const [note, el] of [[a.targetRef, a.sourceRef], [a.sourceRef, a.targetRef]]) {
        if (kindOf(note?.$type) === "TextAnnotation" && el?.id && (note.text || "").trim()) {
          out.set(el.id, [...(out.get(el.id) || []), note.text.trim()]);
        }
      }
    }
  }
  return out;
}

// flowsOf maps each of a container's flow nodes to the sequence flows
// that leave it and counts those that reach it. They are read from the
// flows' sourceRef and targetRef, which BPMN requires; the nodes' own
// incoming and outgoing lists are written by some tools only.
function flowsOf(nodes, flows) {
  const outs = new Map(nodes.map((n) => [n, []]));
  const ins = new Map(nodes.map((n) => [n, 0]));
  for (const f of flows) {
    if (!outs.has(f.sourceRef) || !outs.has(f.targetRef)) continue;
    outs.get(f.sourceRef).push(f);
    ins.set(f.targetRef, ins.get(f.targetRef) + 1);
  }
  return { outs, ins };
}

// ordered lists a container's flow nodes in reading order: each branch up
// to where it joins, then the join and what follows; an event on a task
// right after the task. A loop is entered where the flow first reaches it.
function ordered(nodes, flows, { outs, ins }) {
  const inside = new Set(nodes);
  const waiting = new Map(ins);
  const boundaries = new Map();
  for (const n of nodes) {
    if (n.attachedToRef && inside.has(n.attachedToRef)) boundaries.set(n.attachedToRef, [...(boundaries.get(n.attachedToRef) || []), n]);
  }
  const attached = (n) => n.attachedToRef && inside.has(n.attachedToRef);
  const out = [];
  const seen = new Set();
  const stack = nodes.filter((n) => waiting.get(n) === 0 && !attached(n)).reverse();
  for (;;) {
    while (stack.length) {
      const n = stack.pop();
      if (seen.has(n)) continue;
      seen.add(n);
      out.push(n);
      const next = [];
      for (const f of outs.get(n)) {
        const t = f.targetRef;
        if (seen.has(t)) continue;
        waiting.set(t, waiting.get(t) - 1);
        if (waiting.get(t) <= 0) next.push(t);
      }
      stack.push(...next.reverse(), ...(boundaries.get(n) || []).filter((b) => !seen.has(b)).reverse());
    }
    // What is left waits on a loop: enter it where the flow reached it.
    const rest = nodes.filter((n) => !seen.has(n));
    if (!rest.length) return out;
    stack.push(rest.find((n) => flows.some((f) => f.targetRef === n && seen.has(f.sourceRef))) || rest[0]);
  }
}

function steps(container, depth, lanes, notes, inheritedLane) {
  const elements = container.flowElements || [];
  const nodes = elements.filter(isFlowNode);
  const flows = elements.filter((e) => kindOf(e?.$type) === "SequenceFlow");
  const graph = flowsOf(nodes, flows);
  const out = [];
  for (const n of ordered(nodes, flows, graph)) {
    const type = kindOf(n.$type);
    const kind = activities.has(type) ? "task" : type.endsWith("Gateway") ? "decision" : "event";
    const documentation = textOf(n.documentation);
    const own = notes.get(n.id) || [];
    const lane = lanes.get(n.id) ?? inheritedLane;
    const ways = graph.outs.get(n);
    const decides = kind === "decision" && ways.length > 1;
    if (kind === "task" || decides || documentation || own.length) {
      out.push({
        id: n.id, type, kind, name: n.name || "", lane, documentation, notes: own,
        paths: kind === "decision" ? ways.map((f) => ({
          name: f.name || "", condition: (f.conditionExpression?.body || "").trim(),
          target: f.targetRef?.name || "", targetId: f.targetRef?.id || "", isDefault: n.default === f,
        })) : [],
        depth,
      });
    }
    if (containers.has(type)) out.push(...steps(n, depth + 1, lanes, notes, lane));
  }
  return out;
}

// describe returns the processes of a model's definitions, each
// {id, name, documentation, steps}, a pool's name before its process's.
// A step is {id, type, kind ("task", "decision" or "event"), name, lane,
// documentation, notes, paths, depth}: every activity, every decision
// with more than one way out, and any other element the model describes;
// paths are a decision's ways out, {name, condition, target, targetId,
// isDefault}; depth counts the sub-processes around it.
export function describe(definitions) {
  const roots = definitions?.rootElements || [];
  const collaborations = roots.filter((r) => kindOf(r.$type) === "Collaboration");
  const named = [];
  for (const c of collaborations) {
    for (const p of c.participants || []) if (p.processRef) named.push([p.processRef, p.name || p.processRef.name || ""]);
  }
  for (const r of roots) if (kindOf(r.$type) === "Process" && !named.some(([p]) => p === r)) named.push([r, r.name || ""]);
  const shared = collaborations.map((c) => c.artifacts);
  const out = [];
  for (const [p, name] of named) {
    if (out.some((x) => x.process === p)) continue;
    const nested = [];
    const collect = (c) => {
      nested.push(c.artifacts);
      for (const e of c.flowElements || []) if (containers.has(kindOf(e.$type))) collect(e);
    };
    collect(p);
    out.push({ process: p, id: p.id || "", name, documentation: textOf(p.documentation),
      steps: steps(p, 0, lanesOf(p.laneSets), notesOf([...shared, ...nested]), "") });
  }
  return out.map(({ process, ...rest }) => rest);
}
