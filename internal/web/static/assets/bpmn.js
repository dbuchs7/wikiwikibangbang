// BPMN diagrams in a text (ADR-0129): a model kept as a file of the item
// (block kind bpmn) or the newest version of a process of a process engine
// (block kind process), drawn by bpmn-js under the UI's own content
// security policy. The viewer is loaded when a diagram first comes into
// sight, the modeler when one is drawn. bpmn-js draws the bpmn.io
// watermark at the bottom right of every diagram; its license requires
// that it stays visible, so nothing here covers it.
import { h } from "./dom.js";
import { get } from "./api.js";
import { t } from "./i18n.js";
import { describe as describeModel } from "./bpmndoc.js";

// BPMN ends the name of a model's file.
export const BPMN = ".bpmn";

// bpmnName makes a file name for a model from its caption.
export function bpmnName(caption) {
  return ((caption || "").replace(/[\\/\r\n]+/g, " ").trim().slice(0, 80) || t("bpmn.untitled")) + BPMN;
}

let styles = null;
// loadStyles adds bpmn-js's stylesheets to the page once.
function loadStyles() {
  styles ||= Promise.all(["/vendor/bpmn/diagram-js.css", "/vendor/bpmn/bpmn-js.css", "/vendor/bpmn/bpmn-font/css/bpmn.css"].map((href) =>
    new Promise((resolve) => {
      const link = h("link", { rel: "stylesheet", href, onload: resolve, onerror: resolve });
      document.head.append(link);
    })));
  return styles;
}
const loadViewer = async () => (await Promise.all([import("/vendor/bpmn-viewer.js"), loadStyles()]))[0].Viewer;
const loadModeler = async () => (await Promise.all([import("/vendor/bpmn-modeler.js"), loadStyles()]))[0].Modeler;

const enc = encodeURIComponent;
const processPath = (engine, process) => `/api/engines/${enc(engine)}/processes/${enc(process)}`;

// operationsEvery is how often the operations view looks again while it is
// on and the page is in sight.
const operationsEvery = 30_000;

// markOperations marks the elements of a drawn process with the counts of
// its operations o: how many tokens passed, wait there and are held there
// by an incident (ADR-0129), as atlas does.
function markOperations(viewer, o) {
  const overlays = viewer.get("overlays");
  const registry = viewer.get("elementRegistry");
  overlays.remove({ type: "pm-ops" });
  for (const el of o.elements) {
    if (!registry.get(el.element) || !(el.passed || el.tokens || el.incidents)) continue;
    overlays.add(el.element, "pm-ops", { position: { bottom: 12, left: -8 }, html: badgesOf(el) });
  }
}

// badgesOf shows the counts of one element: passed, waiting, held.
function badgesOf(el) {
  const badge = (cls, n, title) => (n ? h("span", { class: "pm-ops-badge " + cls, title, text: String(n) }) : "");
  return h("span", { class: "pm-ops-badges" },
    badge("pm-ops-passed", el.passed, t("bpmn.opsPassed", { n: el.passed })),
    badge("pm-ops-tokens", el.tokens, t("bpmn.opsTokens", { n: el.tokens })),
    badge("pm-ops-incidents", el.incidents, t("bpmn.opsIncidents", { n: el.incidents })));
}

// The views of a diagram (ADR-0143): its diagram alone, the diagram and
// its description, or the description alone.
const views = () => [["", t("bpmn.viewDiagram")], ["both", t("bpmn.viewBoth")], ["text", t("bpmn.viewText")]];

// kindLabel names an element's type, e.g. "User task"; a type without a
// name of its own keeps BPMN's.
function kindLabel(type) {
  const key = "bpmn.kind." + type;
  const label = t(key);
  return label === key ? type : label;
}

// pathText says where a decision's way leads: its name and condition, or
// that it is the default.
function pathText(w) {
  const label = [w.name, w.condition ? `(${w.condition})` : ""].filter(Boolean).join(" ") || t("bpmn.docPath");
  return t(w.isDefault ? "bpmn.docPathDefault" : "bpmn.docPathTo", { label, target: w.target || w.targetId });
}

