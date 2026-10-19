// The UI's message catalogue (ADR-0017). English is the source language
// and the fallback; a key missing even there renders as the key itself, so
// the hole is visible. Placeholders are {name}; a plural message is an
// object of the forms Intl.PluralRules names, chosen by {n}.
import en from "./locales/en.js";
import de from "./locales/de.js";

const catalogues = { en, de };
let current = "en";

// languages are the offered languages, each with its own name.
export const languages = Object.entries(catalogues).map(([code, c]) => ({ code, name: c.$name }));

// setLanguage switches the UI language; an unknown one falls back to
// English. Dates and numbers follow it.
export function setLanguage(code) {
  current = catalogues[code] ? code : "en";
  document.documentElement.lang = current;
}

export function language() {
  return current;
}

// suggestedLanguage is the browser's language, if the UI offers it.
export function suggestedLanguage() {
  for (const tag of navigator.languages || [navigator.language || ""]) {
    const code = tag.toLowerCase().split("-")[0];
    if (catalogues[code]) return code;
  }
  return "en";
}

export function t(key, params = {}) {
  let msg = catalogues[current][key] ?? en[key];
  if (msg === undefined) return key;
  if (typeof msg === "object") msg = msg[new Intl.PluralRules(current).select(params.n ?? 0)] ?? msg.other;
  return msg.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m));
}
