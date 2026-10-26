// BPMN diagrams in a text (ADR-0128): a model kept as a file of the item
// (block kind bpmn) or the newest version of a process of a process engine
// (block kind process), drawn by bpmn-js under the UI's own content
// security policy. The viewer is loaded when a diagram first comes into
// sight, the modeler when one is drawn. bpmn-js draws the bpmn.io
// watermark at the bottom right of every diagram; its license requires
// that it stays visible, so nothing here covers it.
import { h } from "./dom.js";
import { get } from "./api.js";
import { t } from "./i18n.js";

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

// diagramView shows a bpmn or a process block, in reading and in editing.
// files serves the item's files (bpmn); an editor also changes the caption,
// removes the block, and draws a stored model anew in the modeler.
export function diagramView(node, view, getPos, files) {
  const canvas = h("div", { class: "pm-diagram-canvas" });
  const info = h("span", { class: "pm-diagram-info" });
  const ops = h("span", { class: "pm-diagram-ops", role: "status" });
  const tools = h("span", { class: "pm-diagram-tools" });
  const error = h("p", { class: "error pm-diagram-error", role: "status" });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-diagram", contenteditable: "false" }, h("div", { class: "pm-diagram-head" }, info, ops, tools), canvas, error, caption);
  const isProcess = () => node.type.name === "process";
  let viewer = null;
  let drawn = null;
  let xml = "";
  let engine = null;
  let opsOn = false;
  let opsTimer = 0;
  let caught = null;

  const source = () => (isProcess() ? `p:${node.attrs.engine}/${node.attrs.process}` : `f:${node.attrs.ref}`);
  const fit = () => viewer?.get("canvas").zoom("fit-viewport", "auto");

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
      viewer ||= new Viewer({ container: canvas });
      await viewer.importXML(got);
      xml = got;
      fit();
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
  // there and are held there by an incident (ADR-0128), as atlas does.
  const showOperations = async () => {
    clearTimeout(opsTimer);
    if (!opsOn || !viewer) return;
    try {
      const { operations: o } = await get(processPath(node.attrs.engine, node.attrs.process) + "/operations");
      const overlays = viewer.get("overlays");
      const registry = viewer.get("elementRegistry");
      overlays.remove({ type: "pm-ops" });
      for (const el of o.elements) {
        if (!registry.get(el.element) || !(el.passed || el.tokens || el.incidents)) continue;
        const badge = (cls, n, title) => (n ? h("span", { class: "pm-ops-badge " + cls, title, text: String(n) }) : "");
        overlays.add(el.element, "pm-ops", { position: { bottom: 12, left: -8 }, html: h("span", { class: "pm-ops-badges" },
          badge("pm-ops-passed", el.passed, t("bpmn.opsPassed", { n: el.passed })),
          badge("pm-ops-tokens", el.tokens, t("bpmn.opsTokens", { n: el.tokens })),
          badge("pm-ops-incidents", el.incidents, t("bpmn.opsIncidents", { n: el.incidents }))) });
      }
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
    ops.textContent = "";
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
  const render = () => {
    const key = [source(), node.attrs.text, view.editable, drawn, engine?.operations, opsOn].join("\u0000");
    if (shown === key) return;
    shown = key;
    if (!isProcess()) info.textContent = t("bpmn.fileInfo");
    tools.replaceChildren(
      h("button", { type: "button", class: "link pm-diagram-fit", text: t("bpmn.fit"), disabled: !drawn, onclick: fit }),
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
  const stopEvent = (ev) => canvas.contains(ev.target) || caption.contains(ev.target) || tools.contains(ev.target);
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
// resolves to {engine, process, name} or to null when closed.
export function pickProcess() {
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { "aria-label": t("bpmn.engine"), class: "bpmn-engine" });
    const search = h("input", { type: "search", placeholder: t("bpmn.filter"), "aria-label": t("bpmn.filter") });
    const list = h("ul", { class: "bpmn-processes" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": t("bpmn.pickTitle") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("bpmn.pickTitle") }), close),
      h("div", { class: "bpmn-pick-body" }, h("label", { class: "field" }, t("bpmn.engine"), engines), search, status, list));
    let done = false;
    let shown = [];
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
        h("button", { type: "button", class: "link", onclick: () => finish({ engine: engines.value, process: p.process, name: p.name }) },
          h("strong", { text: p.name || p.process }), " ", h("span", { class: "muted", text: t("bpmn.processLine", { process: p.process, version: p.version }) })))));
    };
    const load = async () => {
      shown = [];
      list.replaceChildren();
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
        const { engines: all } = await get("/api/engines");
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