// diagramView shows a bpmn or a process block, in reading and in editing.
// files serves the item's files (bpmn); an editor also changes the caption,
// removes the block, and draws a stored model anew in the modeler.
export function diagramView(node, view, getPos, files) {
  const canvas = h("div", { class: "pm-diagram-canvas" });
  const info = h("span", { class: "pm-diagram-info" });
  const ops = h("span", { class: "pm-diagram-ops", role: "status" });
  const tools = h("span", { class: "pm-diagram-tools" });
  const error = h("p", { class: "error pm-diagram-error", role: "status" });
  // The description of the process (ADR-0143), below the diagram or in
  // its place.
  const docs = h("section", { class: "pm-diagram-doc", hidden: true, "aria-label": t("bpmn.doc") });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-diagram", contenteditable: "false" }, h("div", { class: "pm-diagram-head" }, info, ops, tools), canvas, error, docs, caption);
  const isProcess = () => node.type.name === "process";
  const mode = () => node.attrs.view || "";
  let viewer = null;
  let drawn = null;
  let xml = "";
  let engine = null;
  let opsOn = false;
  let opsTimer = 0;
  let caught = null;
  // model is the description read with the last drawing; entries map an
  // element's ID to its entry; lit is the element shown from its entry;
  // counts are the last operations, for the entries.
  let model = null;
  const entries = new Map();
  let lit = null;
  let counts = null;

  const source = () => (isProcess() ? `p:${node.attrs.engine}/${node.attrs.process}` : `f:${node.attrs.ref}`);
  // fit shows the whole diagram. A canvas out of sight has no size to fit
  // to. diagram-js keeps the size it last saw, which is none when the model
  // was read with the description alone, so it is told to look again.
  const fit = () => {
    if (!viewer || !canvas.clientWidth || !canvas.clientHeight) return;
    const c = viewer.get("canvas");
    c.resized();
    c.zoom("fit-viewport", "auto");
  };

  const read = async () => {
    if (isProcess()) {
      const { engine: e, diagram } = await get(processPath(node.attrs.engine, node.attrs.process));
      engine = e;
      info.textContent = t("bpmn.processInfo", { name: diagram.name || diagram.process, version: diagram.version, engine: e.name });
      return diagram.xml;
    }
    if (!files) throw new Error(t("bpmn.noFile"));
    const res = await fetch(files.url(node.attrs.ref), { credentials: "same-origin" });
    if (!res.ok) throw new Error(res.status === 404 ? t("bpmn.noFile") : res.statusText);
    return res.text();
  };

  // draw reads the model and draws it, once per source, or again when
  // again is set (a refresh).
  const draw = async (again = false) => {
    const key = source();
    if (drawn === key && !again) return;
    drawn = key;
    error.textContent = "";
    try {
      const got = await read();
      if (drawn !== key) return;
      const Viewer = await loadViewer();
      if (!viewer) {
        viewer = new Viewer({ container: canvas });
        viewer.on("element.click", (e) => reveal(e.element));
      }
      await viewer.importXML(got);
      xml = got;
      lit = null;
      model = describeModel(viewer.getDefinitions());
      showDoc();
      if (mode() !== "text") fit();
      dom.classList.add("pm-diagram-drawn");
      if (opsOn) await showOperations();
    } catch (err) {
      if (drawn === key) {
        drawn = null;
        error.textContent = t("bpmn.notShown", { error: err.message });
      }
    }
    render();
  };

  // showOperations marks the elements with how many tokens passed, wait
  // there and are held there by an incident (ADR-0129), as atlas does.
  const showOperations = async () => {
    clearTimeout(opsTimer);
    if (!opsOn || !viewer) return;
    try {
      const { operations: o } = await get(processPath(node.attrs.engine, node.attrs.process) + "/operations");
      markOperations(viewer, o);
      markSteps(counts = o);
      ops.textContent = t("bpmn.opsSummary", { version: o.version, instances: o.instances, finished: o.finished, incidents: o.incidents });
      ops.classList.toggle("error", o.incidents > 0);
    } catch (err) {
      ops.textContent = err.message;
      ops.classList.add("error");
    }
    const tick = () => {
      if (!opsOn || !dom.isConnected) return;
      if (document.visibilityState !== "visible") opsTimer = setTimeout(tick, operationsEvery);
      else showOperations();
    };
    opsTimer = setTimeout(tick, operationsEvery);
  };
  const setOperations = (on) => {
    opsOn = on;
    clearTimeout(opsTimer);
    if (on) return showOperations();
    viewer?.get("overlays").remove({ type: "pm-ops" });
    markSteps(counts = null);
    ops.textContent = "";
  };

  // showDoc lists the processes and their tasks and decisions (ADR-0143).
  // With the diagram beside it, a title shows its element there, and an
  // element clicked there shows its entry here.
  const showDoc = () => {
    entries.clear();
    if (!mode() || !model) return docs.replaceChildren();
    const linked = mode() === "both";
    const parts = [];
    for (const p of model) {
      if (model.length > 1 || p.name || p.documentation) parts.push(h("p", { class: "pm-doc-process" }, h("strong", { text: p.name || p.id })));
      if (p.documentation) parts.push(h("p", { class: "pm-doc-text", text: p.documentation }));
      if (!p.steps.length) {
        parts.push(h("p", { class: "muted", text: t("bpmn.docEmpty") }));
        continue;
      }
      parts.push(h("ol", { class: "pm-doc-steps" }, p.steps.map((s) => {
        const name = s.name || t("bpmn.docUnnamed");
        const tally = h("span", { class: "pm-doc-ops" });
        const li = h("li", { class: "pm-doc-step pm-doc-" + s.kind, "data-element": s.id },
          h("div", { class: "pm-doc-head" },
            linked ? h("button", { type: "button", class: "link pm-doc-title", text: name, onclick: () => highlight(s.id, true) }) : h("strong", { class: "pm-doc-title", text: name }),
            " ", h("span", { class: "pm-doc-kind", text: [kindLabel(s.type), s.lane].filter(Boolean).join(" · ") }), tally),
          s.documentation ? h("p", { class: "pm-doc-text", text: s.documentation }) : s.kind === "task" ? h("p", { class: "muted pm-doc-text", text: t("bpmn.docNone") }) : "",
          s.notes.map((n) => h("p", { class: "pm-doc-note", text: t("bpmn.docNote", { note: n }) })),
          s.paths.length ? h("ul", { class: "pm-doc-paths" }, s.paths.map((w) => h("li", { text: pathText(w) }))) : "");
        if (s.depth) li.style.marginLeft = `${s.depth * 1.5}em`;
        entries.set(s.id, { li, tally });
        return li;
      })));
    }
    docs.replaceChildren(...parts);
    markSteps(counts);
  };
  // markSteps shows the operations' counts at the entries too.
  const markSteps = (o) => {
    for (const { tally } of entries.values()) tally.replaceChildren();
    for (const el of o?.elements || []) {
      const e = entries.get(el.element);
      if (e && (el.passed || el.tokens || el.incidents)) e.tally.replaceChildren(badgesOf(el));
    }
  };
  // highlight marks an element in the diagram; from its entry, the diagram
  // also brings it into sight.
  const highlight = (id, bring) => {
    if (!viewer || mode() !== "both") return;
    const c = viewer.get("canvas");
    const el = viewer.get("elementRegistry").get(id);
    if (!el) return;
    if (lit && viewer.get("elementRegistry").get(lit)) c.removeMarker(lit, "pm-doc-lit");
    c.addMarker(id, "pm-doc-lit");
    lit = id;
    if (!bring) return;
    c.scrollToElement?.(el);
    canvas.scrollIntoView({ block: "nearest", behavior: "smooth" });
  };
  // reveal shows an element's entry when it is clicked in the diagram.
  const reveal = (el) => {
    const target = el?.labelTarget || el;
    const e = target && entries.get(target.id);
    if (!e || mode() !== "both") return;
    highlight(target.id, false);
    e.li.scrollIntoView({ block: "nearest", behavior: "smooth" });
    e.li.classList.remove("pm-doc-flash");
    void e.li.offsetWidth; // restarts the flash
    e.li.classList.add("pm-doc-flash");
  };

  const editable = () => view.editable && !dom.classList.contains("pm-locked");
  const setAttrs = (attrs) => {
    const pos = getPos();
    if (pos === undefined || dom.classList.contains("pm-locked")) throw new Error(t("draw.locked"));
    view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, ...attrs }));
  };
  // A stored model is drawn anew in the modeler and saved as a new version
  // of its file (ADR-0103); the block then shows it. The former file stays,
  // as earlier versions of the body show it (ADR-0043).
  const redraw = async () => {
    error.textContent = "";
    try {
      const a = await openModeler(xml || await read(), (out) =>
        files.upload(new File([out], bpmnName(node.attrs.text), { type: "application/xml" }), { item: files.item, attachment: node.attrs.ref }));
      if (a) setAttrs({ ref: a.id });
    } catch (err) {
      error.textContent = err.message;
    }
  };

  let shown = null;
  let shownMode = null;
  const render = () => {
    const key = [source(), node.attrs.text, view.editable, drawn, engine?.operations, opsOn, mode()].join("\u0000");
    if (shown === key) return;
    shown = key;
    if (!isProcess()) info.textContent = t("bpmn.fileInfo");
    canvas.hidden = mode() === "text";
    docs.hidden = !mode();
    if (shownMode !== mode()) {
      const was = shownMode;
      shownMode = mode();
      if (model) showDoc();
      // A canvas shown again is laid out first, then fitted.
      if (was === "text" && viewer) requestAnimationFrame(fit);
    }
    tools.replaceChildren(
      view.editable ? h("select", { class: "pm-diagram-view", "aria-label": t("bpmn.view"), onchange: (e) => {
        try {
          setAttrs({ view: e.target.value });
        } catch {
          render(shown = null);
        }
      } }, views().map(([v, label]) => h("option", { value: v, text: label, selected: v === mode() }))) : "",
      mode() !== "text" ? h("button", { type: "button", class: "link pm-diagram-fit", text: t("bpmn.fit"), disabled: !drawn, onclick: fit }) : "",
      isProcess() ? h("button", { type: "button", class: "link pm-diagram-refresh", text: t("bpmn.refresh"), onclick: () => draw(true) }) : "",
      isProcess() && engine?.operations ? h("label", { class: "check pm-diagram-operations" },
        h("input", { type: "checkbox", checked: opsOn, onchange: (e) => setOperations(e.target.checked) }), " ", t("bpmn.operations")) : "",
      !isProcess() && files ? h("a", { class: "pm-diagram-download", href: files.url(node.attrs.ref), text: t("bpmn.download") }) : "",
      !isProcess() && files && view.editable ? h("button", { type: "button", class: "link pm-diagram-edit", text: t("bpmn.edit"), onclick: redraw }) : "");
    if (!view.editable) {
      caption.replaceChildren(node.attrs.text);
      caption.hidden = !node.attrs.text;
      return;
    }
    caption.hidden = false;
    const input = h("input", { value: node.attrs.text, placeholder: t("editor.caption"), "aria-label": t("editor.caption") });
    input.addEventListener("change", () => {
      try {
        setAttrs({ text: input.value.replace(/[\r\n]+/g, " ") });
      } catch {
        render(shown = null);
      }
    });
    const remove = h("button", { type: "button", class: "link pm-image-remove", text: t("editor.imageRemove"), onclick: () => {
      const pos = getPos();
      if (pos === undefined || !editable()) return;
      view.dispatch(view.state.tr.delete(pos, pos + node.nodeSize));
      view.focus();
    } });
    caption.replaceChildren(input, " ", remove);
  };

  // A diagram is read and drawn when it first comes into sight: bpmn-js
  // needs its container laid out, and a long text need not load them all.
  const sight = new IntersectionObserver((entries) => {
    if (entries.some((e) => e.isIntersecting)) {
      sight.disconnect();
      caught = true;
      draw();
    }
  });
  sight.observe(dom);
  render();

  // The canvas keeps its clicks, drags and wheel for panning and zooming,
  // and the head and caption theirs.
  const stopEvent = (ev) => canvas.contains(ev.target) || caption.contains(ev.target) || tools.contains(ev.target) || docs.contains(ev.target);
  return {
    dom, stopEvent, ignoreMutation: () => true,
    update: (n) => {
      if (n.type !== node.type) return false;
      node = n;
      if (caught && drawn !== source()) {
        engine = null;
        setOperations(false);
        draw();
      }
      render();
      return true;
    },
    destroy: () => {
      sight.disconnect();
      clearTimeout(opsTimer);
      opsOn = false;
      viewer?.destroy();
    },
  };
}

