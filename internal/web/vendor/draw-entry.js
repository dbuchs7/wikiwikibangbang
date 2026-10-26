// The parts of Excalidraw the drawing page uses (ADR-0053, ADR-0126), with
// the React it renders with and Excalidraw's stylesheet, bundled into one
// module and the chunks it loads when needed.
import "@excalidraw/excalidraw/index.css";
export { default as React } from "react";
export { createRoot } from "react-dom/client";
export { Excalidraw, exportToBlob, loadFromBlob, MIME_TYPES } from "@excalidraw/excalidraw";
