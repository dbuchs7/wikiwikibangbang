// The DMN viewer (ADR-0144): dmn-js's navigated viewer, which shows the
// requirements graph, decision tables and literal expressions of a DMN
// model but does not change them, bundled into one module that is loaded
// only when a decision is shown.
export { default as Viewer } from "dmn-js/lib/NavigatedViewer";