// openModeler shows the BPMN modeler in a dialog, with xml to change a
// model or without to start one. store(xml) keeps the model, e.g. as a
// file of the item; the dialog stays open, saving, until it has, and shows
// what went wrong if it fails. It resolves to what store returns, or to
// null when the dialog is closed without saving.
export function openModeler(xml = "", store = async (out) => out) {
  return new Promise((resolve) => {
    const status = h("span", { class: "error", role: "status" });
    const save = h("button", { type: "button", class: "primary", text: t("bpmn.save"), disabled: true });
    const cancel = h("button", { type: "button", text: t("draw.cancel") });
    const container = h("div", { class: "bpmn-modeler-canvas" });
    const dialog = h("dialog", { class: "drawing bpmn-modeler", "aria-label": t("bpmn.title") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("bpmn.title") }), status, cancel, save), container);
    let modeler = null;
    let done = false;
    const leave = () => finish(null);
    const finish = (result) => {
      if (done) return;
      done = true;
      window.removeEventListener("hashchange", leave);
      modeler?.destroy();
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    save.addEventListener("click", async () => {
      status.classList.remove("error");
      status.textContent = t("bpmn.saving");
      save.disabled = cancel.disabled = true;
      try {
        const { xml: out } = await modeler.saveXML({ format: true });
        finish(await store(out));
      } catch (err) {
        status.classList.add("error");
        status.textContent = err.message;
        save.disabled = cancel.disabled = false;
      }
    });
    cancel.addEventListener("click", () => finish(null));
    window.addEventListener("hashchange", leave);
    // Escape belongs to the modeler (it ends a tool); the dialog closes by
    // its buttons only.
    dialog.addEventListener("cancel", (e) => e.preventDefault());
    document.body.append(dialog);
    dialog.showModal();
    (async () => {
      try {
        const Modeler = await loadModeler();
        modeler = new Modeler({ container });
        if (xml) await modeler.importXML(xml);
        else await modeler.createDiagram();
        modeler.get("canvas").zoom("fit-viewport", "auto");
        save.disabled = false;
      } catch (err) {
        status.textContent = t("bpmn.notShown", { error: err.message });
      }
    })();
  });
}

