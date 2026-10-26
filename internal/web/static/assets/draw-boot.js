// Excalidraw loads its fonts from this path, not from a CDN. It reads it
// when its module is evaluated, so draw.html loads this as a classic script
// before any module (ADR-0053, ADR-0126).
window.EXCALIDRAW_ASSET_PATH = "/vendor/";
