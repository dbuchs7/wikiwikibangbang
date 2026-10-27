// Forms and decisions of process engines in a text (ADR-0144): a form as
// the engine keeps it (block kind form), drawn by form-js as a preview
// whose entries are never sent, and the newest deployed version of a
// decision (block kind decision), drawn by dmn-js: its decision table or
// literal expression, and the requirements graph of its model. Both are
// read through this server whenever they are shown, as a process is
// (ADR-0129); a viewer is loaded when its first block comes into sight.
// Both viewers draw the bpmn.io watermark; its license requires that it
// stays visible, so nothing here covers it.
//
// A form is chosen among the forms the steps of a process name, and may
// show the list of its fields below it or alone; a start form (block kind
// startform) shows the form of a public start link, filled in beforehand
// with what this site knows of the reader where the editor chose, and
// sends it through this server to the engine's public route, which starts
// the link's process as anyone with the link does (ADR-0147).
import { h, when } from "./dom.js";
import { get, post } from "./api.js";
import { t } from "./i18n.js";
import { asDMN13, fieldsOf, fillable, linkOf, prefillData, prefillSources, previewSchema, suggestPrefill, viewOf } from "./engineparts.js";

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
// says what the head says and why it failed; it may add tools, given the
// block's attributes and a way to change them, and an element below the
// canvas (after).
function blockView(node, view, getPos, part) {
  const info = h("span", { class: "pm-diagram-info" });
  const note = h("span", { class: "pm-diagram-ops", role: "status" });
  const tools = h("span", { class: "pm-diagram-tools" });
  const error = h("p", { class: "error pm-diagram-error", role: "status" });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-diagram " + part.class, contenteditable: "false" },
    h("div", { class: "pm-diagram-head" }, info, note, tools), part.canvas, part.after || "", error, caption);
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
    const ctl = { attrs: node.attrs, editable: editable(), set: (attrs) => {
      try {
        setAttrs(attrs);
      } catch {
        render(shown = null);
      }
    } };
    tools.replaceChildren(...part.tools(ctl), h("button", { type: "button", class: "link pm-diagram-refresh", text: t("bpmn.refresh"), onclick: () => draw(true) }));
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
  const stopEvent = (ev) => part.canvas.contains(ev.target) || part.after?.contains(ev.target) || caption.contains(ev.target) || tools.contains(ev.target);
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

// The views of a form block: the form, the form and the list of its
// fields, or the list alone, as a process block offers its diagram and its
// description (ADR-0143).
const formViews = () => [["", t("form.viewForm")], ["both", t("form.viewBoth")], ["text", t("form.viewText")]];

// typeName names the kind of a form-js field for its description.
const typeName = (type) => ({
  textfield: t("form.typeText"), textarea: t("form.typeTextarea"), number: t("form.typeNumber"), datetime: t("form.typeDate"),
  checkbox: t("form.typeCheckbox"), checklist: t("form.typeChecklist"), radio: t("form.typeRadio"), select: t("form.typeSelect"),
  taglist: t("form.typeTaglist"), filepicker: t("form.typeFile"), expression: t("form.typeExpression"),
  group: t("form.typeGroup"), dynamiclist: t("form.typeList"),
}[type] || type);

// stepKind names the kind of a step that names a form.
const stepKind = (kind) => (kind === "startEvent" ? t("form.stepStart") : kind === "userTask" ? t("form.stepTask") : t("form.stepRepair"));