// pickProcess lets the reader choose a process of a process engine; it
// resolves to {engine, process, name} or to null when closed. With
// operations set, as for a drawing (ADR-0142), it also asks whether the
// operations are shown at first, where the engine allows them, and adds
// operations to what it resolves to.
export function pickProcess({ operations = false } = {}) {
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { "aria-label": t("bpmn.engine"), class: "bpmn-engine" });
    const search = h("input", { type: "search", placeholder: t("bpmn.filter"), "aria-label": t("bpmn.filter") });
    const list = h("ul", { class: "bpmn-processes" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const opsBox = h("input", { type: "checkbox", class: "bpmn-pick-operations", checked: true });
    const opsField = h("label", { class: "check", hidden: true }, opsBox, " ", t("draw.processOperations"));
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": t("bpmn.pickTitle") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("bpmn.pickTitle") }), close),
      h("div", { class: "bpmn-pick-body" }, h("label", { class: "field" }, t("bpmn.engine"), engines), opsField, search, status, list));
    let done = false;
    let shown = [];
    let all = [];
    const allowed = () => all.some((e) => e.id === engines.value && e.operations);
    const finish = (result) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    const filter = () => {
      const q = search.value.trim().toLowerCase();
      list.replaceChildren(...shown.filter((p) => !q || `${p.name} ${p.process}`.toLowerCase().includes(q)).map((p) => h("li", {},
        h("button", { type: "button", class: "link", onclick: () => finish({ engine: engines.value, process: p.process, name: p.name,
          ...(operations ? { operations: allowed() && opsBox.checked } : {}) }) },
          h("strong", { text: p.name || p.process }), " ", h("span", { class: "muted", text: t("bpmn.processLine", { process: p.process, version: p.version }) })))));
    };
    const load = async () => {
      shown = [];
      list.replaceChildren();
      opsField.hidden = !operations || !allowed();
      status.textContent = t("bpmn.loading");
      try {
        shown = (await get(`/api/engines/${enc(engines.value)}/processes`)).processes;
        status.textContent = shown.length ? "" : t("bpmn.noProcesses");
      } catch (err) {
        status.textContent = err.message;
      }
      filter();
    };
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    search.addEventListener("input", filter);
    engines.addEventListener("change", load);
    document.body.append(dialog);
    dialog.showModal();
    (async () => {
      try {
        ({ engines: all } = await get("/api/engines"));
        if (!all.length) {
          status.textContent = t("bpmn.noEngines");
          engines.disabled = search.disabled = true;
          return;
        }
        engines.replaceChildren(...all.map((e) => h("option", { value: e.id, text: e.name })));
        await load();
      } catch (err) {
        status.textContent = err.message;
      }
    })();
  });
}

