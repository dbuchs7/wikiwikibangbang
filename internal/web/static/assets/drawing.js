// Drawings (ADR-0053): the drawing editor runs on a page of its own, in a
// frame of a dialog, as Excalidraw needs a looser content security policy
// than the UI. A drawing is stored as a PNG attachment of its item that
// carries its scene; its name ends in DRAWING, which marks it for editing.
import { h } from "./dom.js";
import { t, language } from "./i18n.js";

export const DRAWING = ".drawing.png";

export const isDrawing = (name) => typeof name === "string" && name.endsWith(DRAWING);

// drawingName makes an attachment name for a drawing from its caption.
export function drawingName(caption) {
  const base = (caption || "").replace(/[\\/\r\n]+/g, " ").trim().slice(0, 80) || t("draw.untitled");
  return base + DRAWING;
}

// openDrawing shows the drawing editor, with scene (a drawing's PNG) to
// edit it or without to start one. store(png) keeps the new PNG, e.g. as
// an attachment; the dialog stays open, saving, until it has, and shows
// what went wrong if it fails. It resolves to what store returns, or to
// null when the dialog is closed without saving. process(want, current),
// if given, answers the drawing's asks for a process (ADR-0142) with the
// picture to place, or null.
export function openDrawing(scene = null, store = async (png) => png, process = null) {
  return new Promise((resolve) => {
    const origin = window.location.origin;
    const frame = h("iframe", { src: "/draw", title: t("draw.title"), class: "drawing-frame" });
    const status = h("span", { class: "error", role: "status" });
    const save = h("button", { type: "button", class: "primary", text: t("draw.save"), disabled: true });
    const cancel = h("button", { type: "button", text: t("draw.cancel") });
    const dialog = h("dialog", { class: "drawing", "aria-label": t("draw.title") },
      h("div", { class: "drawing-bar" }, h("strong", { text: t("draw.title") }), status, cancel, save), frame);
    let done = false;
    // Going back, as a phone's back button does, leaves the drawing too.
    const leave = () => finish(null);
    const finish = (result) => {
      if (done) return;
      done = true;
      window.removeEventListener("message", onMessage);
      window.removeEventListener("hashchange", leave);
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    const onMessage = (e) => {
      if (e.origin !== origin || e.source !== frame.contentWindow || !e.data) return;
      if (e.data.type === "ready") {
        frame.contentWindow.postMessage({ type: "open", lang: language(), scene }, origin);
        save.disabled = false;
      } else if (e.data.type === "png") {
        status.classList.remove("error");
        status.textContent = t("draw.saving");
        cancel.disabled = true;
        store(e.data.blob).then(finish, (err) => {
          status.classList.add("error");
          status.textContent = err.message;
          save.disabled = cancel.disabled = false;
        });
      } else if (e.data.type === "process") {
        // The drawing waits for an answer, null when there is none.
        const answer = (picture) => frame.contentWindow?.postMessage({ type: "process", picture }, origin);
        status.classList.remove("error");
        status.textContent = "";
        if (!process) return answer(null);
        process(e.data.want, e.data.current).then(answer, (err) => {
          answer(null);
          status.classList.add("error");
          status.textContent = err.message;
        });
      } else if (e.data.type === "error") {
        save.disabled = false;
        status.classList.add("error");
        status.textContent = e.data.message === "empty" ? t("draw.empty") : e.data.message;
      }
    };
    window.addEventListener("message", onMessage);
    save.addEventListener("click", () => {
      status.textContent = "";
      save.disabled = true;
      frame.contentWindow.postMessage({ type: "export" }, origin);
    });
    cancel.addEventListener("click", () => finish(null));
    window.addEventListener("hashchange", leave);
    // Escape belongs to the drawing (it ends a tool); the dialog closes by
    // its buttons only.
    dialog.addEventListener("cancel", (e) => e.preventDefault());
    document.body.append(dialog);
    dialog.showModal();
  });
}
