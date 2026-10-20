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
import { React, createRoot, Excalidraw, exportToBlob, loadFromBlob, MIME_TYPES } from "/vendor/excalidraw.js";

const parent = window.parent;
const origin = window.location.origin;
let api = null;

// The dark scheme follows the system unless the installation turned it off
// (ADR-0070).
const dark = window.matchMedia("(prefers-color-scheme: dark)").matches &&
  await fetch("/branding.json").then((r) => r.json()).then((b) => b.dark !== false).catch(() => true);
const root = createRoot(document.getElementById("root"));

function render(lang, initialData) {
  root.render(React.createElement(Excalidraw, {
    excalidrawAPI: (a) => { api = a; },
    langCode: lang === "de" ? "de-DE" : "en",
    theme: dark ? "dark" : "light",
    initialData,
    onChange: (elements) => { document.body.dataset.elements = String(elements.filter((e) => !e.isDeleted).length); },
    UIOptions: { canvasActions: { loadScene: false, saveToActiveFile: false, export: false, saveAsImage: false } },
  }));
}

async function open({ lang, scene }) {
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