// snapshot draws a model as a picture for a drawing (ADR-0142): an SVG as
// a data URL, with its width and height, on white, as diagrams are shown.
// bpmn-js lays the model out in a box out of sight. The SVG is changed as
// text: parsed into a document, its style attributes would break the UI's
// content security policy.
async function snapshot(xml) {
  const Viewer = await loadViewer();
  const box = h("div", { class: "bpmn-snapshot", "aria-hidden": "true" });
  document.body.append(box);
  const viewer = new Viewer({ container: box });
  try {
    await viewer.importXML(xml);
    const { svg } = await viewer.saveSVG();
    const root = /<svg\b[^>]*>/.exec(svg);
    const [x, y, width, height] = (/\sviewBox="([^"]*)"/.exec(root?.[0] || "")?.[1] || "").split(/[\s,]+/).map(Number);
    if (root[0].endsWith("/>") || ![x, y, width, height].every(Number.isFinite) || width <= 0 || height <= 0) throw new Error(t("bpmn.notShown", { error: "SVG" }));
    const at = root.index + root[0].length;
    const white = `<rect x="${x}" y="${y}" width="${width}" height="${height}" fill="#ffffff"/>`;
    const data = await new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(new Blob([svg.slice(0, at) + white + svg.slice(at)], { type: "image/svg+xml" }));
    });
    return { svg: data, width, height };
  } finally {
    viewer.destroy();
    box.remove();
  }
}

