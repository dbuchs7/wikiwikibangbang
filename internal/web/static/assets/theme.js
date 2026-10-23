// The light or dark look (ADR-0095). It follows the system until someone
// chooses one with the bulb in the header; the choice is kept in this
// browser. A forced look rewrites every rule that asks for the system's
// scheme: the dark stylesheet's link, the logo's picture source and the
// @media rules of the design's own stylesheet (ADR-0070), so a theme
// with a dark look of its own follows the choice too.

const key = "wwbb.theme";
const system = window.matchMedia("(prefers-color-scheme: dark)");
// original holds each rewritten rule's or element's media as written.
const original = new WeakMap();

// chosenTheme is "dark", "light", or "" to follow the system.
export function chosenTheme() {
  try {
    const v = localStorage.getItem(key);
    return v === "dark" || v === "light" ? v : "";
  } catch {
    return "";
  }
}

// isDark reports whether the dark look is wanted, chosen or by the system.
export function isDark() {
  const c = chosenTheme();
  return c ? c === "dark" : system.matches;
}

// setTheme keeps a choice ("" follows the system again) and applies it.
export function setTheme(v) {
  try {
    if (v) localStorage.setItem(key, v);
    else localStorage.removeItem(key);
  } catch { /* the choice lasts for this page only */ }
  applyTheme(v);
}

// forced rewrites a media query list for a forced look: a query asking for
// the other scheme matches nothing, one asking for this scheme loses only
// that condition.
export function forced(media, dark) {
  return media.split(",").map((q) => {
    q = q.trim();
    const m = q.match(/\(\s*prefers-color-scheme\s*:\s*(dark|light)\s*\)/i);
    if (!m) return q;
    if ((m[1].toLowerCase() === "dark") !== dark) return "not all";
    const rest = q.replace(m[0], "").replace(/\s+/g, " ").trim()
      .replace(/^and\s+/i, "").replace(/\s+and$/i, "").replace(/\band and\b/i, "and");
    return rest || "all";
  }).join(", ");
}

function rewrite(get, set, holder, choice) {
  if (!original.has(holder)) {
    const m = get();
    if (!/prefers-color-scheme/i.test(m)) return;
    original.set(holder, m);
  }
  const m = original.get(holder);
  set(choice ? forced(m, choice === "dark") : m);
}

function rewriteRules(rules, choice) {
  for (const r of rules) {
    if (r.media && r.cssRules) rewrite(() => r.media.mediaText, (v) => { r.media.mediaText = v; }, r, choice);
    if (r.cssRules) rewriteRules(r.cssRules, choice);
  }
}

// applyTheme puts the choice into effect wherever the page asks for the
// system's scheme.
export function applyTheme(choice = chosenTheme()) {
  document.documentElement.dataset.theme = isDark() ? "dark" : "light";
  for (const el of document.querySelectorAll("link[media], source[media]")) {
    // theme-boot.js forced the dark stylesheet's link already, keeping
    // its media as written.
    rewrite(() => el.dataset.media || el.media, (v) => { el.media = v; }, el, choice);
  }
  for (const sheet of document.styleSheets) {
    try {
      rewriteRules(sheet.cssRules, choice);
    } catch { /* a sheet of another origin cannot be read; none is linked */ }
  }
  document.dispatchEvent(new CustomEvent("themechange"));
}

// A stylesheet that loads again, e.g. after the design changed, brings its
// rules as written.
document.addEventListener("load", (ev) => {
  if (ev.target instanceof HTMLLinkElement && ev.target.rel === "stylesheet") applyTheme();
}, true);
system.addEventListener("change", () => applyTheme());
