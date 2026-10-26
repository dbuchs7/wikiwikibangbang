// Forms and decisions of process engines in a text (ADR-0143): a form as
// the engine keeps it (block kind form), drawn by form-js as a preview
// whose entries are never sent, and the newest deployed version of a
// decision (block kind decision), drawn by dmn-js: its decision table or
// literal expression, and the requirements graph of its model. Both are
// read through this server whenever they are shown, as a process is
// (ADR-0129); a viewer is loaded when its first block comes into sight.
// Both viewers draw the bpmn.io watermark; its license requires that it
// stays visible, so nothing here covers it.
import { h, when } from "./dom.js";
import { get } from "./api.js";
import { t } from "./i18n.js";
import { asDMN13, previewSchema, viewOf } from "./engineparts.js";

const enc = encodeURIComponent;
const pathOf = (what, engine, id) => `/api/engines/${enc(engine)}/${what}/${enc(id)}`;

const sheets = {};
// stylesheets adds a viewer's stylesheets to the page once.
function stylesheets(key, hrefs) {
  sheets[key] ||= Promise.all(hrefs.map((href) => new Promise((resolve) => {
    document.head.append(h("link", { rel: "stylesheet", href, onload: resolve, onerror: resolve }));
  })));
  return sheets[key];
}
const loadForm = async () => (await Promise.all([import("/vendor/form-viewer.js"), stylesheets("form", ["/vendor/form/form-js.css"])]))[0].Form;
const loadDMN = async () => (await Promise.all([import("/vendor/dmn-viewer.js"), stylesheets("dmn", ["/vendor/dmn/diagram-js.css",
  "/vendor/dmn/dmn-js-shared.css", "/vendor/dmn/dmn-js-drd.css", "/vendor/dmn/dmn-js-decision-table.css", "/vendor/dmn/dmn-js-literal-expression.css",
  "/vendor/dmn/dmn-js-boxed-expression.css", "/vendor/dmn/dmn-font/css/dmn.css"])]))[0].Viewer;

// blockView shows a form or a decision block, in reading and in editing,
// as a process block is shown (bpmn.js): a head that says what is shown,
// the canvas the part draws into, and the caption, which an editor changes
// and with which the block is removed. part names the block's source,
// reads it, draws it, saying in note what a reader should know of it,
// says what the head says and why it failed; it may add tools.
function blockView(node, view, getPos, part) {
  const info = h("span", { class: "pm-diagram-info" });
  const note = h("span", { class: "pm-diagram-ops", role: "status" });
  const tools = h("span", { class: "pm-diagram-tools" });
  const error = h("p", { class: "error pm-diagram-error", role: "status" });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-diagram " + part.class, contenteditable: "false" },
    h("div", { class: "pm-diagram-head" }, info, note, tools), part.canvas, error, caption);
  let drawn = null;
  let caught = false;

  const source = () => part.source(node.attrs);
  // draw reads the block's source and draws it, once per source, or again
  // when again is set (a reload).
  const draw = async (again = false) => {
    const key = source();
    if (drawn === key && !again) return;
    drawn = key;
    error.textContent = "";
    try {
      const got = await part.read(node.attrs);
      if (drawn !== key) return;
      await part.draw(got, note);
      info.textContent = part.info(got);
      dom.classList.add("pm-diagram-drawn");
    } catch (err) {
      if (drawn === key) {
        drawn = null;
        error.textContent = part.failed(err.message);
      }
    }
    render();
  };

  const editable = () => view.editable && !dom.classList.contains("pm-locked");
  const setAttrs = (attrs) => {
    const pos = getPos();
    if (pos === undefined || dom.classList.contains("pm-locked")) throw new Error(t("draw.locked"));
    view.dispatch(view.state.tr.setNodeMarkup(pos, null, { ...node.attrs, ...attrs }));
  };

  let shown = null;
  const render = () => {
    const key = [source(), node.attrs.text, view.editable, drawn].join("\u0000");
    if (shown === key) return;
    shown = key;
    tools.replaceChildren(...part.tools(), h("button", { type: "button", class: "link pm-diagram-refresh", text: t("bpmn.refresh"), onclick: () => draw(true) }));
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

  // A block is read and drawn when it first comes into sight: the viewers
  // need their container laid out, and a long text need not load them all.
  const sight = new IntersectionObserver((entries) => {
    if (entries.some((e) => e.isIntersecting)) {
      sight.disconnect();
      caught = true;
      draw();
    }
  });
  sight.observe(dom);
  render();

  // The canvas keeps its clicks, keys and wheel: a form is filled in, a
  // graph panned and zoomed, and the head and caption keep theirs.
  const stopEvent = (ev) => part.canvas.contains(ev.target) || caption.contains(ev.target) || tools.contains(ev.target);
  return {
    dom, stopEvent, ignoreMutation: () => true,
    update: (n) => {
      if (n.type !== node.type) return false;
      node = n;
      if (caught && drawn !== source()) draw();
      render();
      return true;
    },
    destroy: () => {
      sight.disconnect();
      part.destroy();
    },
  };
}

