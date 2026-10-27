// Architecture sketches of releases in a text (ADR-0146): the sketch of a
// release of an application a process engine keeps (block kind
// architecture), the newest release with a sketch or a fixed one, with the
// layers chosen. The server reads it from the engine whenever it is shown,
// as it reads a process (ADR-0129), and answers it as a picture of this
// site, which the block shows as an image: the UI's policy stays as it is,
// and the picture runs nothing.
import { h, when } from "./dom.js";
import { get } from "./api.js";
import { t } from "./i18n.js";
import { canonical, checkedLayers, resolve, sketchPath } from "./archparts.js";

const enc = encodeURIComponent;
const listPath = (engine) => `/api/engines/${enc(engine)}/architectures`;

// layerName names a layer in the UI language.
function layerName(layer) {
  switch (layer) {
    case "strategy": return t("arch.layer.strategy");
    case "business": return t("arch.layer.business");
    case "application": return t("arch.layer.application");
    case "data": return t("arch.layer.data");
    case "technology": return t("arch.layer.technology");
  }
  return layer;
}

// refusal reads src for the server's words on why it answers no picture,
// or "" when it answers one: an image says only that it failed.
async function refusal(src) {
  try {
    const res = await fetch(src, { credentials: "same-origin" });
    if (res.ok) return "";
    let message = res.statusText;
    try { message = (await res.json()).error || message; } catch { /* not JSON */ }
    return message;
  } catch (err) {
    return err.message;
  }
}

// showImage shows the picture at src in img and resolves once it is
// drawn, or rejects with the server's words. A picture already drawn from
// src is kept when reuse is set: a release's sketch never changes.
async function showImage(img, src, reuse) {
  if (reuse && img.getAttribute("src") === src && img.complete && img.naturalWidth) return;
  const drawn = await new Promise((resolve) => {
    const settle = (ok) => () => {
      img.removeEventListener("load", loaded);
      img.removeEventListener("error", failed);
      resolve(ok);
    };
    const loaded = settle(true);
    const failed = settle(false);
    img.addEventListener("load", loaded);
    img.addEventListener("error", failed);
    img.setAttribute("src", src);
  });
  if (!drawn) throw new Error(await refusal(src) || t("arch.noPicture"));
}

