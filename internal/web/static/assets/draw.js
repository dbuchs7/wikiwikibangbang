// The drawing page (ADR-0053): Excalidraw in a frame of the UI. The UI
// talks to it with messages, from the same origin only:
//
//   page → UI  {type: "ready"}                     the page can take a drawing
//   UI → page  {type: "open", lang, scene: Blob?}  start, or edit a drawing
//   UI → page  {type: "export"}                    asks for the picture
//   page → UI  {type: "png", blob} | {type: "error", message}
//
// A drawing is a PNG that carries its scene (Excalidraw's exportEmbedScene),
// so the picture shown in a page is also the source it is edited from.
import "./draw-boot.js";
import { isDark } from "./theme.js";
import { setLanguage, t } from "./i18n.js";
import { React, createRoot, Excalidraw, exportToBlob, loadFromBlob, MIME_TYPES } from "/vendor/excalidraw.js";

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

function render(lang, initialData) {
  root.render(React.createElement(React.Fragment, null, React.createElement(Excalidraw, {
    excalidrawAPI: (a) => { api = a; },
    langCode: lang === "de" ? "de-DE" : "en",
    theme: dark ? "dark" : "light",
    initialData,
    onLibraryChange: (items) => { library = items; },
    renderTopRightUI: (isMobile) => (isMobile ? null : paletteChooser("palette")),
    onChange: (elements) => { document.body.dataset.elements = String(elements.filter((e) => !e.isDeleted).length); },
    UIOptions: { canvasActions: { loadScene: false, saveToActiveFile: false, export: false, saveAsImage: false } },
  }), paletteChooser("palette phone")));
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
  // scheme, and a dark drawing on a light page is hard to read.
  return exportToBlob({
    elements, files: api.getFiles(), mimeType: MIME_TYPES.png,
    appState: { ...api.getAppState(), exportEmbedScene: true, exportBackground: true, exportWithDarkMode: false },
  });
}

window.addEventListener("message", async (e) => {
  if (e.origin !== origin || e.source !== parent || !e.data) return;
  try {
    if (e.data.type === "open") await open(e.data);
    else if (e.data.type === "export") parent.postMessage({ type: "png", blob: await picture() }, origin);
  } catch (err) {
    parent.postMessage({ type: "error", message: err.message }, origin);
  }
});
parent.postMessage({ type: "ready" }, origin);