// pickModel lets the editor choose a BPMN model for a drawing: one of the
// item's files, or one from this device. It resolves to {name, xml} or to
// null when closed.
export function pickModel(files) {
  return new Promise((resolve) => {
    const status = h("p", { class: "error", role: "status" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const device = h("input", { type: "file", accept: ".bpmn,.xml,application/xml,text/xml", "aria-label": t("draw.processDevice") });
    const models = (files?.list?.() || []).filter((a) => /^text\/(xml|plain)/.test(a.type || "") && /\.(bpmn|xml)$/i.test(a.name || ""));
    const list = h("ul", { class: "bpmn-processes" });
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": t("draw.processFile") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("draw.processFile") }), close),
      h("div", { class: "bpmn-pick-body" },
        h("p", { class: "muted", text: models.length ? t("draw.processFiles") : t("draw.processNoFiles") }), list,
        h("label", { class: "field" }, t("draw.processDevice"), device), status));
    let done = false;
    const finish = (result) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    const take = async (name, read) => {
      status.textContent = "";
      try {
        finish({ name: name.replace(/\.[^.]*$/, ""), xml: await read() });
      } catch (err) {
        status.textContent = err.message;
      }
    };
    list.replaceChildren(...models.map((a) => h("li", {}, h("button", { type: "button", class: "link", text: a.name, onclick: () => take(a.name, async () => {
      const res = await fetch(files.url(a.id), { credentials: "same-origin" });
      if (!res.ok) throw new Error(res.statusText);
      return res.text();
    }) }))));
    device.addEventListener("change", () => device.files[0] && take(device.files[0].name, () => device.files[0].text()));
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    document.body.append(dialog);
    dialog.showModal();
  });
}

// processPicture is the picture of a process's newest version.
async function processPicture(engine, process, operations) {
  const { diagram } = await get(processPath(engine, process));
  return { ...(await snapshot(diagram.xml)), data: { kind: "process", engine, process, name: diagram.name || process, operations } };
}

// drawingProcesses answers the drawing page's asks for a process (ADR-0142)
// with a picture and what it shows, or null when the editor gave up: a
// model drawn anew in the modeler, one chosen from the item's files or
// this device, or a process of an engine. Edited, a model is drawn again
// in the modeler and a process of an engine is read again in its newest
// version. A model is kept in the drawing, not as a file of the item: the
// drawing is its one source, wherever it is copied to.
export function drawingProcesses(files) {
  return async (want, current) => {
    if (want === "new" || (want === "edit" && current?.kind === "bpmn")) {
      const xml = await openModeler(current?.xml || "");
      return xml ? { ...(await snapshot(xml)), data: { kind: "bpmn", name: current?.name || "", xml } } : null;
    }
    if (want === "file") {
      const m = await pickModel(files);
      return m ? { ...(await snapshot(m.xml)), data: { kind: "bpmn", name: m.name, xml: m.xml } } : null;
    }
    if (want === "process") {
      const p = await pickProcess({ operations: true });
      return p ? processPicture(p.engine, p.process, p.operations) : null;
    }
    if (want === "edit" && current?.kind === "process") return processPicture(current.engine, current.process, current.operations);
    return null;
  };
}

// processRegion shows the newest version of a process of an engine over
// its picture in a drawing (ADR-0142), and its operations while they are
// on: at first when the drawing says so and the engine allows them, then
// as the reader switches them. The picture shows through until the
// process is drawn, and stays when it cannot be: it is what was saved.
// The region does not pan or zoom; the page scrolls over it.
export function processRegion(r) {
  const canvas = h("span", { class: "pm-live-canvas" });
  const toggle = h("button", { type: "button", class: "pm-live-ops", hidden: true, "aria-pressed": "false", text: t("bpmn.operations") });
  const state = h("span", { class: "pm-live-state", role: "status" });
  const dom = h("span", { class: "pm-live-region", title: r.name }, canvas, h("span", { class: "pm-live-bar" }, toggle, state));
  for (const [k, v] of [["left", r.x], ["top", r.y], ["width", r.w], ["height", r.h]]) dom.style[k] = `${v * 100}%`;
  let viewer = null;
  let version = 0;
  let opsOn = false;
  let timer = 0;
  let gone = false;

  const draw = async () => {
    const { engine, diagram } = await get(processPath(r.engine, r.process));
    const Viewer = await loadViewer();
    if (gone) return engine;
    viewer ||= new Viewer({ container: canvas, zoomScroll: { enabled: false } });
    await viewer.importXML(diagram.xml);
    version = diagram.version;
    viewer.get("canvas").zoom("fit-viewport", "auto");
    dom.classList.add("pm-live-drawn");
    dom.title = t("bpmn.processInfo", { name: diagram.name || diagram.process, version, engine: engine.name });
    toggle.hidden = !engine.operations;
    return engine;
  };
  const showOperations = async () => {
    clearTimeout(timer);
    if (!opsOn || gone) return;
    try {
      const { operations: o } = await get(processPath(r.engine, r.process) + "/operations");
      // A newer version deployed since is drawn before it is counted on.
      if (o.version !== version) await draw();
      if (!opsOn || gone) return;
      markOperations(viewer, o);
      state.textContent = t("bpmn.opsSummary", { version: o.version, instances: o.instances, finished: o.finished, incidents: o.incidents });
      state.classList.toggle("error", o.incidents > 0);
    } catch (err) {
      state.textContent = err.message;
      state.classList.add("error");
    }
    const tick = () => {
      if (!opsOn || gone || !dom.isConnected) return;
      if (document.visibilityState !== "visible") timer = setTimeout(tick, operationsEvery);
      else showOperations();
    };
    timer = setTimeout(tick, operationsEvery);
  };
  const setOperations = (on) => {
    opsOn = on;
    toggle.setAttribute("aria-pressed", String(on));
    clearTimeout(timer);
    if (on) return showOperations();
    viewer?.get("overlays").remove({ type: "pm-ops" });
    state.textContent = "";
    state.classList.remove("error");
  };
  toggle.addEventListener("click", () => setOperations(!opsOn));

  const sight = new IntersectionObserver((entries) => {
    if (!entries.some((e) => e.isIntersecting)) return;
    sight.disconnect();
    draw().then((engine) => setOperations(r.operations && engine.operations), (err) => {
      if (gone) return;
      dom.classList.remove("pm-live-drawn");
      dom.classList.add("pm-live-failed");
      state.textContent = t("draw.processAsSaved");
      state.title = err.message;
    });
  });
  sight.observe(dom);
  return {
    dom,
    destroy: () => {
      gone = true;
      sight.disconnect();
      clearTimeout(timer);
      viewer?.destroy();
    },
  };
}