// architectureView shows an architecture block, in reading and in editing,
// as a process block is shown (bpmn.js): a head naming the application,
// the release and when its sketch was taken, the sketch scaled to the text
// column, which opens on its own in a new tab, and the caption. An editor
// changes the sketch in the picker, changes the caption and removes the
// block.
export function architectureView(node, view, getPos) {
  const img = h("img", { class: "pm-arch-img", alt: "" });
  const open = h("a", { class: "pm-arch-open", target: "_blank", rel: "noopener", title: t("arch.open") }, img);
  const canvas = h("div", { class: "pm-arch-canvas", hidden: true }, open);
  const info = h("span", { class: "pm-diagram-info" });
  const tools = h("span", { class: "pm-diagram-tools" });
  const error = h("p", { class: "error pm-diagram-error", role: "status" });
  const caption = h("figcaption");
  const dom = h("figure", { class: "pm-diagram pm-arch", contenteditable: "false" },
    h("div", { class: "pm-diagram-head" }, info, tools), canvas, error, caption);
  let drawn = null;
  let caught = false;

  const source = () => JSON.stringify([node.attrs.engine, node.attrs.application, node.attrs.release, node.attrs.layers]);
  // draw reads the engine's list for what the head says and shows the
  // sketch, once per source, or again when again is set (a reload). A
  // release the list offers is shown at its version, which never changes,
  // so that the head and the picture name the same release.
  const draw = async (again = false) => {
    const key = source();
    if (drawn === key && !again) return;
    drawn = key;
    error.textContent = "";
    const a = node.attrs;
    try {
      const { engine, applications } = await get(listPath(a.engine));
      const { app, rel } = resolve(applications, a.application, a.release);
      const src = sketchPath(a.engine, a.application, rel ? rel.version : a.release, a.layers);
      // What the list does not offer is asked for as the block names it,
      // so that the server says why it is not there; the browser could
      // otherwise show a picture it drew before from the same address.
      const why = rel ? "" : await refusal(src);
      if (why) throw new Error(why);
      if (drawn !== key) return;
      await showImage(img, src, Boolean(rel));
      if (drawn !== key) return;
      const name = app?.name || a.application;
      open.setAttribute("href", src);
      img.setAttribute("alt", t("arch.alt", { name }));
      info.textContent = !rel ? name
        : a.release > 0 ? t("arch.infoFixed", { name, version: rel.version, taken: when(rel.takenAt), engine: engine.name })
          : t("arch.infoNewest", { name, version: rel.version, taken: when(rel.takenAt), engine: engine.name });
      canvas.hidden = false;
      dom.classList.add("pm-diagram-drawn");
    } catch (err) {
      if (drawn === key) {
        drawn = null;
        canvas.hidden = true;
        info.textContent = "";
        error.textContent = t("arch.notShown", { error: err.message });
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
  // change reopens the picker with the block's values.
  const change = async () => {
    error.textContent = "";
    const p = await pickArchitecture(node.attrs);
    if (!p) return;
    try {
      setAttrs(p);
    } catch (err) {
      error.textContent = err.message;
    }
  };

  let shown = null;
  const render = () => {
    const key = [source(), node.attrs.text, view.editable, drawn].join("\u0000");
    if (shown === key) return;
    shown = key;
    tools.replaceChildren(
      view.editable ? h("button", { type: "button", class: "link pm-arch-edit", text: t("arch.edit"), onclick: change }) : "",
      h("button", { type: "button", class: "link pm-diagram-refresh", text: t("bpmn.refresh"), onclick: () => draw(true) }));
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

  // A sketch is read when it first comes into sight: a long text need not
  // read them all.
  const sight = new IntersectionObserver((entries) => {
    if (entries.some((e) => e.isIntersecting)) {
      sight.disconnect();
      caught = true;
      draw();
    }
  });
  sight.observe(dom);
  render();

  // The picture keeps its clicks, which open it, and the head and caption
  // theirs.
  const stopEvent = (ev) => canvas.contains(ev.target) || caption.contains(ev.target) || tools.contains(ev.target);
  return {
    dom, stopEvent, ignoreMutation: () => true,
    update: (n) => {
      if (n.type !== node.type) return false;
      node = n;
      if (caught && drawn !== source()) draw();
      render();
      return true;
    },
    destroy: () => sight.disconnect(),
  };
}

// pickArchitecture lets the editor choose a sketch: the engine, then the
// application, then the newest release, which the block follows, or a
// fixed one, then the layers that release records, then the caption, which
// is the application's name until it is changed. current holds a block's
// values to change them. It resolves to {engine, application, release,
// layers, text} or to null when closed.
export function pickArchitecture(current = null) {
  return new Promise((resolveChoice) => {
    const title = t("arch.pickTitle");
    const status = h("p", { class: "muted arch-status", role: "status", text: t("bpmn.loading") });
    const engines = h("select", { class: "bpmn-engine arch-engine", "aria-label": t("bpmn.engine") });
    const apps = h("select", { class: "arch-application", "aria-label": t("arch.application"), disabled: true });
    const releases = h("select", { class: "arch-release", "aria-label": t("arch.release"), disabled: true });
    const layers = h("div", { class: "arch-layer-list" });
    const caption = h("input", { class: "arch-caption", value: current?.text || "", "aria-label": t("editor.caption") });
    const close = h("button", { type: "button", text: t("common.cancel") });
    const apply = h("button", { type: "button", class: "primary arch-apply", text: current ? t("common.save") : t("arch.place"), disabled: true });
    const dialog = h("dialog", { class: "drawing bpmn-pick bpmn-settings arch-pick", "aria-label": title },
      h("div", { class: "drawing-bar" }, h("strong", { text: title }), close, apply),
      h("div", { class: "bpmn-pick-body" },
        h("label", { class: "field" }, t("bpmn.engine"), engines),
        h("label", { class: "field" }, t("arch.application"), apps),
        h("label", { class: "field" }, t("arch.release"), releases),
        h("fieldset", { class: "arch-layers" }, h("legend", { text: t("arch.layers") }), layers),
        h("label", { class: "field" }, t("editor.caption"), caption),
        status));
    let done = false;
    let list = [];
    // chosen is what the person checked last, or the block's layers;
    // named says whether the caption is the person's own words.
    let chosen = current?.layers || [];
    let named = Boolean(current?.text);
    let first = current;
    const finish = (result) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolveChoice(result);
    };
    const app = () => list.find((a) => a.application === apps.value) || null;
    const checked = () => [...layers.querySelectorAll("input:checked")].map((box) => box.value);
    // fillLayers offers the layers the chosen release records.
    const fillLayers = () => {
      const { rel } = resolve(list, apps.value, Number(releases.value));
      const on = checkedLayers(chosen, rel?.recorded);
      layers.replaceChildren(...canonical(rel?.recorded).map((l) => h("label", { class: "check" },
        h("input", { type: "checkbox", value: l, checked: on.includes(l), onchange: () => { chosen = checked(); } }), " ", layerName(l))));
      apply.disabled = !rel;
    };
    // fillReleases offers the newest release, which the block follows, and
    // each release with a sketch, by version and when it was taken.
    const fillReleases = (keep) => {
      const a = app();
      const all = a?.releases || [];
      releases.replaceChildren(...(all.length ? [h("option", { value: "0", text: t("arch.newest", { version: all[0].version, taken: when(all[0].takenAt) }) })] : []),
        ...all.map((r) => h("option", { value: String(r.version), text: t("arch.version", { version: r.version, taken: when(r.takenAt) }) })));
      releases.disabled = !all.length;
      releases.value = keep && all.some((r) => r.version === keep) ? String(keep) : "0";
      if (!named) caption.value = a?.name || "";
      fillLayers();
    };
    const load = async () => {
      list = [];
      apps.replaceChildren();
      releases.replaceChildren();
      layers.replaceChildren();
      apps.disabled = releases.disabled = apply.disabled = true;
      status.textContent = t("bpmn.loading");
      try {
        list = (await get(listPath(engines.value))).applications;
        status.textContent = list.length ? "" : t("arch.none");
      } catch (err) {
        status.textContent = err.message;
      }
      apps.replaceChildren(...list.map((a) => h("option", { value: a.application, text: a.name || a.application })));
      apps.disabled = !list.length;
      const keep = first && first.engine === engines.value && list.some((a) => a.application === first.application) ? first : null;
      if (keep) apps.value = keep.application;
      first = null;
      fillReleases(keep?.release);
    };
    caption.addEventListener("input", () => { named = caption.value.trim() !== ""; });
    apps.addEventListener("change", () => fillReleases(0));
    releases.addEventListener("change", fillLayers);
    engines.addEventListener("change", load);
    apply.addEventListener("click", () => {
      const shown = canonical(checked());
      if (!shown.length) {
        status.textContent = t("arch.noLayers");
        return;
      }
      finish({ engine: engines.value, application: apps.value, release: Number(releases.value), layers: shown, text: caption.value.replace(/[\r\n]+/g, " ").trim() });
    });
    close.addEventListener("click", () => finish(null));
    dialog.addEventListener("close", () => finish(null));
    document.body.append(dialog);
    dialog.showModal();
    (async () => {
      try {
        const { engines: all } = await get("/api/engines");
        if (!all.length) {
          status.textContent = t("bpmn.noEngines");
          engines.disabled = true;
          return;
        }
        engines.replaceChildren(...all.map((e) => h("option", { value: e.id, text: e.name, selected: e.id === current?.engine })));
        await load();
      } catch (err) {
        status.textContent = err.message;
      }
    })();
  });
}