// formView shows a form block: the form as the engine keeps it, drawn by
// form-js as a preview. A reader may fill it in to see how it behaves, its
// conditions and checks included; nothing entered is sent or kept.
export function formView(node, view, getPos) {
  const canvas = h("div", { class: "pm-form-canvas" });
  let form = null;
  return blockView(node, view, getPos, {
    class: "pm-form",
    canvas,
    source: (a) => `${a.engine}/${a.form}`,
    read: (a) => get(pathOf("forms", a.engine, a.form)),
    draw: async ({ form: f }, note) => {
      const Form = await loadForm();
      form?.destroy();
      canvas.replaceChildren();
      form = new Form({ container: canvas });
      const fields = form.get("formFields");
      await form.importSchema(previewSchema(f.schema, (type) => Boolean(type && fields.get(type)), (type) => t("form.unsupported", { type })), {});
      form.on("submit", () => {
        note.textContent = t("form.notSent");
      });
      note.textContent = t("form.preview");
    },
    info: ({ engine, form: f }) => t("form.info", { name: f.name || f.form, saved: when(f.savedAt), engine: engine.name }),
    failed: (error) => t("form.notShown", { error }),
    tools: () => [],
    destroy: () => form?.destroy(),
  });
}

// decisionView shows a decision block: the newest deployed version of the
// decision, opened at its table or expression; a list switches to the
// model's other decisions and to its requirements graph, which pans,
// zooms and is fitted to the block.
export function decisionView(node, view, getPos) {
  const canvas = h("div", { class: "pm-dmn-canvas" });
  const views = h("select", { class: "pm-dmn-views", "aria-label": t("decision.view"), hidden: true });
  const fit = h("button", { type: "button", class: "link pm-diagram-fit", text: t("bpmn.fit"), hidden: true });
  let viewer = null;
  let list = [];
  let gone = false;
  const graph = () => !gone && viewer?.getActiveView()?.type === "drd";
  // fitGraph fits the graph to the block; the block has just changed its
  // height for it, which diagram-js learns first.
  const fitGraph = () => {
    if (!graph()) return;
    const c = viewer.getActiveViewer().get("canvas");
    c.resized();
    c.zoom("fit-viewport", "auto");
  };
  const sync = () => {
    const active = viewer.getActiveView();
    list = viewer.getViews();
    views.replaceChildren(...list.map((v, i) => h("option", { value: String(i), selected: v === active,
      text: v.type === "drd" ? t("decision.graph") : v.element.name || v.element.id })));
    views.hidden = list.length < 2;
    fit.hidden = !graph();
    canvas.classList.toggle("pm-dmn-graph", graph());
  };
  views.addEventListener("change", async () => {
    const next = list[Number(views.value)];
    canvas.classList.toggle("pm-dmn-graph", next.type === "drd");
    await viewer.open(next);
    fitGraph();
  });
  fit.addEventListener("click", fitGraph);
  return blockView(node, view, getPos, {
    class: "pm-dmn",
    canvas,
    source: (a) => `${a.engine}/${a.decision}`,
    read: (a) => get(pathOf("decisions", a.engine, a.decision)),
    draw: async ({ decision: d }, note) => {
      const Viewer = await loadDMN();
      if (!viewer) {
        viewer = new Viewer({ container: canvas });
        viewer.on("views.changed", () => {
          sync();
          requestAnimationFrame(fitGraph);
        });
      }
      const { xml, upgraded } = asDMN13(d.xml);
      const { warnings } = await viewer.importXML(xml);
      const own = viewOf(viewer.getViews(), d.decision);
      if (own && own !== viewer.getActiveView()) await viewer.open(own);
      sync();
      fitGraph();
      note.textContent = warnings.length ? t("decision.partly", { n: warnings.length }) : upgraded ? t("decision.asDMN13") : "";
    },
    info: ({ engine, decision: d }) => t("decision.info", { name: d.decision, model: d.model || d.decision, version: d.version, engine: engine.name }),
    failed: (error) => t("decision.notShown", { error }),
    tools: () => [views, fit],
    destroy: () => {
      gone = true;
      viewer?.destroy();
    },
  });
}

// pickFromEngine lets the editor choose a form ("forms") or a decision
// ("decisions") of a process engine; it resolves to {engine, id, name} or
// to null when closed. It is the process picker of ADR-0129 for them.
export function pickFromEngine(what) {
  const isForm = what === "forms";
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { "aria-label": t("bpmn.engine"), class: "bpmn-engine" });
    const filterLabel = isForm ? t("form.filter") : t("decision.filter");
    const search = h("input", { type: "search", placeholder: filterLabel, "aria-label": filterLabel });
    const list = h("ul", { class: "bpmn-processes" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const title = isForm ? t("form.pickTitle") : t("decision.pickTitle");
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": title },
      h("div", { class: "drawing-bar" }, h("strong", { text: title }), close),
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
    // Each entry: what is chosen, its name, and a line that tells it apart.
    const entries = (got) => (isForm
      ? got.forms.map((f) => ({ id: f.form, name: f.name || f.form, line: t("form.line", { form: f.form, saved: when(f.savedAt) }) }))
      : got.decisions.map((d) => ({ id: d.decision, name: d.decision, line: t("decision.line", { model: d.model || d.decision, version: d.version }) })));
    const filter = () => {
      const q = search.value.trim().toLowerCase();
      list.replaceChildren(...shown.filter((e) => !q || `${e.name} ${e.id} ${e.line}`.toLowerCase().includes(q)).map((e) => h("li", {},
        h("button", { type: "button", class: "link", onclick: () => finish({ engine: engines.value, id: e.id, name: e.name }) },
          h("strong", { text: e.name }), " ", h("span", { class: "muted", text: e.line })))));
    };
    const load = async () => {
      shown = [];
      list.replaceChildren();
      status.textContent = t("bpmn.loading");
      try {
        shown = entries(await get(`/api/engines/${enc(engines.value)}/${what}`));
        status.textContent = shown.length ? "" : isForm ? t("form.none") : t("decision.none");
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
