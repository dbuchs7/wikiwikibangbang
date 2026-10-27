// Architecture sketches of releases (ADR-0146): what the block and its
// picker need and nothing that needs a browser, so it is tested on its own.

// LAYERS are the layers a sketch may show, in the order the engine draws
// them from top to bottom; a block names its layers in this order.
// DEFAULT_LAYERS are those the engine draws when a block names none.
export const LAYERS = ["strategy", "business", "application", "data", "technology"];
export const DEFAULT_LAYERS = ["application", "technology"];

// canonical returns the known layers among layers, each once, in the
// engine's order.
export function canonical(layers) {
  return LAYERS.filter((l) => (layers || []).includes(l));
}

// checkedLayers says which of the layers a release records the picker
// checks: those a block names, or, when it names none, the engine's
// default ones; and when the release records neither of those, every
// layer it records, so that a sketch is never asked for nothing.
export function checkedLayers(named, recorded) {
  const own = canonical(recorded);
  if (named?.length) return own.filter((l) => named.includes(l));
  const chosen = own.filter((l) => DEFAULT_LAYERS.includes(l));
  return chosen.length ? chosen : own;
}

// sketchPath is the address of a sketch on this server: the release's
// version, or newest for release 0, and the layers, none for the engine's
// default.
export function sketchPath(engine, application, release, layers) {
  const enc = encodeURIComponent;
  const shown = canonical(layers);
  return `/api/engines/${enc(engine)}/architectures/${enc(application)}/${release > 0 ? release : "newest"}` +
    (shown.length ? `?layers=${shown.join(",")}` : "");
}

// resolve finds what a block shows in the engine's list of applications:
// the application and the release shown now, the newest for release 0.
// Either is null when the list does not offer it.
export function resolve(applications, application, release) {
  const app = (applications || []).find((a) => a.application === application) || null;
  const releases = app?.releases || [];
  const rel = (release > 0 ? releases.find((r) => r.version === release) : releases[0]) || null;
  return { app, rel };
}