// fieldList describes a form: the steps of its process that name it, then
// each field with its label, kind, key, whether it is required, its
// description and its options; a group or a list heads its fields.
function fieldList(f) {
  const fields = fieldsOf(f.schema);
  const parts = [];
  if (f.steps?.length) {
    parts.push(h("p", { class: "pm-doc-text muted", text: t("form.usedAt", { steps: f.steps.map((s) => `${s.name || s.element} (${stepKind(s.kind)})`).join(", ") }) }));
  }
  if (!fields.some((x) => !x.group)) {
    parts.push(h("p", { class: "muted", text: t("form.noFields") }));
    return parts;
  }
  parts.push(h("ol", { class: "pm-doc-steps pm-form-fields" }, fields.map((x) => {
    const li = x.group
      ? h("li", { class: "pm-doc-step pm-form-group" }, h("div", { class: "pm-doc-head" }, h("strong", { class: "pm-doc-title", text: x.label || typeName(x.type) }),
        " ", h("span", { class: "pm-doc-kind", text: typeName(x.type) })))
      : h("li", { class: "pm-doc-step" },
        h("div", { class: "pm-doc-head" }, h("strong", { class: "pm-doc-title", text: x.label || x.key }),
          " ", h("span", { class: "pm-doc-kind", text: [typeName(x.type), x.key, x.required ? t("form.required") : "", x.hidden ? t("form.conditional") : ""].filter(Boolean).join(" · ") })),
        x.description ? h("p", { class: "pm-doc-text", text: x.description }) : "",
        x.options.length ? h("ul", { class: "pm-doc-paths" }, x.options.map((o) => h("li", { text: o }))) : "");
    if (x.depth) li.style.marginLeft = `${x.depth * 1.5}em`;
    return li;
  })));
  return parts;
}

