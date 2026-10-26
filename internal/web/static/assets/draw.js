// The drawing page (ADR-0053): Excalidraw in a frame of the UI. The UI
// talks to it with messages, from the same origin only:
//
//   page → UI  {type: "ready"}                     the page can take a drawing
//   UI → page  {type: "open", lang, scene: Blob?}  start, or edit a drawing
//   UI → page  {type: "export"}                    asks for the picture
//   page → UI  {type: "png", blob} | {type: "error", message}
//   page → UI  {type: "process", want, current}    asks for a process (ADR-0142)
//   UI → page  {type: "process", picture}          {svg, width, height, data}, or null
//
// A drawing is a PNG that carries its scene (Excalidraw's exportEmbedScene),
// so the picture shown in a page is also the source it is edited from.
import { isDark } from "./theme.js";
import { setLanguage, t } from "./i18n.js";
import { KEY, PADDING, processOf, regionsOf, rootsOf, withRegions } from "./drawprocesses.js";
import {
  React, createRoot, Excalidraw, exportToBlob, loadFromBlob, MIME_TYPES,
  convertToExcalidrawElements, getCommonBounds, newElementWith, CaptureUpdateAction,
} from "/vendor/excalidraw.js";

const parent = window.parent;
const origin = window.location.origin;
let api = null;

// The dark scheme follows the system or the look chosen in the header
// (ADR-0095), unless the installation turned it off (ADR-0070).
const dark = isDark() &&
  await fetch("/branding.json").then((r) => r.json()).then((b) => b.dark !== false).catch(() => true);
document.body.dataset.theme = dark ? "dark" : "light";
const root = createRoot(document.getElementById("root"));

// The palettes (ADR-0117) are libraries of shapes, loaded when chosen. The
// one chosen replaces the one before in the library; what the user added
// to it themselves stays.
const palettes = () => [
  ["archimate", t("draw.paletteArchimate")],
  ["bpmn", t("draw.paletteBpmn")],
  ["network", t("draw.paletteNetwork")],
  ["it", t("draw.paletteIt")],
  ["microsoft", t("draw.paletteMicrosoft")],
];
let library = [];

async function usePalette(name) {
  const res = await fetch(`/assets/palettes/${name}.json`);
  if (!res.ok) throw new Error(res.statusText);
  const { libraryItems } = await res.json();
  const own = library.filter((item) => item.status !== "published");
  await api.updateLibrary({ libraryItems: [...own, ...libraryItems], merge: false, openLibraryMenu: true });
  document.body.dataset.palette = name;
}

// On a wide screen it sits in the top row, left of the library button; on a
// phone that row is full, and it floats above the bottom bar (draw.css).
function paletteChooser(className) {
  const { createElement: h } = React;
  return h("select", {
    className, "aria-label": t("draw.palette"), defaultValue: "",
    onChange: (e) => { if (e.target.value) usePalette(e.target.value).catch((err) => parent.postMessage({ type: "error", message: err.message }, origin)); },
  }, h("option", { value: "", disabled: true }, t("draw.palette")), ...palettes().map(([value, label]) => h("option", { key: value, value }, label)));
}

// A process in a drawing (ADR-0142) is a picture of a BPMN model whose
// customData says what it shows: a model kept in the drawing, drawn anew
// in the modeler, or a process of a process engine, which a page shows in
// its newest version over the picture. The UI makes the picture: the
// modeler and the engines are its, not this page's.
const processChoices = () => [
  ["new", t("draw.processNew")],
  ["file", t("draw.processFile")],
  ["process", t("draw.processEngine")],
  ["edit", t("draw.processEdit")],
];

// pending is the element the UI's answer replaces, or "" to add one.
let pending = null;

// selectedProcess is the one selected element that shows a process.
function selectedProcess() {
  const ids = Object.keys(api.getAppState().selectedElementIds || {});
  const el = ids.length === 1 ? api.getSceneElements().find((e) => e.id === ids[0]) : null;
  return processOf(el) ? el : null;
}

function askProcess(want) {
  let current = null;
  pending = "";
  if (want === "edit") {
    const el = selectedProcess();
    if (!el) return parent.postMessage({ type: "error", message: t("draw.processSelect") }, origin);
    pending = el.id;
    current = processOf(el);
  }
  parent.postMessage({ type: "process", want, current }, origin);
}

