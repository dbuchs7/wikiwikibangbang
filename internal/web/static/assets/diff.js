// Comparing two versions of a body (ADR-0043): blocks are matched by their
// content, a block changed in place is shown word by word. Everything is
// inserted as text (ADR-0014).
import { h } from "./dom.js";
import { t } from "./i18n.js";

// line is a block as one line of text, with a marker for its kind.
export function line(b) {
  const text = b.text || "";
  switch (b.kind) {
    case "heading": return "#".repeat(b.level || 1) + " " + text;
    case "bullet": return "• " + text;
    case "ordered": return "1. " + text;
    case "task": return (b.checked ? "☑ " : "☐ ") + text;
    case "quote": return "> " + text;
    case "panel": return `[${b.tone}] ` + text;
    case "table": return text.split("\n").map((r) => "| " + r.split("\t").join(" | ") + " |").join("\n");
    case "toc": return "[" + t("editor.toc") + "]";
    case "figures": return "[" + t("editor.figures") + "]";
    case "tables": return "[" + t("editor.tables") + "]";
    case "children": return "[" + t("editor.children") + "]";
    case "divider": return "———";
    case "embed": return "[" + t("editor.embed") + " " + (b.ref || "").slice(0, 8) + "]";
    default: return text;
  }
}

// lcs returns the pairs of indexes of a longest common subsequence of a
// and b under eq, or null when the table would be too large.
function lcs(a, b, eq) {
  if (a.length * b.length > 1_000_000) return null;
  const n = a.length, m = b.length;
  const len = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      len[i][j] = eq(a[i], b[j]) ? len[i + 1][j + 1] + 1 : Math.max(len[i + 1][j], len[i][j + 1]);
    }
  }
  const pairs = [];
  for (let i = 0, j = 0; i < n && j < m;) {
    if (eq(a[i], b[j])) { pairs.push([i, j]); i++; j++; } else if (len[i + 1][j] >= len[i][j + 1]) i++; else j++;
  }
  return pairs;
}

// diff lists the steps from a to b: same, del (only in a), add (only in b).
export function diff(a, b, eq = (x, y) => x === y) {
  const pairs = lcs(a, b, eq) ?? [];
  const out = [];
  let i = 0, j = 0;
  for (const [pi, pj] of [...pairs, [a.length, b.length]]) {
    while (i < pi) out.push({ op: "del", a: a[i++] });
    while (j < pj) out.push({ op: "add", b: b[j++] });
    if (pi < a.length) out.push({ op: "same", a: a[i++], b: b[j++] });
  }
  return out;
}

const split = (s) => s.split(/(\s+)/).filter(Boolean);

// words shows a changed line word by word.
function words(before, after) {
  return diff(split(before), split(after)).map((s) => s.op === "same" ? s.a : h(s.op === "del" ? "del" : "ins", { text: s.op === "del" ? s.a : s.b }));
}

// similar reports whether two lines share most of their words, so showing
// one as a change of the other helps rather than confuses.
function similar(a, b) {
  const x = a.split(/\s+/).filter(Boolean), y = b.split(/\s+/).filter(Boolean);
  if (x.length + y.length === 0) return true;
  const common = (lcs(x, y, (p, q) => p === q) ?? []).length;
  return (2 * common) / (x.length + y.length) >= 0.5;
}

// renderDiff shows the changes from the blocks before to the blocks after.
export function renderDiff(before, after) {
  const a = before.map(line), b = after.map(line);
  const steps = diff(a, b);
  if (steps.every((s) => s.op === "same")) return h("p", { class: "muted", text: t("version.noChanges") });
  const row = (op, text) => h("div", { class: "diff-row diff-" + op, text });
  const rows = [];
  for (let k = 0; k < steps.length;) {
    if (steps[k].op === "same") {
      rows.push(row("same", steps[k++].a));
      continue;
    }
    // A run of removed and added blocks: a removed block similar to an
    // added one, in order, is a block changed in place.
    const dels = [], adds = [];
    for (; k < steps.length && steps[k].op !== "same"; k++) (steps[k].op === "del" ? dels : adds).push(steps[k]);
    let next = 0;
    for (const d of dels) {
      const at = adds.findIndex((x, i) => i >= next && similar(d.a, x.b));
      if (at < 0) {
        rows.push(row("del", d.a));
        continue;
      }
      for (; next < at; next++) rows.push(row("add", adds[next].b));
      rows.push(h("div", { class: "diff-row diff-change" }, words(d.a, adds[at].b)));
      next = at + 1;
    }
    for (; next < adds.length; next++) rows.push(row("add", adds[next].b));
  }
  return h("div", { class: "diff", role: "region", "aria-label": t("version.changes") }, rows);
}