// formView shows a form block: the form as the engine keeps it, drawn by
// form-js as a preview, the list of its fields, or both. A reader may fill
// the form in to see how it behaves, its conditions and checks included;
// nothing entered is sent or kept. A form chosen among a process's steps
// is read as the form of that process's steps.
export function formView(node, view, getPos) {
  const canvas = h("div", { class: "pm-form-canvas" });
  const docs = h("section", { class: "pm-diagram-doc pm-form-doc", hidden: true, "aria-label": t("form.fields") });
  let form = null;
  const mode = (a) => a.view || "";
  return blockView(node, view, getPos, {
    class: "pm-form",
    canvas,
    after: docs,
    source: (a) => [a.engine, a.form, a.process || "", mode(a)].join("\u0000"),
    read: async (a) => ({ ...(await get(pathOf("forms", a.engine, a.form) + (a.process ? `?process=${enc(a.process)}` : ""))), mode: mode(a) }),
    draw: async ({ form: f, mode: m }, note) => {
      canvas.hidden = m === "text";
      docs.hidden = !m;
      docs.replaceChildren(...(m ? fieldList(f) : []));
      form?.destroy();
      form = null;
      canvas.replaceChildren();
      note.textContent = "";
      if (m === "text") return;
      const Form = await loadForm();
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
    tools: (ctl) => (ctl.editable ? [h("label", { class: "pm-diagram-view-label" }, t("bpmn.view"), h("select", { class: "pm-diagram-view", onchange: (e) => ctl.set({ view: e.target.value }) },
      formViews().map(([v, label]) => h("option", { value: v, text: label, selected: v === mode(ctl.attrs) }))))] : []),
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
// ("decisions") of a process engine; it resolves to {engine, id, name,
// process} or to null when closed. It is the process picker of ADR-0129
// for them. A form is chosen among the forms the steps of a process name:
// the processes are listed first, then the steps of the one chosen
// (ADR-0147).
export function pickFromEngine(what) {
  const isForm = what === "forms";
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { "aria-label": t("bpmn.engine"), class: "bpmn-engine" });
    const filterLabel = isForm ? t("form.filter") : t("decision.filter");
    const search = h("input", { type: "search", placeholder: filterLabel, "aria-label": filterLabel });
    const back = h("button", { type: "button", class: "link", text: t("form.back"), hidden: true });
    const list = h("ul", { class: "bpmn-processes" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const title = isForm ? t("form.pickTitle") : t("decision.pickTitle");
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": title },
      h("div", { class: "drawing-bar" }, h("strong", { text: title }), close),
      h("div", { class: "bpmn-pick-body" }, h("label", { class: "field" }, t("bpmn.engine"), engines), search, back, status, list));
    let done = false;
    let shown = [];
    const finish = (result) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    // Each entry: its name, a line that tells it apart, and what choosing
    // it does.
    const filter = () => {
      const q = search.value.trim().toLowerCase();
      list.replaceChildren(...shown.filter((e) => !q || `${e.name} ${e.line}`.toLowerCase().includes(q)).map((e) => h("li", {},
        h("button", { type: "button", class: "link", onclick: e.pick },
          h("strong", { text: e.name }), " ", h("span", { class: "muted", text: e.line })))));
    };
    const show = async (read, none) => {
      shown = [];
      list.replaceChildren();
      status.textContent = t("bpmn.loading");
      try {
        shown = await read();
        status.textContent = shown.length ? "" : none;
      } catch (err) {
        status.textContent = err.message;
      }
      filter();
    };
    // steps lists the steps of a process that name a form.
    const steps = (p) => {
      back.hidden = false;
      search.value = "";
      return show(async () => {
        const { process: pf } = await get(`/api/engines/${enc(engines.value)}/processes/${enc(p.process)}/forms`);
        return pf.steps.map((st) => ({ name: st.name || st.element, line: t("form.stepLine", { kind: stepKind(st.kind), form: st.form }),
          pick: () => finish({ engine: engines.value, id: st.form, process: p.process, name: st.name || p.name || st.form }) }));
      }, t("form.noSteps"));
    };
    const load = () => {
      back.hidden = true;
      return show(async () => (isForm
        ? (await get(`/api/engines/${enc(engines.value)}/processes`)).processes.map((p) => ({ name: p.name || p.process,
          line: t("form.processLine", { process: p.process, version: p.version }), pick: () => steps(p) }))
        : (await get(`/api/engines/${enc(engines.value)}/decisions`)).decisions.map((d) => ({ name: d.decision,
          line: t("decision.line", { model: d.model || d.decision, version: d.version }), pick: () => finish({ engine: engines.value, id: d.decision, name: d.decision }) }))),
      isForm ? t("bpmn.noProcesses") : t("decision.none"));
    };
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    search.addEventListener("input", filter);
    engines.addEventListener("change", load);
    back.addEventListener("click", load);
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

// linkPath is the path of a start link's form at this server.
const linkPath = (engine, link) => `/api/engines/${enc(engine)}/public-links/${enc(link)}`;

// sourceName names what a field is filled in with.
const sourceName = (from) => ({ username: t("start.fromUsername"), email: t("start.fromEmail"), page: t("start.fromPage"), url: t("start.fromUrl") }[from] || from);

// reader returns what this site knows of the reader and the page for the
// sources a start form fills in from: the username, one's own mail
// address, and the page's title and address. Only what is asked for is
// read; what cannot be read is left out.
async function reader(prefill, item) {
  const wants = new Set((prefill || []).map((p) => p.from));
  const known = {};
  const tries = [];
  if (wants.has("username")) tries.push(get("/api/me").then((me) => { known.username = me.user?.username || ""; }));
  if (wants.has("email")) tries.push(get("/api/me/contacts").then((c) => { known.email = c.email || ""; }));
  if ((wants.has("page") || wants.has("url")) && item) {
    tries.push(get("/api/items/" + enc(item)).then(({ item: it }) => {
      known.page = it.title || "";
      known.url = `${location.origin}${location.pathname}#/item/${encodeURIComponent(it.key)}`;
    }));
  }
  await Promise.allSettled(tries);
  return known;
}

// startFormView shows a start form block (ADR-0147): the form of a public
// start link, filled in beforehand where the editor chose, with a button
// that sends it. It is sent through this server to the engine's public
// route, without the engine's credential: the engine learns what is
// entered, not who sent it. A process that leads its filler on in the
// engine's own pages is not started here; the block links to that page.
// While the text is edited, nothing is sent.
export function startFormView(node, view, getPos, files) {
  const canvas = h("div", { class: "pm-form-canvas pm-start-canvas" });
  const send = h("button", { type: "button", class: "primary pm-start-send", text: t("start.send") });
  const result = h("p", { class: "pm-start-result", role: "status" });
  const hint = h("p", { class: "muted pm-start-hint" });
  const actions = h("div", { class: "pm-start-actions" }, send, result, hint);
  // The button shows once the form is there: not before it is read, nor
  // when it cannot be.
  const after = h("div", { class: "pm-start-after", hidden: true }, actions);
  let form = null;
  let shownForm = null;
  const destroy = () => {
    form?.destroy();
    form = null;
  };
  // fill shows the form, filled in beforehand, ready to be sent.
  const fill = async (got, a, note) => {
    destroy();
    canvas.replaceChildren();
    result.textContent = "";
    const f = got.form;
    const Form = await loadForm();
    form = new Form({ container: canvas });
    const kinds = form.get("formFields");
    const fields = fieldsOf(f.schema);
    const filled = (a.prefill || []).filter((p) => fields.some((x) => fillable(x) && x.key === p.field));
    await form.importSchema(previewSchema(f.schema, (type) => Boolean(type && kinds.get(type)), (type) => t("form.unsupported", { type })),
      prefillData(filled, fields, await reader(filled, files?.item)));
    note.textContent = filled.length ? t("start.prefilled", { fields: filled.map((p) => `${fields.find((x) => x.key === p.field)?.label || p.field} (${sourceName(p.from)})`).join(", ") }) : "";
    hint.textContent = t("start.anonymous", { engine: got.engine.name });
    after.hidden = false;
    send.hidden = false;
    send.disabled = view.editable;
    shownForm = { got, a, note };
  };
  send.addEventListener("click", async () => {
    if (!form || view.editable) return;
    const { data, errors } = form.submit();
    if (errors && Object.keys(errors).length) return; // form-js marks the fields
    const { got, a, note } = shownForm;
    send.disabled = true;
    result.textContent = t("start.sending");
    try {
      const out = await post(linkPath(a.engine, a.link) + "/start", { variables: data, ...(a.process ? { process: a.process } : {}) });
      destroy();
      canvas.replaceChildren(h("p", { class: "pm-start-done", text: out.wizard ? t("start.wizardStarted") : t("start.sent", { name: got.form.processName || a.process || "" }) }),
        out.wizard ? h("a", { href: got.form.url, target: "_blank", rel: "noopener noreferrer", text: t("start.open") }) : "",
        h("button", { type: "button", class: "link", text: t("start.again"), onclick: () => fill(got, a, note) }));
      after.hidden = true;
      note.textContent = "";
      result.textContent = "";
    } catch (err) {
      send.disabled = false;
      result.textContent = t("start.notSent", { error: err.message });
    }
  });
  return blockView(node, view, getPos, {
    class: "pm-form pm-start",
    canvas,
    after,
    source: (a) => [a.engine, a.link, a.process || "", JSON.stringify(a.prefill || [])].join("\u0000"),
    read: async (a) => ({ ...(await get(linkPath(a.engine, a.link) + (a.process ? `?process=${enc(a.process)}` : ""))), attrs: a }),
    draw: async (got, note) => {
      if (got.form.wizard) {
        destroy();
        after.hidden = true;
        note.textContent = "";
        canvas.replaceChildren(h("p", { text: t("start.wizard") }), h("a", { href: got.form.url, target: "_blank", rel: "noopener noreferrer", text: t("start.open") }));
        return;
      }
      await fill(got, got.attrs, note);
    },
    info: ({ engine, form: f }) => t("start.info", { name: f.processName || t("start.unnamed"), engine: engine.name }),
    failed: (error) => t("start.notShown", { error }),
    tools: (ctl) => {
      // While the text is edited, the form is shown as it opens and is
      // not sent.
      send.disabled = view.editable || result.textContent === t("start.sending");
      send.title = view.editable ? t("start.inEditor") : "";
      return ctl.editable ? [h("button", { type: "button", class: "link pm-start-prefill", text: t("start.prefill"), onclick: async () => {
        const prefill = await editPrefill(ctl.attrs);
        if (prefill) ctl.set({ prefill });
      } })] : [];
    },
    destroy,
  });
}

// editPrefill lets the editor choose what a start form fills in
// beforehand: for each field a person types text into, nothing or one of
// the sources. A start form placed anew is offered what its fields' keys
// and labels hint at. It resolves to the list, in the form's order, or to
// null when closed.
export function editPrefill(a, fresh = false) {
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const rows = h("div", { class: "pm-prefill-rows" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const ok = h("button", { type: "button", class: "primary", text: t("common.save"), disabled: true });
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": t("start.prefillTitle") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("start.prefillTitle") }), close),
      h("div", { class: "bpmn-pick-body" }, h("p", { class: "muted", text: t("start.prefillHelp") }), status, rows, h("div", { class: "pm-prefill-actions" }, ok)));
    let done = false;
    let choices = [];
    const finish = (out) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(out);
    };
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    ok.addEventListener("click", () => finish(choices.filter(([, sel]) => sel.value).map(([key, sel]) => ({ field: key, from: sel.value }))));
    document.body.append(dialog);
    dialog.showModal();
    (async () => {
      try {
        const { form: f } = await get(linkPath(a.engine, a.link));
        const fields = fieldsOf(f.schema).filter(fillable);
        const current = fresh && !a.prefill?.length ? suggestPrefill(fields) : a.prefill || [];
        choices = fields.map((x) => [x.key, h("select", { "aria-label": x.label || x.key },
          [["", t("start.fromNothing")], ...prefillSources.map((s) => [s, sourceName(s)])].map(([v, label]) =>
            h("option", { value: v, text: label, selected: current.some((p) => p.field === x.key && p.from === v) })))]);
        rows.replaceChildren(...choices.map(([key, sel], i) => h("label", { class: "field" }, `${fields[i].label || key} (${key})`, sel)));
        status.textContent = fields.length ? "" : t("start.noFillable");
        ok.disabled = false;
      } catch (err) {
        status.textContent = err.message;
      }
    })();
  });
}

// pickStartLink lets the editor choose a public start link of a process
// engine: among those the engine lists to its credential, which on atlas
// needs the role Modeler, or pasted, its token or its page's address. It
// resolves to {engine, link, process, name} or to null when closed.
export function pickStartLink() {
  return new Promise((resolve) => {
    const status = h("p", { class: "muted", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { "aria-label": t("bpmn.engine"), class: "bpmn-engine" });
    const list = h("ul", { class: "bpmn-processes" });
    const paste = h("input", { type: "text", placeholder: t("start.pastePlaceholder"), "aria-label": t("start.paste"), autocomplete: "off", spellcheck: "false" });
    const use = h("button", { type: "button", text: t("start.use") });
    const pasteError = h("p", { class: "error", role: "status" });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const dialog = h("dialog", { class: "drawing bpmn-pick", "aria-label": t("start.pickTitle") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("start.pickTitle") }), close),
      h("div", { class: "bpmn-pick-body" }, h("label", { class: "field" }, t("bpmn.engine"), engines), status, list,
        h("label", { class: "field" }, t("start.paste"), h("span", { class: "pm-start-paste" }, paste, use)), pasteError));
    let done = false;
    let links = [];
    const finish = (out) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(out);
    };
    const load = async () => {
      links = [];
      list.replaceChildren();
      status.textContent = t("bpmn.loading");
      try {
        links = (await get(`/api/engines/${enc(engines.value)}/public-links`)).links;
        status.textContent = links.length ? "" : t("start.none");
      } catch (err) {
        status.textContent = t("start.notListed", { error: err.message });
      }
      list.replaceChildren(...links.map((l) => h("li", {}, h("button", { type: "button", class: "link",
        onclick: () => finish({ engine: engines.value, link: l.link, process: l.process, name: l.name || l.process }) },
      h("strong", { text: l.name || l.process }), " ", h("span", { class: "muted", text: t("start.line", { process: l.process, form: l.form }) })))));
    };
    use.addEventListener("click", () => {
      const link = linkOf(paste.value);
      if (!link) {
        pasteError.textContent = t("start.notALink");
        return;
      }
      const known = links.find((l) => l.link === link);
      finish({ engine: engines.value, link, process: known?.process || "", name: known?.name || "" });
    });
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    engines.addEventListener("change", load);
    document.body.append(dialog);
    dialog.showModal();
    (async () => {
      try {
        const { engines: all } = await get("/api/engines");
        if (!all.length) {
          status.textContent = t("bpmn.noEngines");
          engines.disabled = paste.disabled = use.disabled = true;
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