// place adds the picture the UI made, in the middle of what is in sight, or
// puts it in place of the one edited, as wide as that was.
function place({ svg, width, height, data }) {
  const target = pending;
  pending = null;
  if (target === null || !processOf({ type: "image", customData: { [KEY]: data } })) return;
  const fileId = crypto.randomUUID();
  api.addFiles([{ id: fileId, dataURL: svg, mimeType: MIME_TYPES.svg, created: Date.now() }]);
  const all = api.getSceneElementsIncludingDeleted();
  const old = target && all.find((e) => e.id === target && !e.isDeleted);
  if (old) {
    const elements = all.map((e) => (e === old ? newElementWith(e, { fileId, height: (e.width * height) / width, customData: { ...e.customData, [KEY]: data } }) : e));
    api.updateScene({ elements, captureUpdate: CaptureUpdateAction.IMMEDIATELY });
    return;
  }
  const s = api.getAppState();
  const w = Math.min(width, 640);
  const hh = (w * height) / width;
  const cx = s.width / 2 / s.zoom.value - s.scrollX;
  const cy = s.height / 2 / s.zoom.value - s.scrollY;
  const [el] = convertToExcalidrawElements([{ type: "image", fileId, status: "saved", x: cx - w / 2, y: cy - hh / 2, width: w, height: hh, customData: { [KEY]: data } }]);
  api.updateScene({ elements: [...all, el], appState: { selectedElementIds: { [el.id]: true } }, captureUpdate: CaptureUpdateAction.IMMEDIATELY });
}

function processChooser(className) {
  const { createElement: h } = React;
  return h("select", {
    className, "aria-label": t("draw.process"), defaultValue: "",
    onChange: (e) => { const want = e.target.value; e.target.value = ""; if (want) askProcess(want); },
  }, h("option", { value: "", disabled: true }, t("draw.process")), ...processChoices().map(([value, label]) => h("option", { key: value, value }, label)));
}

function render(lang, initialData) {
  const { createElement: h, Fragment } = React;
  root.render(h(Fragment, null, h(Excalidraw, {
    excalidrawAPI: (a) => { api = a; },
    langCode: lang === "de" ? "de-DE" : "en",
    theme: dark ? "dark" : "light",
    initialData,
    onLibraryChange: (items) => { library = items; },
    renderTopRightUI: (isMobile) => (isMobile ? null : h(Fragment, null, processChooser("process"), paletteChooser("palette"))),
    onChange: (elements) => { document.body.dataset.elements = String(elements.filter((e) => !e.isDeleted).length); },
    UIOptions: { canvasActions: { loadScene: false, saveToActiveFile: false, export: false, saveAsImage: false } },
  }), h("div", { className: "phone-tools" }, processChooser("process"), paletteChooser("palette"))));
}

async function open({ lang, scene }) {
  setLanguage(lang);
  let initialData = { appState: { viewBackgroundColor: "#ffffff" } };
  if (scene) {
    const data = await loadFromBlob(scene, null, null);
    initialData = { elements: data.elements, files: data.files, appState: { viewBackgroundColor: data.appState.viewBackgroundColor || "#ffffff" } };
  }
  render(lang, initialData);
  document.body.dataset.state = "open";
}

async function picture() {
  const elements = api.getSceneElements();
  if (!elements.length) throw new Error("empty");
  // The picture is always light: it is shown in pages whatever the reader's
  // scheme, and a dark drawing on a light page is hard to read. Its size is
  // Excalidraw's own; it is kept to measure the processes in it by.
  let size = null;
  const blob = await exportToBlob({
    elements, files: api.getFiles(), mimeType: MIME_TYPES.png, exportPadding: PADDING,
    getDimensions: (width, height) => (size = { width, height, scale: 1 }),
    appState: { ...api.getAppState(), exportEmbedScene: true, exportBackground: true, exportWithDarkMode: false },
  });
  const regions = size ? regionsOf(elements, getCommonBounds(rootsOf(elements)), size) : [];
  return regions.length ? new Blob([withRegions(new Uint8Array(await blob.arrayBuffer()), regions)], { type: MIME_TYPES.png }) : blob;
}

window.addEventListener("message", async (e) => {
  if (e.origin !== origin || e.source !== parent || !e.data) return;
  try {
    if (e.data.type === "open") await open(e.data);
    else if (e.data.type === "export") parent.postMessage({ type: "png", blob: await picture() }, origin);
    else if (e.data.type === "process") {
      if (e.data.picture) place(e.data.picture);
      else pending = null;
    }
  } catch (err) {
    parent.postMessage({ type: "error", message: err.message }, origin);
  }
});
parent.postMessage({ type: "ready" }, origin);
