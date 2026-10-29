// The UI shell (ADR-0009, ADR-0031): hash routes, one view per route, all
// data through the HTTP API. Plain ES modules, no build step.
import { api, get, post, put, del, download, upload, ApiError } from "./api.js";
import { h, mount, when } from "./dom.js";
import { createEditor, setSecretClass } from "./editor.js";
import { liveBody, watchLive } from "./live.js";
import { pageTree, ancestors } from "./tree.js";
import { renderDiff } from "./diff.js";
import { commentPlugin } from "./anchors.js";
import { personalNotes } from "./notes.js";
import { isDrawing } from "./drawing.js";
import { albumBrowser, retentionCard } from "./album.js";
import { t, setLanguage, suggestedLanguage, languages, language } from "./i18n.js";
import { applyTheme, setTheme, isDark } from "./theme.js";
import { filtered, serverFilter, filterParams, norm } from "./filter.js";
import { taskLinksCard } from "./tasklinks.js";
import { connectionsCard } from "./engineconn.js";
import { taskDialog, taskPanel } from "./taskdialog.js";
import { finishing } from "./taskparts.js";

const app = document.getElementById("app");
const nav = document.getElementById("nav");
let me = null; // the signed-in principal, from GET /api/me

// --- shared pieces -------------------------------------------------------

// dressFilePicker shows a file field as a button and the chosen names in
// the page's language (ADR-0091); the browser's own button speaks the
// browser's. The field itself stays, unseen but focusable, inside its label.
function dressFilePicker(input) {
  if (input.dataset.dressed || input.hidden) return;
  input.dataset.dressed = "1";
  const none = () => t("file.none");
  const names = h("span", { class: "file-names", text: none() });
  const label = h("label", { class: "file-picker" });
  input.replaceWith(label);
  label.append(input, h("span", { class: "button secondary file-button", text: input.multiple ? t("file.chooseMany") : t("file.choose") }), names);
  input.addEventListener("change", () => { names.textContent = input.files.length ? [...input.files].map((f) => f.name).join(", ") : none(); });
}
new MutationObserver(() => document.querySelectorAll("input[type=file]:not([data-dressed]):not([hidden])").forEach(dressFilePicker))
  .observe(document.body, { childList: true, subtree: true });

// empty says that a list or section has nothing to show, the same way
// everywhere (ADR-0091).
function empty(text, id) {
  return h("p", { class: "empty", id, text });
}

function errorBox(err) {
  return h("p", { class: "error", role: "alert", text: err instanceof Error ? err.message : String(err) });
}

// form wires a submit handler that shows errors next to the form and keeps
// the button from being pressed twice.
function form(attrs, children, onSubmit) {
  const status = h("div", { class: "status" });
  const f = h("form", attrs, children, status);
  f.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const button = f.querySelector("button[type=submit]");
    if (button) button.disabled = true;
    status.replaceChildren();
    try {
      await onSubmit(new FormData(f), status);
    } catch (err) {
      status.replaceChildren(errorBox(err));
    } finally {
      if (button) button.disabled = false;
    }
  });
  return f;
}

function field(label, input) {
  return h("label", { class: "field" }, h("span", { text: label }), input);
}

// link is an anchor; cls styles it, e.g. "button" for an action that
// navigates.
function link(href, text, cls, id) {
  return h("a", { href, text, class: cls || null, id: id || null });
}

// --- export (ADR-0071) ---------------------------------------------------------

// csvLink downloads a list the server writes as CSV.
function csvLink(href, id) {
  return h("a", { href, class: "button secondary csv-link", id: id || null, download: "", text: t("export.csv") });
}

// downloadCSV saves rows the page already holds as CSV, as the server
// writes it: UTF-8 with a byte order mark, a semicolon for German, and no
// cell a spreadsheet would run as a formula.
function downloadCSV(name, rows) {
  const sep = language() === "de" ? ";" : ",";
  const cell = (v) => {
    let s = String(v ?? "");
    if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
    return /["\r\n]/.test(s) || s.includes(sep) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  const text = "\ufeff" + rows.map((r) => r.map(cell).join(sep)).join("\r\n") + "\r\n";
  // A blob address is set directly: h() admits only addresses of pages.
  const a = h("a", { download: name });
  a.href = URL.createObjectURL(new Blob([text], { type: "text/csv;charset=utf-8" }));
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

// printExport prints a page, or a handbook of it and its subpages, in the
// printable form the server writes (ADR-0071): a frame loads it and the
// browser's print dialog saves it as PDF.
function printExport(path, children) {
  document.getElementById("export-frame")?.remove();
  // The address is set before the frame is placed: a frame placed without
  // one loads an empty page first, which was printed as an empty PDF.
  const frame = h("iframe", { id: "export-frame", title: t("export.pdf"), class: "export-frame",
    src: path + "/export/html" + (children ? "?children=true" : "") });
  frame.addEventListener("load", () => {
    if (frame.contentWindow.location.href !== "about:blank") frame.contentWindow.print();
  });
  document.body.append(frame);
}

// exportMenu offers a page as PDF, as a Word document and as Markdown in a
// ZIP archive with its pictures (ADR-0131), all with what it embeds, and
// each of its tables as CSV; a page with subpages also as a handbook with
// them.
function exportMenu(item) {
  const path = "/api/items/" + encodeURIComponent(item.id);
  const tables = (item.body || []).filter((b) => b.kind === "table");
  const close = (ev) => { ev.target.closest("details").open = false; };
  const handbook = h("div", { class: "export-handbook" });
  const menu = h("details", { class: "export-menu", id: "item-export" },
    h("summary", { class: "button", text: t("export.open") }),
    h("div", { class: "export-list" },
      h("button", { type: "button", class: "link", id: "export-pdf", text: t("export.pdf"), onclick: (ev) => { close(ev); printExport(path, false); } }),
      h("a", { href: path + "/export/docx", id: "export-docx", download: "", text: t("export.docx") }),
      h("a", { href: path + "/export/md.zip", id: "export-md", download: "", text: t("export.markdown") }),
      handbook,
      tables.map((b, i) => h("a", { href: `${path}/tables/${i + 1}`, class: "export-table", download: "",
        text: t("export.table", { n: i + 1, head: (b.text || "").split("\n")[0].split("\t").slice(0, 3).join(", ") }) }))));
  // Whether there are subpages is asked when the menu opens.
  // A download leaves the page where it is; the menu closes with it.
  menu.addEventListener("click", (ev) => { if (ev.target.closest("a[download]")) setTimeout(() => { menu.open = false; }); });
  menu.addEventListener("toggle", async () => {
    if (!menu.open || handbook.dataset.asked) return;
    handbook.dataset.asked = "1";
    const kids = await childPagesOf(item)().catch(() => []);
    if (kids.length) handbook.append(
      h("button", { type: "button", class: "link", id: "export-pdf-handbook", text: t("export.pdfHandbook"), onclick: (ev) => { close(ev); printExport(path, true); } }),
      h("a", { href: path + "/export/docx?children=true", id: "export-docx-handbook", download: "", text: t("export.docxHandbook") }),
      h("a", { href: path + "/export/md.zip?children=true", id: "export-md-handbook", download: "", text: t("export.markdownHandbook") }));
  });
  return menu;
}

// --- watching (ADR-0046) --------------------------------------------------------

// watchButton starts or stops watching; path is the item's or space's.
// It is a bell: filled while one watches, outlined while one does not; its
// name and tooltip say what a press does.
function watchButton(path, watching, status) {
  const label = watching ? t("watch.stop") : t("watch.start");
  return h("button", { type: "button", class: "watch-bell", id: "watch", "aria-pressed": String(watching), "aria-label": label, title: label,
    onclick: () => action(() => (watching ? del(path + "/watch") : put(path + "/watch")), status) }, bellIcon(watching));
}

// what says what a notification's change was.
function what(n) {
  if (n.created) return t("notif.child");
  if (n.mentioned) return n.kind === "BodyCommitted" ? t("notif.mentionedText") : t("notif.mentioned");
  switch (n.kind) {
    case "ItemCreated": return t("notif.created");
    case "ItemImported": return t("notif.imported");
    case "TitleSet": return t("notif.renamed");
    case "BodyCommitted": return t("notif.edited");
    case "PropertySet": return t("notif.field");
    case "Transitioned": return t("notif.state");
    case "ItemMoved": return t("notif.moved");
    case "ItemTrashed": return t("notif.trashed");
    case "ItemRestored": return t("notif.restored");
    case "CommentAdded": return t("notif.commented");
    case "LabelsSet": return t("notif.labels");
    case "AttachmentAdded": return t("notif.attached");
    case "RestrictionSet": return t("notif.restrictedSensitive");
    case "HoldSet": return t("notif.held");
    case "HoldReleased": return t("notif.released");
    case "SecretRequested": return t("notif.secretRequested");
    case "SecretGranted": return t("notif.secretGranted");
    case "SecretDeclined": return t("notif.secretDeclined");
    default: return n.kind;
  }
}

// commentsView is the comment overview (ADR-0063): comments on what one
// watches, one's own and those mentioning one by @username, newest first,
// with a tab for each kind.
async function commentsView(tab = "") {
  const show = screen();
  const kinds = { watched: t("comments.watched"), mine: t("comments.mine"), mentions: t("comments.mentions") };
  const current = kinds[tab] ? tab : "";
  // The feed is paged, so the server filters it (ADR-0098).
  const { spaces } = await get("/api/spaces");
  const filter = serverFilter({ id: "comments", label: t("comments.title"),
    choices: [{ key: "space", label: t("col.space"), options: favouritesFirst(spaces).map((s) => [s.key, `${s.name} (${s.key})`]) }],
    load: async () => {
      list.replaceChildren();
      const page = await get(url(0));
      await add(page);
      emptyNow(page.comments.length);
      return page.comments.length + (page.next ? "+" : "");
    } });
  const url = (offset) => {
    const q = filterParams(new URLSearchParams({ limit: 50 }), filter.state);
    if (current) q.set("only", current);
    if (offset) q.set("offset", offset);
    return "/api/comments?" + q;
  };
  const first = await get(url(0));
  const list = h("ul", { class: "comment-feed", id: "comment-feed" });
  const more = h("div");
  const none = empty(t("comments.none"), "comment-feed-empty");
  const noneFiltered = h("p", { class: "empty filter-empty", id: "comments-filter-empty", text: t("filter.none") });
  const emptyNow = (n) => {
    list.hidden = n === 0;
    none.hidden = n > 0 || filter.active();
    noneFiltered.hidden = n > 0 || !filter.active();
  };
  const add = async (page) => {
    await learnNames(page.comments.map((e) => e.comment.author));
    for (const e of page.comments) {
      const c = e.comment;
      list.append(h("li", { class: "comment-entry" + (c.resolved ? " resolved" : "") },
        avatar(c.author, names.get(c.author) || person(c.author), "", 32),
        h("div", { class: "comment-entry-main" },
          h("div", { class: "comment-entry-head" },
            h("strong", { text: person(c.author) }),
            h("span", { class: "muted", text: " " + (c.reply ? t("comments.replied") : t("comments.commented")) + " " }),
            link("#/item/" + encodeURIComponent(e.key), `${e.key} ${e.title}`),
            h("span", { class: "muted", text: " · " + when(c.created) })),
          c.anchor ? h("blockquote", { class: "comment-quote", text: c.anchor.quote }) : "",
          h("p", { class: "comment-text", text: c.text }),
          h("div", { class: "comment-entry-why" },
            ...e.why.map((w) => h("span", { class: "chip why-" + w, text: kinds[w] })),
            c.resolved ? h("span", { class: "chip resolved", text: t("comments.resolved") }) : ""))));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => add(await get(url(page.next))) }));
  };
  await add(first);
  const tabLink = (id, label) => link("#/comments" + (id ? "/" + id : ""), label, id === current ? "tab active" : "tab", "comments-tab-" + (id || "all"));
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("comments.title") })),
    h("p", { class: "muted", text: t("comments.help") }),
    h("nav", { class: "tabs", "aria-label": t("comments.title") },
      tabLink("", t("comments.all")), tabLink("watched", kinds.watched), tabLink("mine", kinds.mine), tabLink("mentions", kinds.mentions)),
    filter.el,
    list, none, noneFiltered,
    more,
  );
  emptyNow(first.comments.length);
}

// notificationsView lists what others changed on what one watches, and
// marks it seen.
async function notificationsView() {
  const show = screen();
  // The list is paged, so the server filters it (ADR-0098).
  const filter = serverFilter({ id: "notifications", label: t("notif.title"),
    choices: [
      { key: "kind", label: t("filter.kind"), options: [["mentions", t("notif.kindMentions")], ["comments", t("notif.kindComments")], ["changes", t("notif.kindChanges")], ["governance", t("gov.title")]] },
      { key: "unread", label: t("filter.read"), options: [["1", t("notif.unreadOnly")]] },
    ],
    load: async () => {
      list.replaceChildren();
      const page = await get(url());
      await add(page);
      emptyNow(page.notifications.length);
      return page.notifications.length + (page.next ? "+" : "");
    } });
  const url = (before) => {
    const q = filterParams(new URLSearchParams({ limit: 50 }), filter.state);
    if (before) q.set("before", before);
    return "/api/notifications?" + q;
  };
  const [first, { watches }] = await Promise.all([get(url() + "&findings=1"), get("/api/me/watches")]);
  const list = h("ul", { class: "notifications", id: "notifications" });
  const more = h("div");
  const none = empty(watches.length ? t("notif.none") : t("notif.noWatches"));
  const noneFiltered = h("p", { class: "empty filter-empty", id: "notifications-filter-empty", text: t("filter.none") });
  const emptyNow = (n) => {
    list.hidden = n === 0;
    none.hidden = n > 0 || filter.active();
    noneFiltered.hidden = n > 0 || !filter.active();
  };
  const add = async (page) => {
    await learnNames(page.notifications.map((n) => n.actor));
    for (const n of page.notifications) {
      const governance = n.actor === "governance";
      list.append(h("li", { class: [n.unread ? "unread" : "", governance ? "governance" : ""].join(" ").trim() },
        h("span", { text: `${governance ? t("gov.title") : person(n.actor)} ${what(n)} ` }),
        link("#/item/" + encodeURIComponent(n.key), `${n.key} ${n.title}`),
        h("span", { class: "muted", text: " · " + when(n.at) })));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => add(await get(url(page.next))) }));
  };
  await add(first);
  emptyNow(first.notifications.length);
  if (!filter.active() && first.notifications.length && first.notifications[0].seq > first.seen) {
    post("/api/notifications/seen", { seq: first.notifications[0].seq }).then(() => renderNav()).catch(() => {});
  }
  // The governance findings one is responsible for, until they are
  // resolved (ADR-0057).
  const f = first.findings;
  const kindName = { sensitive: t("gov.colSensitive"), stale: t("gov.colStale"), implausible: t("gov.colImplausible"), personal: t("gov.colPersonal"), review: t("gov.colReview") };
  const findings = f.items?.length ? h("section", { id: "my-findings" },
    h("h2", {}, t("notif.findings", { n: f.sensitive + f.stale + f.implausible + (f.personal || 0) + (f.reviews || 0) }), " ", link("#/governance", t("notif.findingsAll"))),
    h("p", { class: "muted", text: t("notif.findingsNote") }),
    filtered(h("ul", { class: "notifications" }, f.items.map((x) => h("li", { class: "finding-" + x.kind, "data-f-kind": x.kind },
      h("span", { class: "chip", text: kindName[x.kind] }), " ",
      link("#/item/" + encodeURIComponent(x.key), `${x.key} ${x.title}`),
      h("span", { class: "muted", text: " · " + (x.kind === "stale" ? t("notif.unchangedSince", { date: when(x.since) })
        : x.kind === "review" ? (x.since ? t("gov.lastReviewed", { date: when(x.since) }) : t("gov.neverReviewed")) : x.detail) }),
      x.release ? h("strong", { class: "finding-release", text: " · " + t("dp.yourRelease") }) : null))),
    { id: "my-findings", label: t("notif.findings", { n: f.items.length }), choices: [{ key: "kind", label: t("filter.kind"), name: (k) => kindName[k] || k }] })) : null;
  show(
    h("h1", { text: t("notif.title") }),
    findings,
    findings ? h("h2", { text: t("notif.changes") }) : null,
    filter.el,
    list, none, noneFiltered,
    more,
    h("h2", { text: t("watch.title") }),
    watches.length
      ? filtered(h("ul", { class: "list", id: "watches" }, watches.map((w) => h("li", {}, w.item
        ? link("#/item/" + encodeURIComponent(w.key), `${w.key} ${w.title}`)
        : [t("watch.space") + " ", link("#/space/" + encodeURIComponent(w.space), w.space)]))), { id: "watches", label: t("watch.title") })
      : empty(t("watch.none")),
  );
}

// --- labels (ADR-0044) ----------------------------------------------------------

function labelChip(label, onRemove) {
  return h("span", { class: "label" },
    h("a", { href: "#/label/" + encodeURIComponent(label), text: label }),
    onRemove ? h("button", { type: "button", class: "label-remove", title: t("label.remove", { label }), "aria-label": t("label.remove", { label }), text: "×", onclick: onRemove }) : null);
}

// statusBar shows an item's state and what can be done with it, told
// apart (ADR-0132): the state is a badge, each transition from it a
// button named by the transition with its target beside it. States it
// cannot reach from here are left out; the type's workflow lists them.
// perform is null for those who may not edit.
function statusBar(wf, state, perform) {
  const moves = perform ? wf.transitions.filter((tr) => tr.from.includes(state)) : [];
  return h("div", { class: "state-bar", id: "item-status" },
    h("span", { class: "state-label", text: t("item.status") }),
    h("span", { class: "state-now" + (finished(wf, state) ? " finished" : ""), text: state }),
    moves.length ? h("div", { class: "state-moves", role: "group", "aria-label": t("item.stateMoves") }, moves.map((tr) =>
      h("button", { type: "button", class: "secondary state-move" + (finished(wf, tr.to) ? " finished" : ""), "data-transition": tr.name,
        title: t("item.stateMove", { transition: tr.name, state: tr.to }), onclick: () => perform(tr) },
      tr.name, tr.to !== state ? h("span", { class: "state-move-to", text: `→ ${tr.to}` }) : ""))) : null);
}

// labelRule is the server's rule for a label after it is lowered and its
// inner spaces joined by "-" (ADR-0044); a label that breaks it is
// refused, here at once and in the reader's language.
const labelRule = /^[\p{Ll}\p{Lo}\p{N}](?:[\p{Ll}\p{Lo}\p{N}._-]{0,62}[\p{Ll}\p{Lo}\p{N}])?$/u;
const normalLabel = (l) => l.toLowerCase().split(/\s+/).filter(Boolean).join("-");

// labelsLine shows an item's labels; on a live item they can be added and
// removed. Several labels are added at once, separated by commas; one
// typed and left is added too. What goes wrong is said beside the labels.
function labelsLine(path, item, live) {
  const status = h("div", { class: "status label-status" });
  const set = async (labels) => {
    await put(path + "/labels", { labels });
    route();
  };
  const remove = (l) => set(item.labels.filter((x) => x !== l)).catch((err) => status.replaceChildren(errorBox(err)));
  const chips = item.labels.map((l) => labelChip(l, live ? () => remove(l) : null));
  const input = h("input", { name: "label", placeholder: t("label.placeholder"), "aria-label": t("label.placeholder"), size: 14, autocomplete: "off", list: "label-options" });
  // The labels in use are suggested while typing, the most used first, so
  // one spelling stays one label; after a comma, for the next label.
  const options = h("datalist", { id: "label-options" });
  let timer = 0;
  input.addEventListener("input", () => {
    clearTimeout(timer);
    const cut = input.value.lastIndexOf(",");
    const before = cut >= 0 ? input.value.slice(0, cut + 1) + " " : "";
    const typed = input.value.slice(cut + 1).trim();
    if (!typed) { options.replaceChildren(); return; }
    timer = setTimeout(async () => {
      const { labels } = await get("/api/labels?q=" + encodeURIComponent(typed)).catch(() => ({ labels: [] }));
      options.replaceChildren(...labels.filter((l) => !item.labels.includes(l.label)).slice(0, 12)
        .map((l) => h("option", { value: before + l.label, label: t("label.suggestion", { n: l.count }) })));
    }, 150);
  });
  const add = live ? form({ class: "label-add" }, [
    input,
    options,
    h("button", { type: "submit", class: "label-add-button", title: t("label.add"), "aria-label": t("label.add"), text: "+" }),
  ], async (data) => {
    status.replaceChildren();
    const typed = String(data.get("label") || "").split(",").map((l) => l.trim()).filter(Boolean);
    if (!typed.length) return;
    const bad = typed.filter((l) => !labelRule.test(normalLabel(l)));
    if (bad.length) throw new Error(t("label.invalid", { label: bad.join(", ") }));
    await set([...item.labels, ...typed]);
  }) : null;
  // A label typed and left behind is added, not lost.
  input.addEventListener("change", () => { if (input.value.trim()) add?.requestSubmit(); });
  if (!chips.length && !add) return null;
  return h("div", { class: "labels", id: "labels" }, chips, add, status);
}

// labelView lists the readable items that carry a label.
async function labelView(label) {
  const show = screen();
  // The items of a label are paged, so the server filters them (ADR-0098).
  const [{ spaces }, { types }] = await Promise.all([get("/api/spaces"), get("/api/types")]);
  const filter = serverFilter({ id: "labelled", label: t("label.heading"),
    choices: [
      { key: "space", label: t("col.space"), options: favouritesFirst(spaces).map((s) => [s.key, `${s.name} (${s.key})`]) },
      { key: "type", label: t("col.type"), options: types.map((ty) => [ty.name, ty.name]) },
    ],
    load: async (state) => {
      const page = await get(url(state));
      fill(page);
      return page.total.value;
    } });
  const url = (state) => "/api/items?" + filterParams(new URLSearchParams({ label }), state);
  const rows = h("tbody");
  const table = h("table", { id: "labelled" }, h("thead", {}, h("tr", {}, [t("col.key"), t("col.title"), t("col.type")].map((c) => h("th", { text: c })))), rows);
  const none = h("p", { class: "muted", id: "labelled-none" });
  const more = h("p", { class: "muted" });
  const csv = csvLink("", "label-csv");
  const fill = (page) => {
    rows.replaceChildren(...page.items.map((it) => h("tr", {},
      h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key, "key")), h("td", { text: it.title }), h("td", { text: it.type }))));
    table.hidden = !page.items.length;
    none.hidden = page.items.length > 0;
    none.textContent = filter.active() ? t("filter.none") : t("label.noItems");
    more.textContent = page.next ? t("label.more", { n: page.total.value }) : "";
    csv.href = "/api/items.csv?" + filterParams(new URLSearchParams({ label }), filter.state);
  };
  const first = await get(url(filter.state));
  fill(first);
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / " + t("label.title")),
    h("div", { class: "page-head" }, h("h1", {}, t("label.heading") + " ", labelChip(label)),
      h("p", { class: "actions" }, link(`#/labelboard/${encodeURIComponent(label)}`, t("labelboard.open"), "button secondary", "label-board"), csv)),
    filter.el,
    table, none, more,
  );
}

// withTree lays a space's page tree beside a view's content.
function withTree(tree, ...content) {
  return h("div", { class: "with-tree" }, h("div", { class: "tree-col" }, columnToggle("tree"), tree), h("div", { class: "with-tree-main" }, ...content));
}

// The page tree and an item's side column fold away to give the content
// room (ADR-0100); the choice is kept in this browser, for every page.
const folded = {
  get(col) { try { return localStorage.getItem("wwbb.fold." + col) === "1"; } catch { return false; } },
  set(col, on) { try { localStorage.setItem("wwbb.fold." + col, on ? "1" : ""); } catch { /* for this page only */ } },
  apply() { for (const col of ["tree", "side"]) document.documentElement.classList.toggle(col + "-folded", folded.get(col)); },
};
folded.apply();

// columnToggle folds a column away or shows it again: "tree" on the left,
// "side" on the right.
function columnToggle(col) {
  const btn = h("button", { type: "button", class: "icon-btn col-toggle col-toggle-" + col, id: "fold-" + col });
  const draw = () => {
    const on = folded.get(col);
    // The chevron points where the column goes or comes from.
    const left = (col === "tree") !== on;
    const name = t(on ? (col === "tree" ? "fold.showTree" : "fold.showSide") : (col === "tree" ? "fold.hideTree" : "fold.hideSide"));
    btn.title = name;
    btn.setAttribute("aria-label", name);
    btn.setAttribute("aria-expanded", String(!on));
    btn.replaceChildren(chevronIcon(left));
  };
  btn.addEventListener("click", () => {
    folded.set(col, !folded.get(col));
    folded.apply();
    document.querySelectorAll(".col-toggle-" + col).forEach((b) => b.dispatchEvent(new Event("redraw")));
  });
  btn.addEventListener("redraw", draw);
  draw();
  return btn;
}

// chevronIcon draws an arrow head pointing left or right.
function chevronIcon(left) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", width: "18", height: "18", "aria-hidden": "true" })) svg.setAttribute(k, v);
  const p = document.createElementNS(ns, "path");
  for (const [k, v] of Object.entries({ d: left ? "M15 5l-7 7 7 7" : "M9 5l7 7-7 7", fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round" })) p.setAttribute(k, v);
  svg.append(p);
  return svg;
}

// childPagesOf lists an item's child pages for a list of child pages in
// its body (ADR-0042).
const childPagesOf = (item) => async () =>
  (await get(`/api/spaces/${encodeURIComponent(item.space)}/tree?parent=${item.id}`)).items;

// filesOf serves and stores an item's attachments for its body (ADR-0045),
// and knows which of them are drawings (ADR-0053).
const filesOf = (item, attachments = []) => {
  const known = new Map(attachments.map((a) => [a.id, a]));
  return {
    item: item.id,
    url: (id) => `/api/items/${item.id}/attachments/${id}`,
    // source names the file a copy or new version comes from (ADR-0055).
    upload: async (file, source) => {
      const a = await upload("/api/items/" + item.id, file, source);
      known.set(a.id, a);
      return a;
    },
    isDrawing: (id) => isDrawing(known.get(id)?.name),
    // list is the item's files known here, for choosing one, and knows
    // whether a file is among them (ADR-0142).
    list: () => [...known.values()],
    knows: (id) => known.has(id),
    // origin says where a copy came from, and whether its original is
    // newer: {readable, item: {id, key, title}, newer}.
    origin: (id) => known.get(id)?.origin || null,
  };
};

// readOnlyBody shows an item's body without editing it; edit, if given,
// stands in the bar where the text begins.
function readOnlyBody(item, embeds, edit) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  return createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item), files: filesOf(item),
    actions: edit ? [edit] : [], embedContent: embedLoader([item.id]), tasks: taskTools(item, false) }).element;
}

// embedDepth is how many embedded items deep a text shows content; deeper
// ones show their heading only.
const embedDepth = 3;

// embedLoader shows an embedded item whole (ADR-0106): its kind, state and
// fields, then its text, read-only, as far as the reader may read it; the
// API veils what they may not. chain holds the items around it, so an item
// never shows itself inside itself.
function embedLoader(chain) {
  return async (ref) => {
    if (chain.includes(ref)) return h("p", { class: "muted pm-embed-note", text: t("embed.cycle") });
    if (chain.length > embedDepth) return h("p", { class: "muted pm-embed-note", text: t("embed.deep") });
    const [{ item }, { embeds }] = await Promise.all([get("/api/items/" + ref), get(`/api/items/${ref}/embeds`)]);
    const resolve = async (r) => (await get("/api/items/" + encodeURIComponent(r))).item;
    const props = Object.entries(item.properties || {}).filter(([, v]) => v !== null && v !== "" && !(Array.isArray(v) && !v.length));
    return h("div", { class: "pm-embed-content", "data-embedded": item.key },
      h("p", { class: "muted pm-embed-meta" }, t("item.meta", { type: item.type, presence: item.presence }), item.state ? t("item.state", { state: item.state }) : ""),
      item.origin?.system === "git:file" ? h("p", { class: "muted pm-embed-meta pm-embed-source" }, ...fileSource(item.origin)) : null,
      props.length ? h("dl", { class: "pm-embed-props" }, props.flatMap(([k, v]) => [h("dt", { text: k }),
        h("dd", { text: (Array.isArray(v) ? v : [v]).map((x) => (names.has(x) ? names.get(x) : String(x))).join(", ") })])) : null,
      item.body.length ? createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item), files: filesOf(item),
        embedContent: embedLoader([...chain, ref]), tasks: taskTools(item, false) }).element : null);
  };
}

// formDialog shows a form in a modal; it answers what submit gave, or
// null when cancelled. ok names the button that submits.
function formDialog(title, id, fields, submit, ok = t("common.create")) {
  return new Promise((resolve) => {
    const status = h("p", { class: "error", role: "alert", hidden: true });
    let result = null;
    const f = h("form", { method: "dialog", class: "note-form" }, fields, status,
      h("div", { class: "actions-row" }, h("button", { type: "submit", class: "primary", id: id + "-ok", text: ok }),
        h("button", { type: "button", class: "link", id: id + "-cancel", text: t("common.cancel"), onclick: () => d.close() })));
    f.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      try {
        result = await submit(f);
        d.close();
      } catch (err) {
        status.textContent = err.message;
        status.hidden = false;
      }
    });
    const name = title.replace(/…$/, "");
    const d = h("dialog", { class: "note-dialog", id, "aria-label": name }, h("div", { class: "drawing-bar" }, h("strong", { text: name })), f);
    d.addEventListener("close", () => { d.remove(); resolve(result); });
    document.body.append(d);
    d.showModal();
    f.querySelector("input, select")?.focus();
  });
}

// markdownTools make a page below the item from a Markdown file of a
// repository, which the text then shows whole (ADR-0138): insert(address)
// asks for the address and the version and answers the page, or null.
function markdownTools(item) {
  const insert = (address) => {
    const url = h("input", { name: "url", type: "url", required: true, value: address || "", maxlength: 1024, id: "markdown-url",
      placeholder: "https://github.com/owner/repository/blob/main/README.md" });
    return formDialog(t("editor.markdownFile"), "markdown-dialog", [
      field(t("markdown.url"), url),
      h("fieldset", { class: "newtask-display" }, h("legend", { text: t("markdown.version") }),
        h("label", { class: "check" }, h("input", { type: "radio", name: "version", value: "current", checked: true }), " ", t("markdown.current")),
        h("label", { class: "check" }, h("input", { type: "radio", name: "version", value: "this" }), " ", t("markdown.this"))),
      h("p", { class: "muted", text: t("markdown.hint", { title: item.title || item.key }) }),
    ], async (f) => {
      const { item: made } = await post(`/api/items/${item.id}/markdown`, { url: url.value.trim(), version: f.querySelector("[name=version]:checked").value });
      return made;
    }, t("markdown.insert"));
  };
  return { insert };
}

// fileSource says where a page made from a Markdown file comes from and
// which version it shows (ADR-0138), in the reader's language.
function fileSource(origin) {
  const file = origin.externalId.slice(origin.externalId.indexOf("/") + 1);
  const repo = origin.instance.replace(/^https?:\/\//, "");
  const commit = (origin.version || "").slice(0, 8);
  return [t("gitfile.from"), " ", h("a", { href: origin.url, target: "_blank", rel: "noopener noreferrer", text: `${repo}: ${file}` }), " · ",
    origin.pinned ? t("gitfile.pinned", { commit }) : t("gitfile.follows", { ref: origin.externalKey, commit })];
}

// fileOrigin is the notice of a page made from a Markdown file: its
// source, and for who may edit it, updating it now and pinning or
// following its version.
function fileOrigin(item, path, canEdit, status) {
  const o = item.origin;
  const buttons = !canEdit ? [] : [
    h("button", { type: "button", class: "link", id: "gitfile-update", text: t("gitfile.update"), onclick: async () => {
      try {
        const r = await post(path + "/markdown/update", {});
        if (r.changed) route();
        else status.replaceChildren(h("p", { class: "muted", text: t("gitfile.unchanged") }));
      } catch (err) {
        status.replaceChildren(errorBox(err));
      }
    } }),
    o.pinned
      ? (o.externalKey ? h("button", { type: "button", class: "link", id: "gitfile-follow", text: t("gitfile.follow", { ref: o.externalKey }),
        onclick: () => action(() => put(path + "/markdown", { version: "current" }), status) }) : null)
      : h("button", { type: "button", class: "link", id: "gitfile-pin", text: t("gitfile.pin"), onclick: () => action(() => put(path + "/markdown", { version: "this" }), status) }),
  ].filter(Boolean);
  return h("p", { class: "notice origin", id: "item-origin" }, ...fileSource(o), ...buttons.flatMap((b) => [" · ", b]));
}

// taskTools are what a text needs for its tasks (ADR-0114): list(label)
// shows the tasks that carry a label, in every space the reader may read,
// as a table one sorts by a column and filters; with item and editing,
// pickLabel asks for a label and create makes a task below the item and
// says whether it goes into the text as a link or shown whole.
function taskTools(item, editing) {
  let typesOnce = null;
  const taskTypes = async () => (typesOnce ??= get("/api/types").then(({ types }) => types.filter((ty) => ty.workflow)));
  const list = async (label) => {
    const [flows, found] = await Promise.all([taskTypes(), readAll(`/api/items?label=${encodeURIComponent(label)}&limit=100`)]);
    const rows = found.items.filter((it) => flows.some((ty) => ty.name === it.type));
    if (!rows.length) return null;
    const cols = [["key", t("col.key")], ["title", t("col.title")], ["state", t("col.state")], ["space", t("col.space")]];
    let by = "key", up = true;
    const order = (a, b) => String(a[by] || "").localeCompare(String(b[by] || ""), undefined, { numeric: true }) * (up ? 1 : -1);
    const body = h("tbody");
    const draw = () => body.replaceChildren(...[...rows].sort(order).map((it) => h("tr", { "data-f-state": it.state || "", "data-f-space": it.space },
      h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key, "key")), h("td", { text: it.title }),
      h("td", {}, it.state ? h("span", { class: "chip", text: it.state }) : ""), h("td", { text: it.space }))));
    const head = h("tr", {}, cols.map(([k, name]) => h("th", { scope: "col", "aria-sort": k === by ? (up ? "ascending" : "descending") : "none" },
      h("button", { type: "button", class: "link sort", "data-sort": k, text: name, onclick: (ev) => {
        up = by === k ? !up : true;
        by = k;
        head.querySelectorAll("th").forEach((th) => th.setAttribute("aria-sort", th.querySelector("button").dataset.sort === by ? (up ? "ascending" : "descending") : "none"));
        draw();
        box.applyFilter();
        ev.currentTarget.focus();
      } }))));
    draw();
    const table = h("table", { class: "list tasklist-table" }, h("thead", {}, head), body);
    const box = filtered(table, { id: "tasks-" + label, label: t("editor.taskListOf", { label }), min: 0,
      choices: [{ key: "state", label: t("col.state") }, { key: "space", label: t("col.space") }] });
    return h("div", { class: "tasklist-box" }, box, found.truncated ? h("p", { class: "muted", text: t("board.truncated", { n: rows.length }) }) : null);
  };
  if (!editing) return { list };
  const dialog = formDialog;
  const pickLabel = async () => {
    const { labels } = await get("/api/labels?tasks=1").catch(() => ({ labels: [] }));
    const input = h("input", { name: "label", required: true, list: "tasklist-labels", autocomplete: "off", placeholder: t("editor.taskListPlaceholder") });
    return dialog(t("editor.taskList"), "tasklist-dialog", [
      field(t("kanban.label"), input),
      h("datalist", { id: "tasklist-labels" }, labels.map((l) => h("option", { value: l.label }))),
      h("p", { class: "muted", text: t("editor.taskListHint") }),
    ], async () => {
      const label = input.value.trim().toLowerCase().split(/\s+/).join("-");
      if (!label) throw new Error(t("editor.taskListNeedsLabel"));
      return label;
    });
  };
  const create = async () => {
    const flows = (await taskTypes()).filter(isTaskType);
    const types = flows.length ? flows : await taskTypes();
    if (!types.length) throw new Error(t("editor.newTaskNoType"));
    const fields = h("div");
    const typeSelect = h("select", { name: "type" }, types.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === "task" })));
    const showFields = () => {
      const ty = types.find((x) => x.name === typeSelect.value);
      fields.replaceChildren(...ty.fields.filter((f) => f.required).map((f) => field(f.name + " *", propertyInput(f))));
    };
    typeSelect.addEventListener("change", showFields);
    showFields();
    const title = h("input", { name: "title", required: true, maxlength: 200, id: "newtask-title" });
    return dialog(t("editor.newTask"), "newtask-dialog", [
      field(t("col.title"), title),
      types.length > 1 ? field(t("col.type"), typeSelect) : null,
      fields,
      h("fieldset", { class: "newtask-display" }, h("legend", { text: t("editor.newTaskShow") }),
        h("label", { class: "check" }, h("input", { type: "radio", name: "display", value: "link", checked: true }), " ", t("editor.newTaskLink")),
        h("label", { class: "check" }, h("input", { type: "radio", name: "display", value: "full" }), " ", t("editor.newTaskFull"))),
      h("p", { class: "muted", text: t("editor.newTaskHint", { title: item.title || item.key }) }),
    ], async (f) => {
      const ty = types.find((x) => x.name === typeSelect.value);
      const properties = {};
      for (const fd of ty.fields.filter((x) => x.required)) {
        const v = readProperty(fd, f.querySelector(`[name="p:${CSS.escape(fd.name)}"]`));
        if (v !== null) properties[fd.name] = v;
      }
      const { item: made } = await post("/api/items", { space: item.space, type: ty.name, parent: item.id, title: title.value.trim(), properties });
      return { item: made, display: f.querySelector("[name=display]:checked").value };
    });
  };
  return { list, pickLabel, create };
}

// sizeText shows a number of bytes for people.
function sizeText(n) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  for (; n >= 1024 && i < units.length - 1; i++) n /= 1024;
  return `${i ? n.toFixed(1) : n} ${units[i]}`;
}

// attachmentsSection lists an item's files (ADR-0045); on a live item more
// can be uploaded and files deleted.
function attachmentsSection(path, files, live) {
  const status = h("div", { class: "status" });
  const kindOf = (a) => a.type?.startsWith("image/") ? t("att.kindImage") : a.type?.startsWith("video/") ? t("att.kindVideo") : t("att.kindOther");
  const row = (a) => h("li", { class: "attachment", "data-f-kind": kindOf(a) },
    h("a", { href: `${path}/attachments/${a.id}`, text: a.name, download: a.inline ? null : a.name, target: a.inline ? "_blank" : null, rel: "noopener" }),
    h("span", { class: "muted", text: ` · ${sizeText(a.size)} · ${person(a.author)} · ${when(a.created)}` }),
    a.origin ? h("span", { class: "att-origin" }, " · ", a.origin.readable
      ? [t("att.copyOf"), " ", link("#/item/" + encodeURIComponent(a.origin.item.key), a.origin.item.key)]
      : t("att.copyOfUnknown"),
    a.origin.newer ? [" ", h("span", { class: "chip", text: t("att.originChanged") })] : null) : null,
    live ? h("button", { class: "link danger", text: t("common.delete"), onclick: () => confirm(t("att.deleteConfirm", { name: a.name }))
      && action(() => del(`${path}/attachments/${a.id}`), status) }) : null);
  const picker = live ? h("input", { type: "file", multiple: true, id: "upload", "aria-label": t("att.upload") }) : null;
  if (picker) {
    picker.addEventListener("change", () => action(async () => {
      for (const file of picker.files) await upload(path, file);
    }, status));
  }
  return h("section", { id: "attachments" },
    h("h2", { text: t("att.title", { n: files.length }) }),
    files.length ? filtered(h("ul", { class: "attachments" }, files.map(row)), { id: "files", label: t("att.title", { n: files.length }),
      choices: [{ key: "kind", label: t("filter.kind") }] }) : empty(t("att.none")),
    picker ? h("label", { class: "upload" }, h("span", { text: t("att.upload") }), picker) : null,
    status);
}

async function action(fn, status) {
  try {
    await fn();
    route();
  } catch (err) {
    if (status) status.replaceChildren(errorBox(err));
    else alert(err.message);
  }
}

// moveItem makes a transition of an item; for a linked item moved to a
// finished state it opens the task dialog instead, as the engine decides
// (ADR-0151).
function moveItem(workflow, it, tr, make, status) {
  if (finishing(workflow, it, tr.to)) return taskDialog(it, tr.to).then((done) => done && route());
  return action(make, status);
}

// --- people ------------------------------------------------------------------

// names caches usernames by user ID, so a view shows who did something
// instead of an ID (ADR-0031, ADR-0035); avatars the content tags of their
// profile pictures (ADR-0050).
const names = new Map();
const avatars = new Map();

// learnNames looks up the usernames of the IDs not yet known, in one
// request. IDs that are no user (such as "governance") stay as they are.
async function learnNames(ids) {
  const missing = [...new Set(ids.filter((id) => id && !names.has(id)))];
  // 100 per request stays below any sensible page budget (ADR-0015).
  for (let i = 0; i < missing.length; i += 100) {
    const chunk = missing.slice(i, i + 100);
    const res = await get("/api/users/names?" + chunk.map((id) => "id=" + encodeURIComponent(id)).join("&"));
    for (const id of chunk) {
      names.set(id, res.names[id] ?? id);
      avatars.set(id, res.avatars?.[id] || "");
    }
  }
}

// person names a user: "you", their username, or the ID as given.
function person(id) {
  if (!id) return "—";
  if (me && id === me.user.id) return t("common.you");
  return names.get(id) ?? id;
}

// face is a person's profile picture, or initials, for lists of who did
// what; learnNames must have seen the ID.
function face(id, size = 24) {
  const tag = me && id === me.user.id ? me.avatar : avatars.get(id);
  return avatar(id, names.get(id) ?? id, tag, size);
}

// --- navigation ------------------------------------------------------------

// renderFooter names the running release for signed-in users, e.g.
// "wikiwikibangbang 0.5.4"; the commit it was built from is its tooltip.
function renderFooter() {
  const footer = document.getElementById("footer");
  if (!me || !me.version) return footer.replaceChildren();
  footer.replaceChildren(h("span", { title: me.revision ? t("home.revision", { revision: me.revision }) : null, text: `wikiwikibangbang ${me.version}` }));
}

// --- module switcher ---------------------------------------------------------

// modules are offered behind the nine dots, as in the atlas console. match
// names the route sections that belong to a module, for its highlight.
// admin marks the modules only administrators see. The notifications are
// reached through the bell in the header, not here.
const modules = [
  { id: "spaces", name: "nav.spaces", href: "#/", match: ["", "space", "item", "board", "links", "search", "query", "label"], icon: "M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z" },
  { id: "new", name: "nav.new", href: "#/new", match: ["new"], icon: "M12 8v8M8 12h8M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z" },
  { id: "tasks", name: "nav.tasks", href: "#/tasks", match: ["tasks", "kanbans", "kanban"], icon: "M9 11l3 3 8-8M20 12v7a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h11" },
  { id: "comments", name: "nav.comments", href: "#/comments", match: ["comments"], icon: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" },
  { id: "album", name: "nav.album", href: "#/album", match: ["album"], icon: "M4 5h16v14H4zM4 16l5-5 4 4 3-3 4 4M15 8h.01" },
  { id: "mcp", name: "nav.mcp", href: "#/mcp", match: ["mcp"], icon: "M12 3l1.8 4.7 4.7 1.8-4.7 1.8L12 16l-1.8-4.7-4.7-1.8 4.7-1.8zM19 15l.8 2.2 2.2.8-2.2.8L19 21l-.8-2.2-2.2-.8 2.2-.8z" },
  { id: "governance", name: "nav.governance", href: "#/governance", match: ["governance"], icon: "M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6zM9 12l2 2 4-4" },
  { id: "account", name: "nav.account", href: "#/account", match: ["account"], icon: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0" },
  { id: "admin", name: "nav.admin", href: "#/admin", match: ["admin"], admin: true, icon: "M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1M15 4v4M9 10v4M17 16v4" },
];

const switcher = document.getElementById("app-switcher");
const drawer = document.getElementById("drawer");
const scrim = document.getElementById("scrim");

function openDrawer() {
  drawer.hidden = false;
  scrim.hidden = false;
  switcher.setAttribute("aria-expanded", "true");
  drawer.querySelector("#drawer-apps a")?.focus();
}

function closeDrawer() {
  if (drawer.hidden) return;
  drawer.hidden = true;
  scrim.hidden = true;
  switcher.setAttribute("aria-expanded", "false");
}

// Home, back and forward sit beside the nine dots on every page. Where the
// browser tells whether there is a way back or forward, the buttons say so.
const historyNav = document.getElementById("history-nav");
const backButton = document.getElementById("nav-back");
const forwardButton = document.getElementById("nav-forward");
backButton.addEventListener("click", () => history.back());
forwardButton.addEventListener("click", () => history.forward());

function updateHistoryNav() {
  historyNav.hidden = !me;
  const nav = window.navigation;
  if (nav && "canGoBack" in nav) {
    backButton.disabled = !nav.canGoBack;
    forwardButton.disabled = !nav.canGoForward;
  }
}

// beforeSearch is where the reader was before the search results, to go
// back to when the search field is cleared.
let beforeSearch = "#/";
window.addEventListener("hashchange", (ev) => {
  const from = new URL(ev.oldURL).hash;
  if (location.hash.startsWith("#/search/") && !from.startsWith("#/search/")) beforeSearch = from || "#/";
});

// clearable makes a search field leave the results when it is cleared with
// its × (or Escape); deleting the words by hand keeps them, so a new search
// can be typed.
function clearable(input) {
  const leave = () => {
    if (!input.value && location.hash.startsWith("#/search/")) location.hash = beforeSearch;
  };
  input.addEventListener("search", leave);
  input.addEventListener("input", (ev) => { if (!ev.inputType) leave(); });
  return input;
}

switcher.addEventListener("click", () => (drawer.hidden ? openDrawer() : closeDrawer()));
document.getElementById("drawer-close").addEventListener("click", () => { closeDrawer(); switcher.focus(); });
scrim.addEventListener("click", closeDrawer);
document.getElementById("drawer-apps").addEventListener("click", closeDrawer);
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape" && !drawer.hidden) {
    closeDrawer();
    switcher.focus();
  }
});

// renderModules lists the modules and marks the one the route belongs to.
function renderModules(section) {
  switcher.hidden = !me;
  updateHistoryNav();
  mount(document.getElementById("drawer-apps"), ...modules.filter((m) => !m.admin || me?.admin).map((m) =>
    h("a", { href: m.href, class: m.match.includes(section) ? "active" : "", "aria-current": m.match.includes(section) ? "page" : "false" },
      lineIcon(m.icon), h("span", { text: t(m.name) }))));
}

// labelChrome translates the fixed parts of index.html.
function labelChrome() {
  readingExit.textContent = t("reading.exit");
  for (const el of [switcher, drawer, document.getElementById("drawer-apps")]) el.setAttribute("aria-label", t("nav.modules"));
  switcher.title = t("nav.modules");
  const close = document.getElementById("drawer-close");
  close.title = t("common.close");
  close.setAttribute("aria-label", t("common.close"));
  for (const [id, key] of [["nav-home", "nav.home"], ["nav-back", "nav.back"], ["nav-forward", "nav.forward"]]) {
    const el = document.getElementById(id);
    el.title = t(key);
    el.setAttribute("aria-label", t(key));
  }
}

// lockIcon draws a padlock, closed or open, as SVG.
function lockIcon(closed) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", width: "18", height: "18", "aria-hidden": "true", class: "lock" })) svg.setAttribute(k, v);
  const body = document.createElementNS(ns, "rect");
  for (const [k, v] of Object.entries({ x: "5", y: "11", width: "14", height: "10", rx: "2", fill: "currentColor" })) body.setAttribute(k, v);
  const shackle = document.createElementNS(ns, "path");
  // An open lock's shackle swings up and away on the right.
  shackle.setAttribute("d", closed ? "M8 11V8a4 4 0 0 1 8 0v3" : "M8 11V7a4 4 0 0 1 7.5-2");
  for (const [k, v] of Object.entries({ fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round" })) shackle.setAttribute(k, v);
  svg.append(shackle, body);
  return svg;
}

// bellIcon draws a bell, as SVG: h() makes HTML elements only; filled
// fills it.
// lineIcon draws an outlined icon from a path, in the text's colour.
function lineIcon(d) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", d);
  path.setAttribute("fill", "none");
  path.setAttribute("stroke", "currentColor");
  path.setAttribute("stroke-width", "1.8");
  path.setAttribute("stroke-linecap", "round");
  path.setAttribute("stroke-linejoin", "round");
  svg.append(path);
  return svg;
}

function bellIcon(filled = false) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "20");
  svg.setAttribute("height", "20");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "M12 3a6 6 0 0 0-6 6v3.6L4.3 15.4A1 1 0 0 0 5.2 17h13.6a1 1 0 0 0 .9-1.6L18 12.6V9a6 6 0 0 0-6-6zm-2 15a2 2 0 0 0 4 0");
  path.setAttribute("fill", filled ? "currentColor" : "none");
  path.setAttribute("stroke", "currentColor");
  path.setAttribute("stroke-width", "2");
  path.setAttribute("stroke-linejoin", "round");
  svg.append(path);
  return svg;
}

// searchSuggestions offers, while one types in the search at the top, the
// labels that hold what is typed and the items that answer it best;
// choosing one opens it (ADR-0092). Arrow keys move through them, Enter
// opens the chosen one, Escape closes the list; without a choice, Enter
// searches as before.
function searchSuggestions(form) {
  const input = form.querySelector("input");
  const list = h("ul", { class: "suggest-list", id: "search-suggest", role: "listbox", "aria-label": t("search.suggestions"), hidden: true });
  input.setAttribute("role", "combobox");
  input.setAttribute("aria-autocomplete", "list");
  input.setAttribute("aria-controls", list.id);
  input.setAttribute("aria-expanded", "false");
  input.setAttribute("autocomplete", "off");
  let active = -1;
  let timer = null;
  let asked = 0;
  const options = () => [...list.querySelectorAll("[role=option]")];
  const close = () => {
    list.hidden = true;
    active = -1;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
  };
  const mark = (i) => {
    const all = options();
    active = all.length ? (i + all.length) % all.length : -1;
    all.forEach((o, j) => o.setAttribute("aria-selected", String(j === active)));
    if (active >= 0) input.setAttribute("aria-activedescendant", all[active].id);
  };
  const go = (href) => {
    close();
    input.value = "";
    location.hash = href;
  };
  let n = 0;
  const option = (href, data, ...children) => h("li", { role: "option", id: "search-suggest-" + n++, "aria-selected": "false", class: "suggest-option",
    "data-href": href, ...data, onmousedown: (ev) => { ev.preventDefault(); go(href); } }, ...children);
  const group = (text) => h("li", { role: "presentation", class: "suggest-group", text });
  input.addEventListener("input", () => {
    clearTimeout(timer);
    const typed = input.value.trim();
    if (!/[\p{L}\p{N}]/u.test(typed)) return close();
    timer = setTimeout(async () => {
      const mine = ++asked;
      const [{ labels }, { hits }] = await Promise.all([
        get("/api/labels?q=" + encodeURIComponent(typed)).catch(() => ({ labels: [] })),
        get("/api/search?limit=5&q=" + encodeURIComponent(typed)).catch(() => ({ hits: [] }))]);
      if (mine !== asked || input.value.trim() !== typed) return;
      n = 0;
      const shownLabels = labels.slice(0, 4);
      list.replaceChildren(
        ...(shownLabels.length ? [group(t("search.labelsGroup")), ...shownLabels.map((l) =>
          option("#/label/" + encodeURIComponent(l.label), { "data-label": l.label },
            h("span", { class: "label", text: l.label }), h("span", { class: "muted", text: t("label.suggestion", { n: l.count }) })))] : []),
        ...(hits.length ? [group(t("search.itemsGroup")), ...hits.map((hit) =>
          option("#/item/" + encodeURIComponent(hit.item.key), { "data-item": hit.item.key },
            h("span", { class: "suggest-item" }, h("span", { class: "suggest-key", text: hit.item.key }), " ", h("span", { text: hit.item.title })),
            h("span", { class: "muted", text: hit.item.type })))] : []));
      list.hidden = !(shownLabels.length || hits.length);
      active = -1;
      input.setAttribute("aria-expanded", String(!list.hidden));
    }, 200);
  });
  input.addEventListener("keydown", (ev) => {
    if (list.hidden) return;
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      ev.preventDefault();
      mark(active + (ev.key === "ArrowDown" ? 1 : -1));
    } else if (ev.key === "Enter" && active >= 0) {
      ev.preventDefault();
      go(options()[active].dataset.href);
    } else if (ev.key === "Escape") {
      close();
    }
  });
  input.addEventListener("blur", () => setTimeout(close, 100));
  form.addEventListener("submit", close);
  form.append(list);
}

function renderNav() {
  labelChrome();
  if (!me) {
    nav.replaceChildren(lookToggles());
    return;
  }
  // What someone is typing into the search survives a redraw, e.g. when a
  // page replaces its address by the item's canonical one.
  const typing = nav.querySelector("input[name=q]");
  const keep = typing && document.activeElement === typing ? { value: typing.value, at: typing.selectionStart ?? typing.value.length } : null;
  const current = keep ? keep.value : location.hash.startsWith("#/search/") ? decodeURIComponent(location.hash.slice(9)) : "";
  const search = h("form", { class: "search", role: "search", onsubmit: (ev) => {
    ev.preventDefault();
    const q = new FormData(ev.target).get("q").trim();
    if (q) location.hash = "#/search/" + encodeURIComponent(q);
  } }, clearable(h("input", { name: "q", type: "search", placeholder: t("nav.search"), "aria-label": t("nav.search"), value: current })));
  searchSuggestions(search);
  // The unread count arrives after the navigation is drawn (ADR-0046).
  // A bell with the number of unread notifications; its name says it in
  // words, for screen readers and as a tooltip.
  const count = h("span", { class: "count", hidden: true });
  const bell = h("a", { href: "#/notifications", id: "nav-notifications", class: "bell", title: t("nav.notifications"), "aria-label": t("nav.notifications") },
    bellIcon(), count);
  get("/api/notifications?limit=1").then((n) => {
    const shown = n.unread >= 100 ? "99+" : String(n.unread);
    count.textContent = shown;
    count.hidden = !n.unread;
    // Open governance findings one is responsible for show as a dot until
    // they are resolved (ADR-0057); the number counts unread changes.
    const open = n.findings ? n.findings.sensitive + n.findings.stale + n.findings.implausible + (n.findings.personal || 0) + (n.findings.reviews || 0) : 0;
    bell.classList.toggle("findings", open > 0);
    const name = (n.unread ? t("nav.notificationsUnread", { n: shown }) : t("nav.notifications"))
      + (open ? " · " + t("nav.findingsOpen", { n: open }) : "");
    bell.title = name;
    bell.setAttribute("aria-label", name);
    bell.classList.toggle("unread", n.unread > 0);
  }).catch(() => {});
  // The spaces are reached through the logo and the module switcher; the
  // page tree navigates within a space (ADR-0041).
  mount(nav,
    search,
    h("button", { id: "nav-new", class: "primary", type: "button", text: t("nav.newButton"), onclick: () => { location.hash = newHref(); } }),
    bell,
    lookToggles(),
    accountMenu(),
  );
  if (keep) {
    const input = search.querySelector("input[name=q]");
    input.focus();
    input.setSelectionRange(keep.at, keep.at);
  }
}

// --- reading mode (ADR-0101) --------------------------------------------------

// readingMode shows the content alone: no top bar, no page tree, no side
// column. It starts with its button, Ctrl+Shift+F, or the browser's full
// screen (F11); Esc, the same keys or its button end it.
const readingMode = {
  on: false,
  set(on) {
    if (on === this.on) return;
    this.on = on;
    document.documentElement.classList.toggle("reading-mode", on);
  },
};
const readingExit = h("button", { type: "button", class: "reading-exit", id: "reading-exit", onclick: () => readingMode.set(false) });
document.body.append(readingExit);
document.addEventListener("keydown", (ev) => {
  if ((ev.ctrlKey || ev.metaKey) && ev.shiftKey && ev.key.toLowerCase() === "f") {
    ev.preventDefault();
    readingMode.set(!readingMode.on);
  } else if (ev.key === "Escape" && readingMode.on && !document.querySelector("dialog[open]")) {
    readingMode.set(false);
  }
});
// The browser's own full screen (F11) is reading mode too, and leaving it
// ends it. The page itself never asks for the full screen: a request and
// its late answer would race with the keys.
window.matchMedia("(display-mode: fullscreen)").addEventListener("change", (ev) => readingMode.set(ev.matches));

// expandIcon draws four corners pointing outwards.
function expandIcon() {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", width: "18", height: "18", "aria-hidden": "true" })) svg.setAttribute(k, v);
  const p = document.createElementNS(ns, "path");
  for (const [k, v] of Object.entries({ d: "M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5", fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round" })) p.setAttribute(k, v);
  svg.append(p);
  return svg;
}

// --- language and look (ADR-0095) -------------------------------------------

// localLanguage is the language chosen in this browser before signing in.
function localLanguage() {
  try { return localStorage.getItem("wwbb.language") || ""; } catch { return ""; }
}

// switchLanguage changes the UI's language: a signed-in user's is saved
// with the account (ADR-0017), anyone else's in this browser.
async function switchLanguage(code) {
  if (me) {
    await put("/api/me/language", { language: code });
    me.language = code;
  } else {
    try { localStorage.setItem("wwbb.language", code); } catch { /* for this page only */ }
  }
  setLanguage(code);
  route();
}

// darkOffered is false when the installation turned the dark look off
// (ADR-0070); the bulb is not shown then.
let darkOffered = true;

// bulbIcon draws a light bulb, lit with rays or unlit.
function bulbIcon(lit) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ viewBox: "0 0 24 24", width: "20", height: "20", "aria-hidden": "true" })) svg.setAttribute(k, v);
  const paths = [
    ["M9 18h6M10 21h4", "none"],
    ["M12 4a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 4z", lit ? "currentColor" : "none"],
  ];
  if (lit) paths.push(["M12 1v1.5M4.2 4.2l1 1M19.8 4.2l-1 1M1.5 11H3M21 11h1.5", "none"]);
  for (const [d, fill] of paths) {
    const p = document.createElementNS(ns, "path");
    for (const [k, v] of Object.entries({ d, fill, stroke: "currentColor", "stroke-width": "1.8", "stroke-linecap": "round", "stroke-linejoin": "round" })) p.setAttribute(k, v);
    svg.append(p);
  }
  return svg;
}

// lookToggles are the header's switches of the language and of the light
// or dark look.
function lookToggles() {
  const offered = languages.filter((l) => !me?.languages || me.languages.includes(l.code))
    .sort((a, b) => a.code.localeCompare(b.code));
  const langs = offered.length > 1 ? h("span", { class: "lang-toggle", id: "lang-toggle", role: "group", "aria-label": t("nav.language") },
    offered.map((l) => h("button", { type: "button", lang: l.code, "data-lang": l.code, text: l.code.toUpperCase(), title: l.name,
      "aria-label": l.name, "aria-pressed": String(l.code === language()),
      onclick: () => l.code !== language() && action(() => switchLanguage(l.code)) }))) : null;
  const bulb = h("button", { type: "button", class: "icon-btn theme-toggle", id: "theme-toggle", hidden: !darkOffered });
  const draw = () => {
    const dark = isDark();
    const name = dark ? t("nav.lightMode") : t("nav.darkMode");
    bulb.replaceChildren(bulbIcon(!dark));
    bulb.title = name;
    bulb.setAttribute("aria-label", name);
  };
  bulb.addEventListener("click", () => {
    const next = isDark() ? "light" : "dark";
    // Back to the system's look when the choice is what the system says.
    setTheme(next === (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light") ? "" : next);
  });
  document.addEventListener("themechange", draw);
  onLeave(() => document.removeEventListener("themechange", draw));
  draw();
  const reader = h("button", { type: "button", class: "icon-btn reading-toggle", id: "reading-toggle",
    title: t("reading.enter"), "aria-label": t("reading.enter"), onclick: () => readingMode.set(true) }, expandIcon());
  return h("span", { class: "look-toggles" }, langs, bulb, reader);
}

// avatar shows a user's profile picture, or their initials when they have
// none (ADR-0050). tag names the picture's content, so a new one is
// fetched when it changes.
function avatar(id, name, tag, size = 32) {
  const initials = (name || "?").split(/[\s._@-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join("").toUpperCase() || "?";
  // The initials are drawn by CSS from an attribute, so they are no part
  // of the text beside them, which names the person.
  const box = h("span", { class: `avatar avatar-${size}`, "aria-hidden": "true" });
  if (tag) {
    const img = h("img", { src: `/api/users/${encodeURIComponent(id)}/avatar?v=${encodeURIComponent(tag)}`, alt: "", width: size, height: size });
    img.addEventListener("error", () => { img.remove(); box.dataset.initials = initials; });
    box.append(img);
  } else {
    box.dataset.initials = initials;
  }
  return box;
}

// setRequests records how many account requests wait, as a view that
// read them found, and updates the dot on the account button, which was
// drawn before the view.
function setRequests(n) {
  me.requests = n;
  const button = document.getElementById("account-button");
  if (button) showRequests(button);
}

function showRequests(button) {
  const waiting = me.admin && me.requests > 0;
  button.classList.toggle("pending", waiting);
  button.title = t("nav.accountOf", { name: me.user.username }) + (waiting ? " · " + t("nav.requestsWaiting", { n: me.requests }) : "");
  button.setAttribute("aria-label", button.title);
}

// accountMenu is the account's button in the header: the profile picture
// or initials, opening a menu with the account, the AI assistants, the
// administration for administrators, and signing out.
function accountMenu() {
  const button = h("button", { type: "button", id: "account-button", class: "account-button", "aria-haspopup": "menu", "aria-expanded": "false",
    "aria-controls": "account-menu", title: t("nav.accountOf", { name: me.user.username }), "aria-label": t("nav.accountOf", { name: me.user.username }) },
  avatar(me.user.id, me.user.username, me.avatar));
  // Account requests waiting for an administrator show as a dot
  // (ADR-0061).
  showRequests(button);
  const item = (href, text) => h("a", { href, role: "menuitem", text, tabindex: "-1" });
  const roles = [me.admin ? t("admin.roleAdmin") : null, me.erasure ? t("admin.roleErasure") : null].filter(Boolean);
  const menu = h("div", { id: "account-menu", class: "menu", role: "menu", hidden: true, "aria-label": t("nav.account") },
    h("div", { class: "menu-head" }, avatar(me.user.id, me.user.username, me.avatar, 40),
      h("div", {}, h("strong", { text: me.user.username }), roles.length ? h("div", { class: "muted", text: roles.join(", ") }) : null)),
    item("#/account", t("nav.accountPage")),
    item("#/mcp", t("nav.mcp")),
    me.admin ? item("#/admin", me.requests ? t("nav.adminRequests", { n: me.requests }) : t("nav.admin")) : null,
    h("hr"),
    h("button", { type: "button", role: "menuitem", tabindex: "-1", class: "link", id: "logout", text: t("nav.logout"),
      onclick: () => action(async () => { await post("/api/logout"); me = null; location.hash = "#/"; }) }));
  const entries = () => [...menu.querySelectorAll("[role=menuitem]")];
  const close = (focus) => {
    if (menu.hidden) return;
    menu.hidden = true;
    button.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", outside, true);
    if (focus) button.focus();
  };
  const outside = (ev) => { if (!menu.contains(ev.target) && !button.contains(ev.target)) close(false); };
  const open = () => {
    menu.hidden = false;
    button.setAttribute("aria-expanded", "true");
    document.addEventListener("click", outside, true);
    entries()[0]?.focus();
  };
  button.addEventListener("click", () => (menu.hidden ? open() : close(true)));
  menu.addEventListener("click", (ev) => { if (ev.target.closest("[role=menuitem]")) close(false); });
  menu.addEventListener("keydown", (ev) => {
    const list = entries();
    const at = list.indexOf(document.activeElement);
    if (ev.key === "Escape") { ev.preventDefault(); close(true); }
    else if (ev.key === "ArrowDown") { ev.preventDefault(); list[(at + 1) % list.length].focus(); }
    else if (ev.key === "ArrowUp") { ev.preventDefault(); list[(at - 1 + list.length) % list.length].focus(); }
    else if (ev.key === "Home") { ev.preventDefault(); list[0].focus(); }
    else if (ev.key === "End") { ev.preventDefault(); list[list.length - 1].focus(); }
    else if (ev.key === "Tab") close(false);
  });
  onLeave(() => close(false));
  return h("div", { class: "account" }, button, menu);
}

// here is where the user is: the space and item shown, so that "New"
// creates in the right place. Views set it; route() clears it.
const here = { space: "", parent: "" };

// newHref is the address of the creation view for the place shown.
function newHref(space = here.space, parent = here.parent, type = "") {
  const q = new URLSearchParams();
  if (space) q.set("space", space);
  if (parent) q.set("parent", parent);
  if (type) q.set("type", type);
  return "#/new" + (q.size ? "?" + q : "");
}

// navigation counts route() calls. A view shows its content only if no
// later navigation started while it was loading; otherwise a slow view
// would replace the one the user chose after it.
let navigation = 0;

// screen captures the current navigation; call it before a view's first
// await and show the view through the function it returns.
function screen() {
  const n = navigation;
  return (...children) => { if (n === navigation) mount(app, ...children); };
}

// leaving holds what the current view must end when another replaces it,
// such as a live session.
let leaving = [];
function onLeave(fn) { leaving.push(fn); }

async function route() {
  const hash = location.hash.replace(/^#/, "") || "/";
  for (const fn of leaving) fn();
  leaving = [];
  here.space = here.parent = "";
  const show = (navigation++, screen());
  try {
    if (!me) {
      try {
        me = await get("/api/me");
      } catch (err) {
        if (!(err instanceof ApiError) || err.status !== 401) throw err;
      }
      await chooseLanguage();
    }
    renderNav();
    renderFooter();
    closeDrawer();
    const [, section, arg, arg2] = hash.split("?")[0].split("/").map(decodeURIComponent);
    const params = new URLSearchParams(hash.split("?")[1] || "");
    renderModules(section || "");
    if (section === "reset") return resetView(arg || "");
    // An assistant's consent page survives signing in, single sign-on
    // included, which returns to the start page (ADR-0068).
    if (!me) {
      if (section === "oauth") try { sessionStorage.setItem("wwbb-after-login", location.hash); } catch { /* not kept */ }
      return loginView();
    }
    let after = null;
    try { after = sessionStorage.getItem("wwbb-after-login"); sessionStorage.removeItem("wwbb-after-login"); } catch { /* not kept */ }
    if (after && after !== location.hash) {
      location.hash = after;
      return;
    }
    switch (section) {
      case "new": return await newView(params);
      case "tasks": return await tasksView(arg, params);
      case "kanbans": return await kanbansView();
      case "kanban": return await kanbanView(arg, arg2);
      case "labelboard": return await labelBoardView(arg, params.get("space") || "");
      case "space": return await spaceView(arg);
      case "board": return await boardView(arg, arg2);
      case "links": return await linksView(arg);
      case "governance": return arg ? await governanceView(arg) : await governanceOverview();
      case "item": return await itemView(arg, params);
      case "label": return await labelView(arg);
      case "notifications": return await notificationsView();
      case "my-notes": return await myNotesView();
      case "comments": return await commentsView(arg);
      case "album": return albumView();
      case "search": return await searchView(arg || "");
      case "query": return await queryView(params);
      case "account": return await accountView();
      case "mcp": return await mcpView();
      case "oauth": return await oauthView(params);
      case "admin": return await adminView();
      case "members": return await membersView(arg);
      default: return await homeView();
    }
  } catch (err) {
    show(errorBox(err));
  }
}

// chooseLanguage applies the signed-in user's language (ADR-0017). The
// browser's language is only a suggestion, taken on the first sign-in and
// saved, so it does not change behind the user's back later.
async function chooseLanguage() {
  if (!me) return setLanguage(localLanguage() || suggestedLanguage());
  if (!me.language) {
    me.language = localLanguage() || suggestedLanguage();
    await put("/api/me/language", { language: me.language }).catch(() => {});
  }
  setLanguage(me.language);
}

// --- corporate design (ADR-0052) -----------------------------------------------

// The start page's picture, by the address the page gives it, which
// changes with its content.
const welcomeArt = document.querySelector("meta[name=wwbb-welcome]")?.content || "/assets/brand/welcome.svg";

// ownWelcome is the design's picture for the start page, and in the dark
// look its picture for the dark look, when it has one (ADR-0109).
function ownWelcome(b) {
  const src = (name) => `/branding/${name}?v=` + encodeURIComponent(b.assets[name]);
  const img = h("img", { class: "welcome-art welcome-own", id: "welcome-art", src: src("welcome"), alt: "" });
  if (b.assets["welcome-dark"]) {
    const pick = () => { img.src = src(darkOffered && isDark() ? "welcome-dark" : "welcome"); };
    pick();
    document.addEventListener("themechange", pick);
    onLeave(() => document.removeEventListener("themechange", pick));
  }
  return img;
}

// The product's own logo, kept to come back to when a design's logo goes.
// The browser's icon stays the product's, whatever the design.
const productLogo = document.querySelector(".brand picture")?.cloneNode(true);

// applyBranding shows the installation's logo and name; the colours, fonts
// and background come with /branding.css. reload asks for the stylesheet
// again after an administrator changed the design.
async function applyBranding(reload = false) {
  const b = await get("/branding.json").catch(() => null);
  if (!b) return;
  const brand = document.querySelector(".brand");
  if (b.assets?.logo) {
    // A logo for the dark look comes in as the product's does, by the
    // system's scheme, which applyTheme below rewrites for a chosen look
    // (ADR-0109).
    const logo = h("img", { src: "/branding/logo?v=" + encodeURIComponent(b.assets.logo), alt: b.name || "wikiwikibangbang", class: "brand-logo" });
    brand.replaceChildren(b.assets["logo-dark"]
      ? h("picture", {}, h("source", { srcset: "/branding/logo-dark?v=" + encodeURIComponent(b.assets["logo-dark"]), media: "(prefers-color-scheme: dark)" }), logo)
      : logo);
  } else {
    if (productLogo) brand.replaceChildren(productLogo.cloneNode(true));
  }
  // Without a dark scheme (ADR-0070) the product's logo stays the dark one,
  // and there is no look to switch to.
  if (b.dark === false) brand.querySelector("picture source")?.remove();
  darkOffered = b.dark !== false;
  document.getElementById("theme-toggle")?.toggleAttribute("hidden", !darkOffered);
  applyTheme();
  // The browser's tab names the instance (ADR-0097).
  document.title = b.name || "wikiwikibangbang";
  if (reload) {
    document.getElementById("branding-css").href = "/branding.css?v=" + Date.now();
    document.getElementById("dark-css").href = "/theme-dark.css?v=" + Date.now();
  }
}

// The editor's toolbar stays just below the top bar while a text scrolls
// (ADR-0149); the top bar wraps on narrow screens, so its height is
// measured rather than assumed.
const topBar = document.querySelector("header.top");
if (topBar && "ResizeObserver" in window) {
  new ResizeObserver(() => document.documentElement.style.setProperty("--top-offset", topBar.offsetHeight + "px")).observe(topBar);
}
window.addEventListener("hashchange", route);
applyTheme();
applyBranding();
route();

// --- login -------------------------------------------------------------------

async function loginView() {
  const show = screen();
  const params = new URLSearchParams(location.search);
  const [sso, signup] = await Promise.all([get("/api/oidc").catch(() => ({ enabled: false })), get("/api/signup").catch(() => ({ open: false }))]);
  const brand = await get("/branding.json").catch(() => ({}));
  // Older servers answer one provider without a list.
  const providers = sso.providers || (sso.enabled ? [{ id: "", name: sso.name }] : []);
  const card = h("section", { class: "card narrow", id: "login-card" });
  // The ways in, in one card: single sign-on first where it is set up,
  // then username and password, then help for a forgotten password and
  // for those without an account (ADR-0030, ADR-0061, ADR-0064). Both
  // links are always there; where a way is not set up, it says whom to
  // ask instead.
  const signIn = () => card.replaceChildren(
    h("h1", { text: t("login.title") }),
    h("p", { class: "muted login-lead", text: t("login.lead", { name: brand.name || "wikiwikibangbang" }) }),
    params.get("login_error") ? h("p", { class: "error", role: "alert", text: params.get("login_error") === "domain" ? t("login.domain") : t("login.failed") }) : "",
    providers.length ? h("div", { class: "login-sso-list" }, ...providers.map((p) =>
      h("a", { class: "button login-sso login-sso-" + (p.kind || "generic"), id: "login-sso-" + p.id, href: "/api/oidc/login?provider=" + encodeURIComponent(p.id) },
        providerMark(p.kind), h("span", { text: t("login.with", { name: p.name }) })))) : "",
    providers.length ? h("p", { class: "login-or", text: t("login.or") }) : "",
    form({ id: "login" }, [
      field(t("field.username"), h("input", { name: "username", autocomplete: "username", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", autocomplete: "current-password", required: true })),
      h("button", { type: "submit", class: sso.enabled ? "secondary login-submit" : "login-submit", text: t("login.submit") }),
    ], async (data) => {
      await post("/api/login", { username: data.get("username"), password: data.get("password") });
      me = null;
      history.replaceState(null, "", "/#/");
      route();
    }),
    h("div", { class: "login-links" },
      h("button", { type: "button", class: "link", id: "forgot-open", text: t("reset.forgot"), onclick: forgotForm }),
      h("span", {}, t("signup.question"), " ", h("button", { type: "button", class: "link", id: "signup-open", text: t("signup.open"), onclick: requestForm }))),
  );
  // explain stands in for a form whose way is not set up on this server.
  const explain = (title, text, id) => card.replaceChildren(
    h("h1", { text: title }),
    h("p", { class: "notice", id, text }),
    h("p", { class: "login-more" }, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })));
  // forgotForm asks for a reset link by mail (ADR-0064); the answer does
  // not say whether the account exists.
  const forgotForm = () => !signup.passwordReset ? explain(t("reset.title"), t("reset.noMail"), "forgot-closed") : card.replaceChildren(
    h("h1", { text: t("reset.title") }),
    h("p", { class: "muted", text: t("reset.help") }),
    form({ id: "forgot" }, [
      field(t("field.username"), h("input", { name: "username", autocomplete: "username", required: true })),
      h("button", { type: "submit", text: t("reset.send") }),
    ], async (data) => {
      await post("/api/password-reset", { username: data.get("username") });
      card.replaceChildren(
        h("h1", { text: t("reset.title") }),
        h("p", { class: "notice success", id: "forgot-sent", text: t("reset.sent") }),
        h("p", {}, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })));
    }),
    h("p", { class: "login-more" }, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })),
  );
  // requestForm asks for an account (ADR-0061): the password is chosen
  // now, an administrator approves or rejects the request.
  const requestForm = () => !signup.open ? explain(t("signup.title"), t("signup.closed"), "signup-closed") : card.replaceChildren(
    h("h1", { text: t("signup.title") }),
    h("p", { class: "muted", text: t("signup.help") }),
    form({ id: "signup" }, [
      field(t("field.username"), h("input", { name: "username", autocomplete: "username", required: true, maxlength: 64 })),
      field(t("contacts.email"), h("input", { name: "email", type: "email", autocomplete: "email", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      field(t("signup.reason"), h("textarea", { name: "reason", rows: 3, maxlength: 500, placeholder: t("signup.reasonHint") })),
      h("button", { type: "submit", text: t("signup.submit") }),
    ], async (data) => {
      await post("/api/signup", { username: data.get("username"), email: data.get("email"), password: data.get("password"), reason: data.get("reason") });
      card.replaceChildren(
        h("h1", { text: t("signup.title") }),
        h("p", { class: "notice success", id: "signup-sent", text: t("signup.sent") }),
        h("p", {}, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })));
    }),
    h("p", { class: "login-more" }, h("button", { type: "button", class: "link", text: t("signup.back"), onclick: signIn })),
  );
  signIn();
  show(card);
}

// providerMark is the sign of an identity provider on its sign-in button:
// Microsoft's four squares, Google's G, or a key for any other.
function providerMark(kind) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  const add = (tag, attrs) => {
    const el = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
    svg.append(el);
  };
  if (kind === "microsoft") {
    add("rect", { x: 2, y: 2, width: 9.5, height: 9.5, fill: "#f25022" });
    add("rect", { x: 12.5, y: 2, width: 9.5, height: 9.5, fill: "#7fba00" });
    add("rect", { x: 2, y: 12.5, width: 9.5, height: 9.5, fill: "#00a4ef" });
    add("rect", { x: 12.5, y: 12.5, width: 9.5, height: 9.5, fill: "#ffb900" });
  } else if (kind === "google") {
    add("path", { d: "M21.6 12.2c0-.7-.1-1.4-.2-2H12v3.8h5.4a4.6 4.6 0 0 1-2 3v2.5h3.2c1.9-1.7 3-4.3 3-7.3z", fill: "#4285f4" });
    add("path", { d: "M12 22c2.7 0 5-.9 6.6-2.4l-3.2-2.5c-.9.6-2 1-3.4 1-2.6 0-4.8-1.8-5.6-4.1H3.1v2.6A10 10 0 0 0 12 22z", fill: "#34a853" });
    add("path", { d: "M6.4 14c-.2-.6-.3-1.3-.3-2s.1-1.4.3-2V7.4H3.1A10 10 0 0 0 2 12c0 1.6.4 3.1 1.1 4.6L6.4 14z", fill: "#fbbc05" });
    add("path", { d: "M12 5.9c1.5 0 2.8.5 3.8 1.5l2.9-2.9A10 10 0 0 0 3.1 7.4L6.4 10C7.2 7.7 9.4 5.9 12 5.9z", fill: "#ea4335" });
  } else {
    add("path", { d: "M15 7a3 3 0 1 1-3 3M3 21l7.5-7.5M7 17l2 2M9 15l2 2", fill: "none", stroke: "currentColor", "stroke-width": 1.8, "stroke-linecap": "round" });
  }
  return svg;
}

// resetView chooses a new password with the token of a reset link
// (ADR-0064). The token travels in the address's fragment, so it reaches
// no server log and no other site.
function resetView(token) {
  const show = screen();
  const card = h("section", { class: "card narrow", id: "reset-card" });
  card.replaceChildren(
    h("h1", { text: t("reset.choose") }),
    form({ id: "reset" }, [
      field(t("account.new"), h("input", { name: "password", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      field(t("reset.repeat"), h("input", { name: "repeat", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      h("button", { type: "submit", class: "primary", text: t("reset.save") }),
    ], async (data) => {
      if (data.get("password") !== data.get("repeat")) throw new Error(t("admin.differ"));
      await post("/api/password-reset/confirm", { token, password: data.get("password") });
      me = null;
      card.replaceChildren(
        h("h1", { text: t("reset.choose") }),
        h("p", { class: "notice success", id: "reset-done", text: t("reset.done") }),
        h("p", {}, link("#/", t("login.submit"), "button primary", "reset-login")));
    }));
  show(card);
}

// --- search (ADR-0033) -------------------------------------------------------

// stars shows how well a hit answers the search, from 1 to 5 (ADR-0072).
function stars(n) {
  return h("span", { class: "stars", role: "img", "aria-label": t("search.stars", { n }), title: t("search.starsHint", { n }) },
    h("span", { class: "stars-on", text: "★".repeat(n) }), h("span", { class: "stars-off", text: "★".repeat(5 - n) }));
}

async function searchView(q) {
  const show = screen();
  // Without a letter or digit there is nothing to search for (the server
  // refuses it): the page asks for a term, as the start page's search
  // button leads here empty.
  const box = h("form", { class: "search search-page", id: "search-page", role: "search", onsubmit: (ev) => {
    ev.preventDefault();
    const typed = new FormData(ev.target).get("q").trim();
    if (typed) location.hash = "#/search/" + encodeURIComponent(typed);
  } }, clearable(h("input", { name: "q", type: "search", value: q, placeholder: t("nav.search"), "aria-label": t("nav.search") })),
  h("button", { type: "submit", class: "primary", text: t("search.go") }));
  // The words lead on to a query of the fields (ADR-0136).
  const toQuery = h("p", { class: "search-to-query" }, link("#/query?" + new URLSearchParams({ q: q.trim() ? `text ~ "${q.trim().replace(/["\\]/g, "\\$&")}"` : "" }),
    t("search.toQuery"), null, "search-to-query"));
  if (!/[\p{L}\p{N}]/u.test(q)) {
    show(h("div", { class: "page-head" }, h("h1", { text: t("search.title") })), box,
      h("p", { class: "muted", id: "search-hint", text: q.trim() ? t("search.needsTerm") : t("search.prompt") }), toQuery);
    box.querySelector("input").focus();
    return;
  }
  // The hits are paged, so the server narrows them (ADR-0098): further
  // words, a space, a type, a state.
  const [{ spaces }, { types }] = await Promise.all([get("/api/spaces"), get("/api/types")]);
  const states = [...new Set(types.flatMap((ty) => ty.workflow?.states || []))].sort();
  const filter = serverFilter({ id: "hits", label: t("search.title"),
    choices: [
      { key: "space", label: t("col.space"), options: favouritesFirst(spaces).map((s) => [s.key, `${s.name} (${s.key})`]) },
      { key: "type", label: t("col.type"), options: types.map((ty) => [ty.name, ty.name]) },
      ...(states.length ? [{ key: "state", label: t("col.state"), options: states.map((s) => [s, s]) }] : []),
    ],
    load: async () => {
      list.replaceChildren();
      const page = await get(url());
      addHits(page);
      total.textContent = t("search.results", { n: page.total.value, q: [q, filter.state.text].filter((x) => x.trim()).join(" ") });
      emptyNow(page.total.value);
      return page.total.value;
    } });
  const url = (after) => {
    const p = new URLSearchParams({ q: [q, filter.state.text].filter((x) => x.trim()).join(" "), limit: 50 });
    for (const [k, v] of Object.entries(filter.state.choices)) if (v) p.set(k, v);
    if (after) p.set("after", after);
    return "/api/search?" + p;
  };
  // Labels that hold what was typed lead to their items (ADR-0092).
  const [first, { labels }] = await Promise.all([get(url()), get("/api/labels?q=" + encodeURIComponent(q)).catch(() => ({ labels: [] }))]);
  const list = h("ol", { class: "list hits" });
  const more = h("div");
  const none = empty(t("search.none"));
  const noneFiltered = h("p", { class: "empty filter-empty", id: "hits-filter-empty", text: t("filter.none") });
  const emptyNow = (n) => {
    list.hidden = n === 0;
    none.hidden = n > 0 || filter.active();
    noneFiltered.hidden = n > 0 || !filter.active();
  };
  const total = h("p", { class: "muted", id: "search-total", text: t("search.results", { n: first.total.value, q }) });
  const addHits = (page) => {
    for (const hit of page.hits) {
      const it = hit.item;
      list.append(h("li", {},
        hit.stars ? stars(hit.stars) : null,
        link("#/item/" + encodeURIComponent(it.key), it.key), " ", h("strong", { text: it.title }),
        " ", h("span", { class: "muted", text: `${it.space} · ${it.type}${it.state ? " · " + it.state : ""}` }),
        it.labels?.length ? [" ", ...it.labels.map((l) => labelChip(l))] : null,
        hit.snippet ? h("p", { class: "snippet", text: hit.comment ? t("search.inComment", { text: hit.snippet }) : hit.snippet }) : null,
        // Which words the hit holds and which it lacks (ADR-0083).
        hit.matched?.length && (hit.missing?.length || hit.matched.length > 1) ? h("p", { class: "hit-words" },
          h("span", { class: "muted", text: t("search.matched") + " " }), ...hit.matched.map((w) => h("span", { class: "chip hit-matched", text: w })),
          hit.missing.length ? [h("span", { class: "muted", text: " " + t("search.missing") + " " }),
            ...hit.missing.map((w) => h("s", { class: "chip hit-missing", text: w }))] : null) : null,
      ));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => addHits(await get(url(page.next))) }));
  };
  addHits(first);
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("search.title") }),
      first.total.value ? h("p", { class: "actions" }, csvLink("/api/search.csv?q=" + encodeURIComponent(q), "search-csv")) : null),
    box,
    toQuery,
    labels.length ? h("p", { class: "search-labels", id: "search-labels" }, h("span", { class: "muted", text: t("search.labels") + " " }),
      labels.slice(0, 12).map((l) => link("#/label/" + encodeURIComponent(l.label), `${l.label} (${l.count})`, "chip"))) : null,
    filter.el,
    total,
    list, none, noneFiltered,
    more,
  );
  emptyNow(first.total.value);
}

// --- query (ADR-0136) ----------------------------------------------------------

// queryExamples are queries to start from; a click puts one in the field.
// The query language is the same in every language of the UI.
const queryExamples = [
  "type = task AND assignee = currentUser() AND finished = false ORDER BY due",
  "updated >= -7d ORDER BY updated DESC",
  'text ~ "release" AND label IN (web, security)',
];

// queryFields are the fields every item has.
const queryFields = ["key", "title", "type", "space", "state", "label", "text", "body", "comment", "parent", "ancestor",
  "created", "updated", "creator", "finished"];

// queryView selects items with the query language (ADR-0136): conditions
// on their fields, and an order. The query is kept in the address, so it
// survives a reload and can be bookmarked and shared (ADR-0098).
async function queryView(params) {
  const show = screen();
  const q = params.get("q") || "";
  const input = h("textarea", { name: "q", id: "query-text", class: "query-text", rows: 3, spellcheck: "false", autocomplete: "off",
    "aria-label": t("query.label"), placeholder: queryExamples[0] });
  input.value = q;
  const box = h("form", { class: "query-form", id: "query-form", onsubmit: (ev) => {
    ev.preventDefault();
    const next = "#/query?" + new URLSearchParams({ q: input.value.trim() });
    if (location.hash === next) route();
    else location.hash = next;
  } }, input, h("p", { class: "actions" },
    h("button", { type: "submit", class: "primary", id: "query-run", text: t("query.run") }),
    h("span", { class: "muted", text: t("query.keys") })));
  // Ctrl+Enter runs the query, as Enter starts a new line.
  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      box.requestSubmit();
    }
  });
  // insert puts a field's name where the cursor is, quoted where it must be.
  const insert = (name) => {
    const text = /^[\p{L}\p{N}_.-]+$/u.test(name) ? name : `"${name.replace(/["\\]/g, "\\$&")}"`;
    input.setRangeText(text, input.selectionStart, input.selectionEnd, "end");
    input.focus();
  };
  const { types } = await get("/api/types");
  const own = [...new Set(types.flatMap((ty) => ty.fields.map((f) => f.name)))].sort((a, b) => a.localeCompare(b));
  const chip = (name, quoted) => h("button", { type: "button", class: "chip query-field", text: name, onclick: () => insert(quoted ? `"${name}"` : name) });
  const help = h("details", { class: "query-help", id: "query-help" },
    h("summary", { text: t("query.help") }),
    h("ul", {}, ["query.helpCombine", "query.helpCompare", "query.helpEmpty", "query.helpTime", "query.helpUser", "query.helpOrder"].map((k) => h("li", { text: t(k) }))),
    h("p", { class: "query-fields" }, h("span", { class: "muted", text: t("query.builtins") + " " }), queryFields.map((f) => chip(f))),
    own.length ? h("p", { class: "query-fields", id: "query-own-fields" }, h("span", { class: "muted", text: t("query.fieldsOfTypes") + " " }),
      // A property named like a field of every item is reached quoted.
      own.map((f) => chip(f, queryFields.includes(f.toLowerCase())))) : null,
    h("p", { class: "muted", text: t("query.examples") }),
    h("ul", { class: "query-examples" }, queryExamples.map((ex) => h("li", {},
      h("button", { type: "button", class: "link", text: ex, onclick: () => { input.value = ex; input.focus(); } })))));
  const head = h("div", { class: "page-head" }, h("h1", { text: t("query.title") }));
  const lead = h("p", { class: "muted", text: t("query.lead") });
  if (!q.trim()) {
    help.open = true;
    show(head, lead, box, help);
    input.focus();
    return;
  }
  const url = (after) => {
    const p = new URLSearchParams({ query: q, limit: 100 });
    if (after) p.set("after", after);
    return "/api/items?" + p;
  };
  let first;
  try {
    first = await get(url());
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 400) throw err;
    help.open = true;
    show(head, lead, box, h("p", { class: "error", role: "alert", id: "query-error", text: err.message }), help);
    // The cursor goes to the mistake. The server counts characters, the
    // field UTF-16 units.
    if (typeof err.at === "number") {
      const at = [...input.value].slice(0, err.at).join("").length;
      input.focus();
      input.setSelectionRange(at, at + 1);
    }
    return;
  }
  const rows = h("tbody");
  const more = h("div");
  const addRows = (page) => {
    for (const it of page.items) {
      rows.append(h("tr", {},
        h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key, "key")),
        h("td", { text: it.title }),
        h("td", { text: it.type }),
        h("td", { text: it.state || "" }),
        h("td", { text: it.space }),
        h("td", {}, (it.labels || []).map((l) => labelChip(l)))));
    }
    more.replaceChildren();
    if (page.next) more.append(h("button", { text: t("common.more"), onclick: async () => addRows(await get(url(page.next))) }));
  };
  addRows(first);
  const n = first.total.value;
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("query.title") }),
      n ? h("p", { class: "actions" }, csvLink("/api/items.csv?" + new URLSearchParams({ query: q }), "query-csv")) : null),
    lead, box, help,
    h("p", { class: "muted", id: "query-total", role: "status", text: t("query.results", { n }) }),
    n ? h("table", { id: "query-results" }, h("thead", {}, h("tr", {},
      [t("col.key"), t("col.title"), t("col.type"), t("col.state"), t("col.space"), t("tasks.labels")].map((c) => h("th", { text: c })))), rows)
      : empty(t("query.none"), "query-none"),
    more,
  );
}

// myNotesView lists one's personal notes on every item one may still
// read, newest first, each with its passage and a link to its item
// (ADR-0113). Nobody else sees them (ADR-0107).
async function myNotesView() {
  const show = screen();
  const { notes } = await get("/api/notes");
  const list = h("ul", { class: "list my-notes", id: "my-notes" }, [...notes].reverse().map((n) => h("li", { "data-f-item": n.key },
    h("p", {}, link("#/item/" + encodeURIComponent(n.key), n.key), " ", h("strong", { text: n.title }),
      h("span", { class: "muted", text: " · " + when(n.updated || n.created) })),
    n.anchor?.quote ? h("blockquote", { class: "my-note-quote", text: n.anchor.quote }) : null,
    h("p", { class: "my-note-text", text: n.text }))));
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("note.title") })),
    h("p", { class: "muted", text: t("note.private") }),
    notes.length ? filtered(list, { id: "mynotes", label: t("note.title") }) : empty(t("note.noneAnywhere"), "my-notes-empty"),
  );
}

// --- spaces ------------------------------------------------------------------

// roleText names a space role for people.
function roleText(role) {
  switch (role) {
    case "viewer": return t("role.viewer");
    case "editor": return t("role.editor");
    case "space-admin": return t("role.space-admin");
    default: return role || "";
  }
}

// mayEdit says whether a role lets one create and change pages.
const mayEdit = (role) => role === "editor" || role === "space-admin";

async function homeView() {
  const show = screen();
  const [{ spaces }, { types }, notes, brand, { requests }, mine, storage] = await Promise.all([get("/api/spaces"), get("/api/types"),
    get("/api/notifications?limit=1").catch(() => ({ unread: 0 })), get("/branding.json").catch(() => ({})),
    me.admin ? get("/api/account-requests").catch(() => ({ requests: [] })) : { requests: [] },
    get("/api/notes").catch(() => ({ notes: [] })),
    me.admin ? get("/api/storage").catch(() => null) : null]);
  if (me.admin) setRequests(requests.length);
  const anyEditable = spaces.some((s) => mayEdit(s.role));
  // A greeting by the time of day, the reader's own clock.
  const hour = new Date().getHours();
  const greeting = hour < 11 ? t("home.morning") : hour < 18 ? t("home.day") : t("home.evening");
  const name = brand.name || "wikiwikibangbang";
  const quick = (href, title, text, id, extra) => h("a", { class: "home-tile", href, id }, h("strong", { text: title }), h("span", { class: "muted", text }), extra || null);
  // How much the wiki holds that one may read: pages, tasks (open and
  // done) and pictures in the album. Counted after the page is shown.
  const stats = h("p", { class: "home-stats", id: "home-stats", "aria-live": "polite" });
  const count = (total) => (total.exact ? "" : "≥ ") + total.value;
  const totalOf = (url) => get(url).then((r) => r.total).catch(() => null);
  Promise.all([
    totalOf("/api/items?type=page&limit=1"),
    Promise.all(types.filter(isTaskType).map((ty) => totalOf(`/api/items?type=${encodeURIComponent(ty.name)}&limit=1`))),
    get("/api/album?limit=1").then((r) => ({ exact: true, value: r.total })).catch(() => null),
  ]).then(([pages, tasks, album]) => {
    tasks = tasks.filter(Boolean);
    const sum = tasks.length ? { exact: tasks.every((x) => x.exact), value: tasks.reduce((n, x) => n + x.value, 0) } : { exact: true, value: 0 };
    const stat = (id, href, total, key) => h(href ? "a" : "span", { class: "home-stat", href, id },
      h("strong", { text: count(total) }), " ", h("span", { text: t(key, { n: total.value }) }));
    stats.replaceChildren(...[
      pages && stat("home-pages", null, pages, "home.statPages"),
      stat("home-task-count", "#/tasks/all", sum, "home.statTasks"),
      album && stat("home-album-count", "#/album", album, "home.statAlbum"),
    ].filter(Boolean));
  });
  const findings = notes.findings ? notes.findings.sensitive + notes.findings.stale + notes.findings.implausible + (notes.findings.personal || 0) + (notes.findings.reviews || 0) : 0;
  show(
    h("section", { class: "welcome", id: "welcome" },
      h("div", { class: "welcome-text" },
        h("h1", { text: `${greeting}, ${me.user.username}` }),
        h("p", { class: "welcome-lead", text: t("home.lead", { name }) }),
        stats,
        h("p", { class: "actions-row" },
          anyEditable ? link("#/new", t("home.newPage"), "button primary") : null,
          link("#/tasks", t("home.myTasks"), "button"))),
      // The design's own picture replaces the product's (ADR-0095).
      brand.assets?.welcome ? ownWelcome(brand) : h("img", { class: "welcome-art", id: "welcome-art", src: welcomeArt, alt: "", width: 480, height: 320 }),
      // The installed version, small, for support questions and releases.
      me.version ? h("span", { class: "welcome-version", id: "home-version", title: me.revision ? t("home.revision", { revision: me.revision }) : "", text: t("home.version", { version: me.version }) }) : ""),
    // Account requests wait on an administrator (ADR-0061): said first,
    // and decided under the tasks.
    requests.length ? h("a", { class: "notice requests-notice", href: "#/tasks", id: "home-requests" },
      h("strong", { text: t("requests.waiting", { n: requests.length }) }), " ", t("requests.homeHint")) : "",
    // A storage getting full is said to administrators first (ADR-0150).
    storage && storage.level !== "ok" ? h("a", { class: "notice storage-notice storage-" + storage.level, href: "#/admin", id: "home-storage" },
      h("strong", { text: t("storage.homeNotice", { level: storageLevel(storage.level) }) }), " ",
      t("storage.free", { free: sizeText(storage.free), total: sizeText(storage.total), share: storage.total ? Math.round((storage.free * 1000) / storage.total) / 10 : "?" })) : "",
    h("section", { class: "home-tiles", "aria-label": t("home.quick") },
      quick("#/tasks", t("nav.tasks"), t("home.tasksText"), "home-tasks",
        requests.length ? h("span", { class: "chip", text: t("nav.requestsWaiting", { n: requests.length }) }) : null),
      quick("#/notifications", t("nav.notifications"), notes.unread ? t("home.notesUnread", { n: notes.unread }) : t("home.notesNone"), "home-notes",
        findings ? h("span", { class: "chip", text: t("nav.findingsOpen", { n: findings }) }) : null),
      // One's personal notes, wherever they were made (ADR-0113).
      quick("#/my-notes", t("note.title"), mine.notes.length ? t("home.myNotesText", { n: mine.notes.length }) : t("home.myNotesNone"), "home-my-notes"),
      quick("#/album", t("nav.album"), t("home.albumText"), "home-album"),
      quick("#/mcp", t("nav.mcp"), t("home.mcpText"), "home-mcp")),
    h("h2", { text: t("spaces.title") }),
    spaces.length === 0
      ? h("div", { class: "card first-steps" },
        h("p", { text: me.admin ? t("spaces.noneAdmin") : t("spaces.none") }),
        me.admin ? h("ol", {}, h("li", { text: t("home.step1") }), h("li", { text: t("home.step2") }), h("li", { text: t("home.step3") })) : null)
      : spacesOverview(spaces),
    me.admin ? setupCard(types) : null,
    me.admin ? newSpaceForm(spaces.length === 0) : null,
  );
}

// favouritesFirst orders spaces with one's favourites first, then the
// others, each group in its order, as the start page and every choice of a
// space show them (ADR-0145).
function favouritesFirst(spaces) {
  return [...spaces.filter((s) => s.favourite), ...spaces.filter((s) => !s.favourite)];
}

// spaceIcon shows a space's icon or logo (ADR-0062), or its key when it
// has none.
function spaceIcon(s, size) {
  if (!s.icon) return h("span", { class: `space-icon space-icon-${size} space-icon-key`, "aria-hidden": "true", text: s.key.slice(0, 3) });
  return h("img", { class: `space-icon space-icon-${size}`, src: `/api/spaces/${encodeURIComponent(s.key)}/icon?v=${encodeURIComponent(s.icon)}`, alt: "", width: size, height: size });
}

// spacesLayout is how the start page shows the spaces, as tiles or as a
// list; each reader's own choice, kept in the browser.
const spacesLayout = {
  get() { try { return localStorage.getItem("wwbb.spaces") === "list" ? "list" : "tiles"; } catch { return "tiles"; } },
  set(v) { try { localStorage.setItem("wwbb.spaces", v); } catch { /* the choice lasts for this page only */ } },
};

// spacesOverview shows the spaces as tiles or as a list, with a switch.
// The spaces one marked as favourites come first, in both (ADR-0145); a
// star on each tile and row marks and unmarks them.
function spacesOverview(spaces) {
  const box = h("div", { id: "spaces-overview" });
  const draw = (layout) => {
    const toggle = (value, label) => h("button", { type: "button", class: "segment", "aria-pressed": String(layout === value), id: "spaces-" + value, text: label,
      onclick: () => { spacesLayout.set(value); draw(value); } });
    const href = (s) => "#/space/" + encodeURIComponent(s.key);
    const star = (s) => h("button", { type: "button", class: "favourite-star", "data-space": s.key, "aria-pressed": String(Boolean(s.favourite)),
      title: s.favourite ? t("spaces.unfavourite") : t("spaces.favourite"), "aria-label": `${s.favourite ? t("spaces.unfavourite") : t("spaces.favourite")}: ${s.name}`,
      text: s.favourite ? "★" : "☆",
      onclick: async () => {
        const path = `/api/spaces/${encodeURIComponent(s.key)}/favourite`;
        try {
          if (s.favourite) await del(path); else await put(path);
        } catch (err) {
          return alert(err.message);
        }
        // Drawn again in place, the star keeps the focus where it went.
        s.favourite = !s.favourite;
        draw(layout);
        box.querySelector(`.favourite-star[data-space="${CSS.escape(s.key)}"]`)?.focus();
      } });
    const ordered = [...spaces.filter((s) => s.favourite), ...spaces.filter((s) => !s.favourite)];
    const favourite = (s) => (s.favourite ? t("spaces.favouriteYes") : t("spaces.favouriteNo"));
    box.replaceChildren(
      h("div", { class: "segmented", role: "group", "aria-label": t("home.layout") }, toggle("tiles", t("home.tiles")), toggle("list", t("home.list"))),
      filtered(layout === "list"
        ? h("table", { class: "spaces space-list" },
          h("tr", {}, h("th", { class: "favourite-col", "aria-label": t("spaces.favouriteCol") }), h("th", { text: t("field.name") }), h("th", { text: t("admin.key") }), h("th", { text: t("admin.role") })),
          ordered.map((s) => h("tr", { "data-f-role": roleText(s.role), "data-f-favourite": favourite(s), class: s.favourite ? "favourite" : null },
            h("td", { class: "favourite-col" }, star(s)),
            h("td", {}, h("a", { href: href(s), class: "space-link" }, spaceIcon(s, 24), h("span", { text: s.name }))),
            h("td", { class: "muted", text: s.key }),
            h("td", {}, h("span", { class: "badge", text: roleText(s.role) })))))
        : h("ul", { class: "space-tiles spaces" }, ordered.map((s) => h("li", { "data-f-role": roleText(s.role), "data-f-favourite": favourite(s), class: s.favourite ? "favourite" : null },
          h("a", { href: href(s), class: "space-tile" },
            h("span", { class: "space-tile-head" }, h("span", { class: "space-key", text: s.key }), s.icon ? spaceIcon(s, 48) : ""),
            h("strong", { text: s.name }),
            h("span", { class: "badge", text: roleText(s.role) })),
          star(s)))),
      { id: "spaces", label: t("spaces.title"), choices: [{ key: "role", label: t("admin.role") }, { key: "favourite", label: t("spaces.favouriteCol") }] }));
  };
  draw(spacesLayout.get());
  return box;
}

// spaceNameCard renames a space; its key, part of every item's key, stays
// (ADR-0086).
function spaceNameCard(sp, key) {
  return form({ class: "card", id: "space-name" }, [
    h("h2", { text: t("space.name") }),
    h("p", { class: "muted", text: t("space.nameHelp", { key }) }),
    field(t("field.name"), h("input", { name: "name", value: sp.name || "", required: true, maxlength: 80 })),
    h("button", { type: "submit", text: t("common.save") }),
  ], async (d, st) => {
    await put(`/api/spaces/${encodeURIComponent(key)}/name`, { name: d.get("name") });
    await route();
  });
}

// spaceIconCard sets or removes a space's icon (space administrators).
function spaceIconCard(sp, status) {
  const picker = h("input", { type: "file", id: "space-icon-file", accept: "image/png,image/jpeg,image/gif,image/webp,image/svg+xml", "aria-label": t("space.iconChoose") });
  const path = `/api/spaces/${encodeURIComponent(sp.key)}/icon`;
  picker.addEventListener("change", () => action(async () => {
    const file = picker.files[0];
    if (!file) return;
    const res = await fetch(path, { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: file });
    if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
  }, status));
  return h("section", { class: "card", id: "space-icon" },
    h("h2", { text: t("space.icon") }),
    h("div", { class: "avatar-row" }, spaceIcon(sp, 64),
      h("div", {},
        h("p", { class: "muted", text: t("space.iconHelp") }),
        h("label", { class: "upload" }, h("span", { text: t("space.iconChoose") }), picker),
        sp.icon ? h("button", { type: "button", class: "link danger", text: t("common.delete"), onclick: () => action(() => del(path), status) }) : "")));
}

// newSpaceForm creates a space and opens it (administrators). It starts
// folded unless open is set.
function newSpaceForm(open) {
  const f = form({ id: "create-space" }, [
    field(t("admin.key"), h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*", placeholder: "DOCS", title: t("admin.keyHint") })),
    field(t("field.name"), h("input", { name: "name", required: true })),
    h("button", { type: "submit", class: "primary", text: t("admin.createSpace") }),
  ], async (d) => {
    await post("/api/spaces", { key: d.get("key"), name: d.get("name") });
    location.hash = "#/space/" + encodeURIComponent(d.get("key"));
  });
  return h("details", { class: "card", id: "new-space-card", open: open || null }, h("summary", { text: t("admin.newSpace") }), f);
}

// typeTemplates are ready-made types for a new installation: types are
// data (ADR-0001), so an empty server has none until someone defines them.
const typeTemplates = [
  { name: "page", body: true, fields: [{ name: "owner", kind: "user" }] },
  { name: "task", body: true,
    fields: [{ name: "assignee", kind: "user" }, { name: "due", kind: "date" }, { name: "priority", kind: "enum", values: ["low", "normal", "high"] }],
    workflow: { initial: "open", states: ["open", "in progress", "done"],
      transitions: [{ name: "start", from: ["open"], to: "in progress" }, { name: "finish", from: ["open", "in progress"], to: "done" },
        { name: "reopen", from: ["done"], to: "open" }] } },
  { name: "decision", body: true, fields: [{ name: "assignee", kind: "user" }, { name: "decided", kind: "date" }],
    workflow: { initial: "proposed", states: ["proposed", "accepted", "rejected"],
      transitions: [{ name: "accept", from: ["proposed"], to: "accepted" }, { name: "reject", from: ["proposed"], to: "rejected" }] } },
];

// setupCard offers administrators the ready-made types that are missing,
// and the fields a ready-made type gained since it was defined here, such
// as the assignee of a decision; the fields are added as a new version of
// the type, everything else of it stays.
function setupCard(types) {
  const missing = typeTemplates.filter((tt) => !types.some((ty) => ty.name === tt.name));
  const outdated = typeTemplates.flatMap((tt) => {
    const ty = types.find((x) => x.name === tt.name);
    const add = ty ? tt.fields.filter((f) => !ty.fields.some((x) => x.name === f.name)) : [];
    return add.length ? [{ ty, add }] : [];
  });
  if (missing.length === 0 && outdated.length === 0) return null;
  const status = h("div", { class: "status" });
  const fieldsText = outdated.map(({ ty, add }) => `${ty.name} (${add.map((f) => f.name).join(", ")})`).join("; ");
  return h("section", { class: "card notice-card", id: "setup-types" },
    h("h2", { text: t("setup.title") }),
    missing.length ? h("p", { text: types.length ? t("setup.someTypes", { list: missing.map((m) => m.name).join(", ") }) : t("setup.noTypes") }) : null,
    missing.length ? h("button", { class: "primary", id: "setup-create", text: t("setup.create", { list: missing.map((m) => m.name).join(", ") }),
      onclick: () => action(async () => { for (const tt of missing) await post("/api/types", tt); }, status) }) : null,
    outdated.length ? h("p", { text: t("setup.newFields", { list: fieldsText }) }) : null,
    outdated.length ? h("button", { class: missing.length ? "secondary" : "primary", id: "setup-fields", text: t("setup.addFields"),
      onclick: () => action(async () => {
        for (const { ty, add } of outdated) {
          const { name, body, fields, workflow, review } = ty;
          await post("/api/types", { name, body, workflow, review, fields: [...fields, ...add] });
        }
      }, status) }) : null,
    status);
}

// newView creates a page or another item, and for administrators a space:
// one place that lists what can be created (#/new?space=X&parent=Y).
async function newView(params) {
  const show = screen();
  const [{ spaces }, { types }] = await Promise.all([get("/api/spaces"), get("/api/types")]);
  // One's favourite spaces come first, and without a space in the address
  // the first of them is chosen (ADR-0145).
  const editable = favouritesFirst(spaces.filter((s) => mayEdit(s.role)));
  let parent = null;
  if (params.get("parent")) parent = (await get("/api/items/" + encodeURIComponent(params.get("parent")))).item;
  const space = editable.some((s) => s.key === params.get("space")) ? params.get("space") : editable[0]?.key;
  here.space = space || "";
  // With a type, as from "New task", only its form is shown.
  const only = types.find((ty) => ty.name === params.get("type"));
  if (only && editable.length) {
    show(
      h("h1", { text: only.workflow ? t("tasks.new") : t("new.title") }),
      newItemForm({ space, spaces: editable, types, type: only.name, heading: only.name }),
      h("p", { class: "muted" }, link(newHref(space, parent?.id || ""), t("new.otherTypes"))));
    document.querySelector("#new-item [name=title]")?.focus();
    return;
  }
  let pageCard;
  if (types.length === 0) {
    pageCard = h("p", { class: "muted", text: me.admin ? t("new.noTypesAdmin") : t("new.noTypes") });
  } else if (editable.length === 0) {
    pageCard = h("p", { class: "muted", text: me.admin ? t("new.noSpaceAdmin") : t("new.noSpace") });
  } else {
    pageCard = newItemForm({ space, spaces: editable, types, parent: parent && parent.space === space ? parent : null, heading: t("new.page") });
  }
  show(
    h("h1", { text: t("new.title") }),
    pageCard,
    h("section", { class: "card" }, h("h2", { text: t("new.withAssistant") }), h("p", { text: t("new.withAssistantText") }),
      link("#/mcp", t("new.withAssistantLink"), "button secondary")),
    me.admin ? setupCard(types) : null,
    me.admin ? newSpaceForm(spaces.length === 0) : null,
    me.admin ? h("p", { class: "muted" }, t("new.moreAdmin"), " ", link("#/admin", t("nav.admin"))) : null,
  );
}

async function spaceView(key) {
  const show = screen();
  // The space's items are paged, so the server filters them (ADR-0098).
  const itemsURL = (state, after) => {
    const p = filterParams(new URLSearchParams({ space: key, limit: 100 }), state);
    if (after) p.set("after", after);
    return "/api/items?" + p;
  };
  const [{ types }, top, { labels }, { watches }, { spaces }] = await Promise.all([
    get("/api/types"),
    get(`/api/spaces/${encodeURIComponent(key)}/tree?limit=1`),
    get(`/api/labels?space=${encodeURIComponent(key)}`),
    get("/api/me/watches"),
    get("/api/spaces"),
  ]);
  here.space = key;
  const sp = spaces.find((s) => s.key === key) || { key, name: key, role: "" };
  const canEdit = mayEdit(sp.role);
  const status = h("div", { class: "status" });
  const create = canEdit ? newItemForm({ space: key, types }) : null;
  // "New page" leads to the form at the end of the view.
  const toForm = () => { create.scrollIntoView({ block: "start" }); create.querySelector("[name=title]").focus(); };
  // The space's home is its first page at the top of the tree.
  let home = h("section", { class: "card empty" }, h("p", { class: "muted", text: t("space.noPages") }),
    create ? h("button", { class: "primary", text: t("space.firstPage"), onclick: toForm }) : null);
  if (top.items.length) {
    const [{ item, editable }, { embeds }] = await Promise.all([get("/api/items/" + top.items[0].id), get(`/api/items/${top.items[0].id}/embeds`)]);
    // The home page is edited from here too, in the bar where its text
    // begins, as on its own page (ADR-0091).
    const edit = editable && item.presence === "live" && item.readsAllSecrets !== false
      ? link("#/item/" + encodeURIComponent(item.key) + "?edit", t("item.edit"), "button primary", "home-edit") : null;
    home = h("section", { id: "home", class: "card" },
      h("h2", {}, link("#/item/" + encodeURIComponent(item.key), item.title || item.key)),
      item.body.length ? readOnlyBody(item, embeds, edit)
        : [edit ? h("div", { class: "pm-toolbar pm-readbar", role: "toolbar" }, edit) : null, h("p", { class: "muted", text: t("space.homeEmpty") })]);
  }
  const states = [...new Set(types.flatMap((ty) => ty.workflow?.states || []))].sort();
  const filter = serverFilter({ id: "items", label: t("space.allItems"),
    choices: [
      { key: "type", label: t("col.type"), options: types.map((ty) => [ty.name, ty.name]) },
      ...(states.length ? [{ key: "state", label: t("col.state"), options: states.map((s) => [s, s]) }] : []),
      ...(labels.length ? [{ key: "label", label: t("label.title"), options: labels.map((l) => [l.label, l.label]) }] : []),
    ],
    load: async (state) => {
      rows.replaceChildren();
      const page = await get(itemsURL(state));
      addRows(page);
      shownCount(page.total.value);
      return page.total.value;
    } });
  const first = await get(itemsURL(filter.state));
  const rows = h("tbody");
  const more = h("div");
  const count = h("p", { class: "muted", id: "space-count" });
  const csv = csvLink("", "space-csv");
  const noneFiltered = h("p", { class: "empty filter-empty", id: "items-filter-empty", text: t("filter.none") });
  const table = h("table", {}, h("thead", {}, h("tr", {}, [t("col.key"), t("col.title"), t("col.type"), t("col.state")].map((c) => h("th", { text: c })))), rows);
  const shownCount = (n) => {
    count.textContent = t("space.items", { n });
    table.hidden = n === 0 && filter.active();
    noneFiltered.hidden = n > 0 || !filter.active();
    csv.href = "/api/items.csv?" + filterParams(new URLSearchParams({ space: key }), filter.state);
  };
  const addRows = (page) => {
    for (const it of page.items) {
      rows.append(h("tr", {},
        h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key, "key")),
        h("td", { text: it.title }),
        h("td", { text: it.type }),
        h("td", { text: it.state || "" }),
      ));
    }
    more.replaceChildren();
    if (page.next) {
      more.append(h("button", { text: t("common.more"), onclick: async () => addRows(await get(itemsURL(filter.state, page.next))) }));
    }
  };
  addRows(first);
  const boards = types.filter((ty) => ty.workflow)
    .map((ty) => link(`#/board/${encodeURIComponent(key)}/${encodeURIComponent(ty.name)}`, t("space.board", { type: ty.name })));
  const tree = pageTree({ space: key, onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  show(withTree(tree,
    h("p", {}, link("#/", t("nav.spaces")), " / " + key),
    h("div", { class: "page-head" },
      h("h1", { text: sp.name }),
      // The view's buttons, and the bell always last (ADR-0112).
      h("p", { class: "actions" },
        create && types.length ? h("button", { class: "primary", id: "space-new-page", text: t("space.newPage"), onclick: toForm }) : null,
        csv,
        watchButton("/api/spaces/" + encodeURIComponent(key), watches.some((w) => !w.item && w.space === key), status))),
    h("nav", { class: "subnav", "aria-label": t("space.views") }, [...boards, link(`#/links/${encodeURIComponent(key)}`, t("space.linkCheck")), link(`#/governance/${encodeURIComponent(key)}`, t("nav.governance")),
      sp.role === "space-admin" ? link(`#/members/${encodeURIComponent(key)}`, t("members.title")) : null].filter(Boolean)),
    status,
    home,
    labels.length ? h("section", { id: "space-labels" }, h("h2", { text: t("label.title") }),
      h("div", { class: "labels" }, labels.map((l) => h("span", { class: "label-count" }, labelChip(l.label), " " + l.count)))) : null,
    h("h2", { text: t("space.allItems") }),
    filter.el,
    count,
    table, noneFiltered,
    more,
    create,
  ));
  shownCount(first.total.value);
}

// --- board -------------------------------------------------------------------

// boardMax bounds the cards one board loads; a larger board says so.
const boardMax = 2000;

// boardView shows the items of one type in a space as a board (ADR-0036),
// with the cards, filter and details of a kanban (ADR-0139). It keeps
// nothing: a card moves by the one transition that leads to its column,
// and colour and lanes are chosen for the view, in its address.
async function boardView(key, typeName) {
  const show = screen();
  const at = location.hash.split("?")[0];
  const [{ types }, { spaces }] = await Promise.all([get("/api/types"), get("/api/spaces")]);
  const type = types.find((ty) => ty.name === typeName);
  if (!type || !type.workflow) throw new Error(t("board.noWorkflow", { type: typeName }));
  const found = await readAll(`/api/items?space=${encodeURIComponent(key)}&type=${encodeURIComponent(typeName)}&limit=100`);
  const canEdit = mayEdit(spaces.find((sp) => sp.key === key)?.role);
  const status = h("div", { class: "status" });
  const view = viewOptions([type], null, `board.${key}/${typeName}`, at);
  const layout = await cardBoard({
    id: "board", status, label: "", mayEdit: () => canEdit, cardFields: viewCardFields,
    sections: [viewSection(type, found.items, status, canEdit ? { space: key } : null, `board.${key}/${typeName}`)],
    ...view.look,
  });
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("board.crumb", { type: typeName })),
    h("div", { class: "page-head" }, h("h1", { text: `${key} · ${typeName}` }),
      h("p", { class: "actions" }, csvLink(`/api/items.csv?space=${encodeURIComponent(key)}&type=${encodeURIComponent(typeName)}`, "board-csv"))),
    found.truncated ? h("p", { class: "notice warning", text: t("board.truncated", { n: found.items.length }) }) : null,
    view.el,
    status,
    layout,
  );
}

// viewCardFields are the fields the cards of a board that keeps nothing
// show (ADR-0139): who has them, their dates and their choices, as on a
// new kanban (ADR-0132), and their labels.
function viewCardFields(type) {
  return [...type.fields.filter((f) => ["user", "date", "enum"].includes(f.kind)).map((f) => f.name).slice(0, 12), "labels"];
}

// viewSection is the board of one type on a board that keeps nothing
// (ADR-0036, ADR-0087, ADR-0139): a column per state, its cards in the
// order of the listing. A card moves by the one transition that leads to
// the column; where several do, one is chosen in its details. A card is
// made in the first state, or in a state one transition away, and is
// moved there by it. make names the space new cards are made in and the
// label they carry; without it no card is made here. keep names where
// the folded columns and lanes are kept.
function viewSection(type, items, status, make, keep, heading) {
  const wf = type.workflow;
  const ways = (from, to) => wf.transitions.filter((tr) => tr.to === to && tr.from.includes(from));
  const transition = (it, name) => post(`/api/items/${it.id}/transitions`, { name });
  return {
    type, items, heading, foldKey: `wwbb.${keep}.folded`, laneKey: `wwbb.${keep}.lanes`,
    columns: wf.states.map((state) => ({ id: state, name: state, state, cards: items.filter((it) => it.state === state).map((it) => it.id) })),
    place: (it, col) => {
      const to = ways(it.state, col.state);
      if (to.length === 1) return action(() => transition(it, to[0].name), status);
      if (to.length) return status.replaceChildren(h("p", { class: "error", text: t("board.several", { from: it.state, to: col.state }) }));
      moveRefused(status, new Error(t("board.none", { from: it.state, to: col.state })), type, it, col.state,
        (now) => transition(it, now.workflow.transitions.find((tr) => tr.to === col.state && tr.from.includes(it.state)).name));
    },
    takes: (col) => Boolean(make) && (col.state === wf.initial || ways(wf.initial, col.state).length === 1),
    create: async (col, title, properties) => {
      const { item } = await post("/api/items", { space: make.space, type: type.name, title, properties });
      if (make.label) await put(`/api/items/${item.id}/labels`, { labels: [make.label] });
      const [way] = col.state === item.state ? [] : ways(item.state, col.state);
      if (way) await transition(item, way.name);
    },
  };
}

// viewOptions are the colour and the lanes of a board that keeps nothing
// (ADR-0139): fields of the task chosen for the view and kept in its
// address, like its filter, so a reload and a shared link show the same.
// One's own choice is also remembered in this browser under keep and
// applies where the address names none (ADR-0141); a shared link shows
// what it names without changing what one chose. A field is offered by
// its name, for every type that has it; a colour is the tone of its
// value's place. lead comes first in the row; home is the view's route
// as the address named it when the view began.
function viewOptions(types, lead, keep, home) {
  const [at, query = ""] = location.hash.split("?");
  const q = new URLSearchParams(query);
  const key = `wwbb.${keep}.view`;
  let mine = {};
  try { mine = JSON.parse(localStorage.getItem(key) || "{}") || {}; } catch { mine = {}; }
  const named = (kinds) => [...new Set(types.flatMap((ty) => ty.fields.filter((f) => kinds.includes(f.kind)).map((f) => f.name)))].sort();
  const enums = named(["enum"]), laneFields = named(["user", "enum"]);
  const chosen = (name, names) => {
    const v = q.has(name) ? q.get(name) : mine[name];
    return names.includes(v) ? v : "";
  };
  const colorField = chosen("color", enums), laneField = chosen("lanes", laneFields);
  // What the browser remembers shows in the address too, so a link
  // copied from here shows the same, unless one has gone on meanwhile.
  if (at === home && ((colorField && !q.has("color")) || (laneField && !q.has("lanes")))) {
    if (colorField) q.set("color", colorField);
    if (laneField) q.set("lanes", laneField);
    history.replaceState(null, "", at + "?" + q.toString());
  }
  const colorValues = [...new Set(types.flatMap((ty) => ty.fields.find((f) => f.name === colorField && f.kind === "enum")?.values || []))];
  const set = (name) => (ev) => {
    try { localStorage.setItem(key, JSON.stringify({ ...mine, [name]: ev.target.value })); } catch { /* kept in the address only */ }
    const [now, rest = ""] = location.hash.split("?");
    const p = new URLSearchParams(rest);
    if (ev.target.value) p.set(name, ev.target.value); else p.delete(name);
    const left = p.toString();
    history.replaceState(null, "", now + (left ? "?" + left : ""));
    route();
  };
  const pick = (id, name, none, names, value) => h("select", { id, onchange: set(name) },
    h("option", { value: "", text: none }), names.map((n) => h("option", { value: n, text: n, selected: n === value })));
  const el = lead || enums.length || laneFields.length ? h("div", { class: "board-view", id: "board-view", role: "group", "aria-label": t("board.view"), title: t("board.viewHint") },
    lead || null,
    enums.length ? field(t("kanban.colors"), pick("board-color", "color", t("kanban.colorNone"), enums, colorField)) : null,
    laneFields.length ? field(t("kanban.lanes"), pick("board-lanes", "lanes", t("kanban.lanesNone"), laneFields, laneField)) : null) : null;
  return { el, look: { colorField, colorValues, laneField, toneOf: (v) => kanbanTones[Math.max(0, colorValues.indexOf(v)) % kanbanTones.length] } };
}

// --- tasks -------------------------------------------------------------------

// tasksView lists open work across every space one may read: items of a
// type with a workflow that are not in its last state. It reads the paged
// item listing, at most boardMax items, like the board.
// taskFields finds the fields of a type that make it a task in the inbox
// (ADR-0049), by convention: the assignee is the user field named assignee
// or, failing that, the first user field; the due date the first date
// field named like a deadline (as governance reads it, ADR-0040); the
// priority an enum named priority, its values ordered from low to high.
// taskType is the type "New task" creates: "task" when it has a workflow,
// else the first type with one.
function taskType(types) {
  const flows = types.filter(isTaskType);
  return (flows.find((ty) => ty.name === "task") || flows[0])?.name || "";
}

// finished tells whether an item in state is done with (ADR-0082): the
// workflow's last state, or a state no transition leaves, as the accepted
// of a decision.
function finished(wf, state) {
  return state === wf.states[wf.states.length - 1] || (wf.states.includes(state) && !wf.transitions.some((tr) => tr.from.includes(state)));
}

// isTaskType tells the types whose items are work to do (ADR-0049): a
// workflow, and the name task or a field for whom it is assigned to or
// when it is due. A catalog entry has a workflow and an owner, but is no
// task (ADR-0080).
function isTaskType(ty) {
  if (!ty.workflow) return false;
  const f = taskFields(ty);
  return ty.name === "task" || Boolean(f.due) || ty.fields.some((x) => x.kind === "user" && /^(assignee|zugewiesen|bearbeiter)/i.test(x.name));
}

function taskFields(type) {
  const users = type.fields.filter((f) => f.kind === "user");
  return {
    assignee: users.find((f) => /^(assignee|zugewiesen|bearbeiter)/i.test(f.name)) || users[0] || null,
    due: type.fields.find((f) => f.kind === "date" && /due|deadline|f(ä|ae)llig/i.test(f.name)) || null,
    priority: type.fields.find((f) => f.kind === "enum" && /^(priority|priorität|prioritaet)$/i.test(f.name)) || null,
  };
}

// tasksView is the inbox of open work across spaces (ADR-0049): the items
// whose type has a workflow and that are not in its last state, in three
// lists — assigned to me, unassigned, all — overdue and urgent first. One
// takes a task over or gives it back, and moves it on by its transitions.
// requestsCard shows administrators the account requests waiting for a
// decision (ADR-0061) among their open work, to approve or reject there.
// The requests are not items: they belong to no space and only
// administrators may see them, so they are read from their own list.
async function requestsCard() {
  if (!me.admin) return "";
  const { requests } = await get("/api/account-requests").catch(() => ({ requests: [] }));
  setRequests(requests.length);
  if (!requests.length) return "";
  const status = h("div", { class: "status" });
  const decide = (fn) => async () => {
    try {
      if (await fn() !== false) route();
    } catch (err) {
      status.replaceChildren(errorBox(err));
    }
  };
  return h("section", { class: "card requests-card", id: "tasks-requests" },
    h("h2", { text: t("requests.waiting", { n: requests.length }) }),
    h("p", { class: "muted", text: t("requests.waitingHelp") }),
    filtered(h("table", {},
      h("thead", {}, h("tr", {}, [t("field.username"), t("contacts.email"), t("signup.reason"), t("requests.when"), ""].map((c) => h("th", { text: c })))),
      h("tbody", {}, requests.map((rq) => h("tr", { "data-username": rq.username },
        h("td", {}, h("strong", { text: rq.username })), h("td", { text: rq.email }), h("td", { class: "muted", text: rq.reason }),
        h("td", { class: "muted", text: when(rq.created) }),
        h("td", { class: "task-actions" },
          h("button", { type: "button", class: "approve", text: t("requests.approve"), onclick: decide(() => post(`/api/account-requests/${rq.id}/approve`)) }), " · ",
          h("button", { type: "button", class: "link", text: t("requests.reject"),
            onclick: decide(() => confirm(t("requests.rejectConfirm", { name: rq.username })) ? del(`/api/account-requests/${rq.id}`) : false) })))))),
    { id: "task-requests", label: t("requests.title") }),
    status);
}

async function tasksView(tab, params) {
  const show = screen();
  const [{ types }, { spaces }, requests, { kanbans }] = await Promise.all([get("/api/types"), get("/api/spaces"), requestsCard(), get("/api/kanbans").catch(() => ({ kanbans: [] }))]);
  // A label that is a kanban's links to its board: a task on a kanban is
  // in the list as well (ADR-0060).
  const kanbanOf = new Map(kanbans.map((k) => [`${k.space}\n${k.label}`, k]));
  const typeOf = new Map(types.filter(isTaskType).map((ty) => [ty.name, { ty, f: taskFields(ty) }]));
  // Only the task types are read, each by itself: a wiki with thousands of
  // catalog entries reads its tasks, not everything (ADR-0081).
  const read = await Promise.all([...typeOf.keys()].map((name) => readAll(`/api/items?type=${encodeURIComponent(name)}&limit=100`)));
  const items = read.flatMap((r) => r.items);
  const page = { next: read.some((r) => r.truncated) };
  const open = items.filter((it) => {
    const wf = typeOf.get(it.type)?.ty.workflow;
    return wf && !finished(wf, it.state);
  });
  const assigneeOf = (it) => {
    const f = typeOf.get(it.type).f.assignee;
    return f ? it.properties[f.name] || "" : null;
  };
  await learnNames(open.map(assigneeOf).filter(Boolean));
  const today = new Date().toISOString().slice(0, 10);
  const dueOf = (it) => {
    const f = typeOf.get(it.type).f.due;
    return f && it.properties[f.name] ? String(it.properties[f.name]).slice(0, 10) : "";
  };
  const rankOf = (it) => {
    const f = typeOf.get(it.type).f.priority;
    return f ? f.values.indexOf(it.properties[f.name]) : -1;
  };
  const space = params.get("space") || "";
  const lists = {
    mine: open.filter((it) => assigneeOf(it) === me.user.id),
    unassigned: open.filter((it) => assigneeOf(it) === ""),
    all: open,
  };
  const current = lists[tab] ? tab : (lists.mine.length ? "mine" : "all");
  const shown = lists[current].filter((it) => !space || it.space === space);
  // Overdue first, then by due date, then the most urgent, then by key.
  shown.sort((a, b) => {
    const da = dueOf(a) || "9999", db = dueOf(b) || "9999";
    return da.localeCompare(db) || rankOf(b) - rankOf(a) || a.space.localeCompare(b.space) || a.key.localeCompare(b.key, undefined, { numeric: true });
  });
  const mayEditIn = new Set(spaces.filter((s) => mayEdit(s.role)).map((s) => s.key));
  const status = h("div", { class: "status" });
  const setAssignee = (it, value) => action(() =>
    put(`/api/items/${it.id}/properties/${encodeURIComponent(typeOf.get(it.type).f.assignee.name)}`, { value }), status);

  const row = (it) => {
    const { ty, f } = typeOf.get(it.type);
    const who = assigneeOf(it);
    const due = dueOf(it);
    const canAct = mayEditIn.has(it.space);
    const claim = canAct && f.assignee
      ? (who === me.user.id
        ? h("button", { class: "link", text: t("tasks.unclaim"), onclick: () => setAssignee(it, null) })
        : h("button", { class: "link", text: t("tasks.claim"), onclick: () => setAssignee(it, me.user.id) }))
      : null;
    const moves = canAct ? ty.workflow.transitions.filter((tr) => tr.from.includes(it.state))
      .map((tr) => h("button", { class: "link", text: tr.name, onclick: () => moveItem(ty.workflow, it, tr, () => post(`/api/items/${it.id}/transitions`, { name: tr.name }), status) })) : [];
    return h("tr", { class: due && due < today ? "overdue" : "", "data-key": it.key },
      h("td", {}, link("#/item/" + encodeURIComponent(it.key), it.key, "key")),
      h("td", { text: it.title }),
      h("td", { class: "task-labels" }, (it.labels || []).map((l) => {
        const k = kanbanOf.get(`${it.space}\n${l}`);
        return k ? link(`#/kanban/${encodeURIComponent(k.space)}/${k.id}`, l, "chip kanban-chip")
          : link(`#/labelboard/${encodeURIComponent(l)}?space=${encodeURIComponent(it.space)}`, l, "chip");
      })),
      h("td", { text: it.state }),
      h("td", { text: who === null ? "" : who ? person(who) : t("tasks.nobody") }),
      h("td", {}, due ? h("span", { class: due < today ? "badge danger" : due === today ? "badge" : "", text: due < today ? t("tasks.overdue", { date: due }) : due }) : ""),
      h("td", { text: f.priority ? it.properties[f.priority.name] || "" : "" }),
      h("td", {}, link(`#/board/${encodeURIComponent(it.space)}/${encodeURIComponent(it.type)}`, it.space)),
      h("td", { class: "task-actions" }, [claim, ...moves].filter(Boolean).flatMap((b, k) => k ? [" · ", b] : [b])));
  };
  // Every column can be filtered. The filters apply at once, without
  // reading the tasks again, and are kept in the address, so a reload or a
  // shared link shows the same; changing a tab keeps them.
  const filterNames = ["key", "title", "state", "who", "due", "prio", "labels", "labelMode"];
  const filters = Object.fromEntries(filterNames.map((k) => [k, params.get(k) || ""]));
  const query = (extra = {}) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries({ space, ...filters, ...extra })) if (v) q.set(k, v);
    return q.size ? "?" + q : "";
  };
  const weekEnd = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10);
  const contains = (text, part) => String(text).toLowerCase().includes(part.trim().toLowerCase());
  // Labels filter by any of the chosen ones, or by all of them; "-" stands
  // for tasks without a label.
  const chosenLabels = () => filters.labels ? filters.labels.split(",") : [];
  const labelled = (it) => {
    const chosen = chosenLabels(), has = (l) => l === "-" ? !(it.labels || []).length : (it.labels || []).includes(l);
    return !chosen.length || (filters.labelMode === "all" ? chosen.every(has) : chosen.some(has));
  };
  const matches = (it) => {
    const due = dueOf(it), who = assigneeOf(it);
    const f = typeOf.get(it.type).f;
    return labelled(it) && contains(it.key, filters.key) && contains(it.title, filters.title)
      && (!filters.state || it.state === filters.state)
      && (!filters.who || (filters.who === "-" ? who === "" : who === filters.who))
      && (!filters.prio || (f.priority && it.properties[f.priority.name] === filters.prio))
      && ({ "": true, overdue: due && due < today, today: due === today, week: due && due >= today && due <= weekEnd, none: !due }[filters.due] ?? true);
  };
  const rows = shown.map((it) => [it, row(it)]);
  const count = h("p", { class: "muted", id: "tasks-count", role: "status" });
  const noneShown = h("p", { class: "empty", text: t("tasks.noneFiltered"), hidden: true });
  const apply = () => {
    let n = 0;
    for (const [it, tr] of rows) n += (tr.hidden = !matches(it)) ? 0 : 1;
    count.textContent = n === rows.length ? "" : t("tasks.filtered", { n, total: rows.length });
    noneShown.hidden = n > 0;
    reset.hidden = !filterNames.some((k) => filters[k]);
    history.replaceState(null, "", `#/tasks/${current}${query()}`);
    relink();
  };
  const set = (k) => (ev) => { filters[k] = ev.target.value; apply(); };
  const distinct = (values) => [...new Set(values.filter((v) => v !== "" && v !== null && v !== undefined))].sort((a, b) => String(a).localeCompare(String(b)));
  const choose = (k, label, options) => h("select", { "aria-label": t("tasks.filterOf", { column: label }), onchange: set(k) },
    h("option", { value: "", text: t("tasks.filterAll") }),
    options.map(([v, text]) => h("option", { value: v, text, selected: v === filters[k] })));
  const typed = (k, label) => {
    const input = h("input", { type: "search", value: filters[k], placeholder: t("tasks.filterText"), "aria-label": t("tasks.filterOf", { column: label }) });
    input.addEventListener("input", set(k));
    return input;
  };
  const whoValues = shown.map(assigneeOf);
  const filterRow = h("tr", { class: "filters" },
    h("td", {}, typed("key", t("col.item"))),
    h("td", {}, typed("title", t("col.title"))),
    h("td", {}),
    h("td", {}, choose("state", t("col.state"), distinct(shown.map((it) => it.state)).map((v) => [v, v]))),
    h("td", {}, choose("who", t("tasks.assignee"), [
      ...(whoValues.includes("") ? [["-", t("tasks.nobody")]] : []),
      ...distinct(whoValues).map((v) => [v, person(v)])])),
    h("td", {}, choose("due", t("tasks.due"), [["overdue", t("tasks.dueOverdue")], ["today", t("tasks.dueToday")], ["week", t("tasks.dueWeek")], ["none", t("tasks.dueNone")]])),
    h("td", {}, choose("prio", t("tasks.priority"), distinct(shown.map((it) => { const f = typeOf.get(it.type).f.priority; return f ? it.properties[f.name] : ""; })).map((v) => [v, v]))),
    h("td", {}, h("select", { "aria-label": t("tasks.filterOf", { column: t("col.space") }), onchange: (ev) => {
      location.hash = `#/tasks/${current}${query({ space: ev.target.value })}`;
    } }, h("option", { value: "", text: t("tasks.allSpaces") }),
    [...new Set(open.map((it) => it.space))].sort().map((k) => h("option", { value: k, text: k, selected: k === space })))),
    h("td", {}));
  // The labels of the tasks shown, to filter by at the right.
  const labelCounts = new Map();
  for (const it of shown) for (const l of it.labels || []) labelCounts.set(l, (labelCounts.get(l) || 0) + 1);
  const unlabelled = shown.filter((it) => !(it.labels || []).length).length;
  const labelBoxes = [...[...labelCounts].sort((a, b) => a[0].localeCompare(b[0])), ...(unlabelled ? [["-", unlabelled]] : [])].map(([l, n]) => {
    const box = h("input", { type: "checkbox", value: l, checked: chosenLabels().includes(l) });
    box.addEventListener("change", () => {
      filters.labels = labelBoxes.filter(([b]) => b.checked).map(([b]) => b.value).join(",");
      apply();
    });
    return [box, h("label", { class: "check" }, box, " ", l === "-" ? h("em", { text: t("tasks.noLabel") }) : l, " ", h("span", { class: "muted", text: `(${n})` }))];
  });
  const modeBox = h("select", { id: "tasks-label-mode", "aria-label": t("tasks.labelMode"), onchange: set("labelMode") },
    h("option", { value: "", text: t("tasks.labelAny"), selected: filters.labelMode !== "all" }),
    h("option", { value: "all", text: t("tasks.labelAll"), selected: filters.labelMode === "all" }));
  const labelPanel = labelBoxes.length ? h("aside", { class: "tasks-filter side-card", id: "tasks-labels", "aria-label": t("tasks.labels") },
    h("h2", { text: t("tasks.labels") }),
    h("div", { class: "tasks-label-list" }, labelBoxes.map(([, el]) => el)),
    h("p", {}, modeBox)) : null;
  const reset = h("button", { type: "button", class: "link", text: t("tasks.filterReset"), hidden: true, onclick: () => {
    location.hash = `#/tasks/${current}${space ? "?space=" + encodeURIComponent(space) : ""}`;
  } });
  const tabLink = (id, label) => link(`#/tasks/${id}${query()}`, `${label} (${lists[id].length})`, id === current ? "tab active" : "tab");
  const tabs = h("nav", { class: "tabs", "aria-label": t("tasks.title") },
    tabLink("mine", t("tasks.mine")), tabLink("unassigned", t("tasks.unassigned")), tabLink("all", t("tasks.all")),
    link("#/kanbans", t("kanban.tab"), "tab", "tab-kanbans"));
  // The tab links carry the filters as they change.
  const relink = () => tabs.querySelectorAll("a").forEach((a) => { a.href = a.getAttribute("href").split("?")[0] + query(); });
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("tasks.title") }),
      h("p", { class: "actions" }, link(newHref(space || here.space, "", taskType(types)), t("tasks.new"), "button primary", "tasks-new"),
        rows.length ? h("button", { type: "button", class: "secondary", id: "tasks-csv", text: t("export.csv"), onclick: () => downloadCSV("tasks.csv", [
          [t("col.item"), t("col.title"), t("tasks.labels"), t("col.state"), t("tasks.assignee"), t("tasks.due"), t("tasks.priority"), t("col.space")],
          ...rows.filter(([, tr]) => !tr.hidden).map(([it]) => {
            const who = assigneeOf(it), f = typeOf.get(it.type).f;
            return [it.key, it.title, (it.labels || []).join(", "), it.state, who ? names.get(who) || who : "", dueOf(it), f.priority ? it.properties[f.priority.name] || "" : "", it.space];
          })]) }) : null)),
    tabs,
    requests,
    status,
    h("div", { class: "tasks-layout" }, h("section", { id: "tasks" }, shown.length
      ? [h("table", {}, h("thead", {}, h("tr", {}, [t("col.item"), t("col.title"), t("tasks.labels"), t("col.state"), t("tasks.assignee"), t("tasks.due"), t("tasks.priority"), t("col.space"), ""]
        .map((c) => h("th", { text: c }))), filterRow),
      h("tbody", {}, rows.map(([, tr]) => tr))), h("p", { class: "tasks-foot" }, count, reset), noneShown]
      : empty(current === "mine" ? t("tasks.noneMine") : t("tasks.none"))),
    labelPanel),
    h("p", { class: "muted", text: t("tasks.conventions") }),
    page.next ? h("p", { class: "notice warning", text: t("tasks.truncated", { n: items.length }) }) : null,
  );
  if (rows.length) apply();
}

// --- kanbans (ADR-0060) -------------------------------------------------------

// readAll reads every page of an item listing, at most boardMax items.
async function readAll(base) {
  let page = await get(base);
  const items = [...page.items];
  while (page.next && items.length < boardMax) {
    page = await get(`${base}&after=${encodeURIComponent(page.next)}`);
    items.push(...page.items);
  }
  return { items, truncated: Boolean(page.next) };
}

// kanbanIcon shows a kanban's picture or logo (ADR-0087), or nothing.
function kanbanIcon(k, size) {
  if (!k.icon) return null;
  return h("img", { class: `space-icon space-icon-${size} kanban-icon`, alt: "", width: size, height: size,
    src: `/api/spaces/${encodeURIComponent(k.space)}/kanbans/${encodeURIComponent(k.id)}/icon?v=${encodeURIComponent(k.icon)}` });
}

// labelBoardView shows every task that carries a label as a board, one
// per type with a workflow, its columns the states (ADR-0087), with the
// cards, filter and details of a kanban (ADR-0139). Nothing is kept: the
// board is the label, as the task list's filter is. A card moves by a
// transition, as on the board of a type (ADR-0036). Where the board shows
// one space one may edit, a card is made there with the label.
async function labelBoardView(label, space) {
  const show = screen();
  const at = location.hash.split("?")[0];
  const [{ types }, { spaces }, { kanbans }] = await Promise.all([get("/api/types"), get("/api/spaces"), get("/api/kanbans")]);
  const found = await readAll(`/api/items?label=${encodeURIComponent(label)}&limit=100${space ? "&space=" + encodeURIComponent(space) : ""}`);
  const flows = types.filter((ty) => ty.workflow && found.items.some((it) => it.type === ty.name));
  const inSpaces = [...new Set(found.items.filter((it) => flows.some((ty) => ty.name === it.type)).map((it) => it.space))].sort();
  const status = h("div", { class: "status" });
  const go = (sp) => { location.hash = `#/labelboard/${encodeURIComponent(label)}${sp ? "?space=" + encodeURIComponent(sp) : ""}`; };
  const filter = inSpaces.length > 1 || space
    ? field(t("col.space"), h("select", { id: "labelboard-space", onchange: (ev) => go(ev.target.value) },
      h("option", { value: "", text: t("labelboard.allSpaces") }),
      favouritesFirst([...new Set([...inSpaces, space].filter(Boolean))].map((key) => ({ key, favourite: spaces.find((x) => x.key === key)?.favourite })))
        .map(({ key: sp }) => h("option", { value: sp, selected: sp === space,
        text: `${sp} ${spaces.find((x) => x.key === sp)?.name || ""}` }))))
    : null;
  const kept = kanbans.filter((k) => k.label === label && (!space || k.space === space));
  const editable = new Set(spaces.filter((sp) => mayEdit(sp.role)).map((sp) => sp.key));
  const home = space || (inSpaces.length === 1 ? inSpaces[0] : "");
  const make = editable.has(home) ? { space: home, label } : null;
  const view = viewOptions(flows, filter, `labelboard.${label}`, at);
  const layout = flows.length ? await cardBoard({
    id: "labelboard", status, label, mayEdit: (it) => editable.has(it.space), cardFields: viewCardFields,
    sections: flows.map((ty) => viewSection(ty, found.items.filter((it) => it.type === ty.name), status, make,
      `labelboard.${label}/${ty.name}`, flows.length > 1 ? ty.name : "")),
    // Taking the label off takes the card off the board; the item keeps
    // its other labels, as on a kanban.
    detailTail: (item, close) => (editable.has(item.space) ? h("button", { type: "button", class: "danger kanban-off", id: "kanban-detail-off",
      text: t("labelboard.takeOff", { label }), onclick: () => {
        close();
        action(() => put(`/api/items/${item.id}/labels`, { labels: item.labels.filter((l) => l !== label) }), status);
      } }) : ""),
    ...view.look,
  }) : null;
  show(
    h("p", {}, link("#/tasks", t("nav.tasks")), " / ", link("#/kanbans", t("kanban.tab")), " / " + label),
    h("div", { class: "page-head" }, h("h1", {}, t("labelboard.title") + " ", labelChip(label))),
    h("p", { class: "muted", text: t("labelboard.intro") }),
    kept.length ? h("p", { class: "muted" }, t("labelboard.kept") + " ",
      kept.map((k) => link(`#/kanban/${encodeURIComponent(k.space)}/${k.id}`, `${k.space} · ${k.name}`, "chip kanban-chip"))) : null,
    view.el,
    found.truncated ? h("p", { class: "notice warning", text: t("board.truncated", { n: found.items.length }) }) : null,
    status,
    layout || empty(t("labelboard.none"), "labelboard-empty"),
  );
}

// kanbansView lists the kanbans of one's spaces and makes new ones: a
// name, a type with a workflow; their cards are the items with its label.
// filteredKanbans filters the kanbans, grouped by space, as one list
// (ADR-0098); a space none of whose kanbans is shown hides its heading.
function filteredKanbans(box) {
  const tiles = () => [...box.querySelectorAll("li[data-f-space]")];
  return filtered(box, { id: "kanbans", label: t("kanban.title"), rows: tiles, choices: [{ key: "space", label: t("col.space") }],
    onApply: () => box.querySelectorAll("section").forEach((s) => { s.hidden = ![...s.querySelectorAll("li")].some((li) => !li.hidden); }) });
}

async function kanbansView() {
  const show = screen();
  const [{ kanbans }, { spaces }, { types }, { labels }] = await Promise.all([get("/api/kanbans"), get("/api/spaces"), get("/api/types"),
    get("/api/labels?tasks=1").catch(() => ({ labels: [] }))]);
  const flows = types.filter((ty) => ty.workflow);
  const editable = spaces.filter((sp) => mayEdit(sp.role));
  const counts = await Promise.all(kanbans.map((k) =>
    get(`/api/items?space=${encodeURIComponent(k.space)}&type=${encodeURIComponent(k.type)}&label=${encodeURIComponent(k.label)}&limit=100`)
      .then((p) => (p.next ? p.items.length + "+" : p.items.length)).catch(() => "?")));
  const create = editable.length && flows.length ? form({ id: "new-kanban", class: "card" }, [
    h("h2", { text: t("kanban.new") }),
    h("div", { class: "form-row" },
      field(t("col.space"), h("select", { name: "space" }, favouritesFirst(editable).map((sp) => h("option", { value: sp.key, text: `${sp.key} ${sp.name}`, selected: sp.key === here.space })))),
      field(t("kanban.name"), h("input", { name: "name", required: true, maxlength: 80, placeholder: t("kanban.namePlaceholder") })),
      field(t("kanban.type"), h("select", { name: "type" }, flows.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === "task" }))))),
    h("details", {}, h("summary", { text: t("kanban.labelMore") }),
      field(t("kanban.label"), h("input", { name: "label", placeholder: t("kanban.labelPlaceholder") }))),
    h("button", { type: "submit", class: "primary", text: t("kanban.create") }),
  ], async (d) => {
    const { kanban } = await post(`/api/spaces/${encodeURIComponent(d.get("space"))}/kanbans`, { name: d.get("name"), type: d.get("type"), label: (d.get("label") || "").trim() });
    // A new kanban's cards show who has them, their dates and their
    // choices, so they say something before anyone sets them (ADR-0132).
    const shown = (flows.find((ty) => ty.name === kanban.type)?.fields || []).filter((f) => ["user", "date", "enum"].includes(f.kind)).map((f) => f.name).slice(0, 12);
    if (shown.length) await put(`/api/spaces/${encodeURIComponent(kanban.space)}/kanbans/${encodeURIComponent(kanban.id)}`, { name: kanban.name, cardFields: shown });
    location.hash = `#/kanban/${encodeURIComponent(kanban.space)}/${kanban.id}`;
  }) : null;
  const bySpace = new Map();
  kanbans.forEach((k, i) => bySpace.set(k.space, [...(bySpace.get(k.space) || []), { ...k, count: counts[i] }]));
  show(
    h("div", { class: "page-head" }, h("h1", { text: t("kanban.title") })),
    h("nav", { class: "tabs", "aria-label": t("tasks.title") },
      link("#/tasks/mine", t("tasks.mine"), "tab"), link("#/tasks/unassigned", t("tasks.unassigned"), "tab"), link("#/tasks/all", t("tasks.all"), "tab"),
      link("#/kanbans", t("kanban.tab"), "tab active", "tab-kanbans")),
    // The boards by label lead on the left, the kanbans stand in the
    // middle, a new kanban is made on the right (ADR-0110).
    h("div", { class: "kanbans-layout" },
      labels.length ? labelBoardNav(labels) : null,
      h("div", { class: "kanbans-main" },
        h("p", { class: "muted", text: t("kanban.intro") }),
        kanbans.length ? filteredKanbans(h("div", { id: "kanbans" }, [...bySpace].map(([sp, list]) => h("section", {},
          h("h2", { text: `${sp} · ${spaces.find((x) => x.key === sp)?.name || ""}` }),
          h("ul", { class: "space-tiles" }, list.map((k) => h("li", { "data-f-space": k.space },
            h("a", { class: "space-tile", href: `#/kanban/${encodeURIComponent(k.space)}/${k.id}` },
              h("span", { class: "space-tile-head" }, h("strong", { text: k.name }), kanbanIcon(k, 48)),
              h("span", { class: "muted", text: t("kanban.meta", { type: k.type, label: k.label }) }),
              h("span", { class: "badge", text: t("kanban.cards", { n: k.count }) })))))))))
          : empty(t("kanban.none"))),
      create ? h("aside", { class: "kanbans-new", "aria-label": t("kanban.new") }, create) : null),
  );
}

// labelBoardNav lists the boards by label as the page's navigation: the
// filter always shows and suggests the labels as one types; Enter opens
// the one board left.
function labelBoardNav(labels) {
  const list = h("ul", { class: "label-boards" }, labels.map((l) => h("li", { "data-filter-text": l.label },
    link(`#/labelboard/${encodeURIComponent(l.label)}`, l.label), " ", h("span", { class: "muted", text: `(${l.count})` }))));
  const box = filtered(list, { id: "labelboards", label: t("labelboard.listTitle"), min: 0 });
  const input = box.querySelector(".filter-text");
  input.setAttribute("list", "labelboards-suggest");
  input.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    const left = [...list.children].filter((li) => !li.hidden);
    const exact = left.find((li) => norm(li.dataset.filterText) === norm(input.value));
    const open = exact || (left.length === 1 ? left[0] : null);
    if (open) {
      ev.preventDefault();
      location.hash = open.querySelector("a").getAttribute("href");
    }
  });
  return h("aside", { class: "kanbans-labels side-card", id: "label-boards", "aria-label": t("labelboard.listTitle") },
    h("h2", { text: t("labelboard.listTitle") }),
    h("p", { class: "muted", text: t("labelboard.listIntro") }),
    box,
    h("datalist", { id: "labelboards-suggest" }, labels.map((l) => h("option", { value: l.label }))));
}

// kanbanView shows one kanban: its cards in the columns of the type's
// workflow. Editors add a new card, put an existing item on it, take a
// card off (the item keeps its other labels and kanbans), rename the
// kanban or remove it.
async function kanbanView(space, id) {
  const show = screen();
  const kpath = `/api/spaces/${encodeURIComponent(space)}/kanbans/${encodeURIComponent(id)}`;
  const [{ types }, { spaces }, board] = await Promise.all([get("/api/types"), get("/api/spaces"), get(kpath + "/board")]);
  const k = board.kanban;
  here.space = space;
  const type = types.find((ty) => ty.name === k.type);
  if (!type?.workflow) throw new Error(t("board.noWorkflow", { type: k.type }));
  const canEdit = mayEdit(spaces.find((sp) => sp.key === space)?.role);
  const base = `/api/items?space=${encodeURIComponent(space)}&type=${encodeURIComponent(k.type)}&limit=100`;
  const [cards, all] = await Promise.all([readAll(`${base}&label=${encodeURIComponent(k.label)}`), canEdit ? readAll(base) : { items: [] }]);
  const status = h("div", { class: "status" });
  const setLabels = (it, labels) => put(`/api/items/${it.id}/labels`, { labels });
  const elsewhere = all.items.filter((it) => !it.labels.includes(k.label));
  const wf = type.workflow;

  // A card moves into a column of its own, or by a transition into a
  // state's column, and stands where it was dropped. A move the workflow
  // lacks is reported; an administrator can add it at once (ADR-0076).
  const place = async (it, col, before) => {
    const again = () => post(kpath + "/place", { card: it.id, column: col.id });
    if (col.state && col.state !== it.state && !wf.transitions.some((tr) => tr.to === col.state && tr.from.includes(it.state))) {
      moveRefused(status, new Error(t("kanban.noTransition", { type: k.type, from: it.state, to: col.state })), type, it, col.state, again);
      return;
    }
    try {
      await post(kpath + "/place", { card: it.id, column: col.id, before: before || "" });
      route();
    } catch (err) {
      if (col.state && /no transition from/.test(err.message)) moveRefused(status, err, type, it, col.state, again);
      else status.replaceChildren(errorBox(err));
    }
  };
  // A new card is made in the column it is meant for: in its state, in
  // a column of its own, or one transition away from where cards start.
  // It carries the kanban's label and stands on top of its column.
  const takes = (col) => canEdit && (!col.state || col.state === wf.initial || wf.transitions.some((tr) => tr.to === col.state && tr.from.includes(wf.initial)));
  const create = async (col, title, properties) => {
    const { item } = await post("/api/items", { space, type: k.type, title, properties });
    await setLabels(item, [k.label]);
    await post(kpath + "/place", { card: item.id, column: col.id, before: (col.cards || [])[0] || "" });
  };

  // The kanban's settings, its name and its removal wait behind one
  // button, so the board starts right below its title (ADR-0132).
  const settings = canEdit ? kanbanSettings(k, type, board.columns, kpath) : null;
  const tools = canEdit && elsewhere.length ? h("div", { class: "kanban-tools" },
    form({ id: "kanban-assign", class: "inline-form" }, [
      h("select", { name: "item", "aria-label": t("kanban.assign") }, elsewhere.map((it) => h("option", { value: it.id, text: `${it.key} ${it.title}` }))),
      h("button", { type: "submit", text: t("kanban.assign") }),
    ], async (d) => {
      const it = elsewhere.find((x) => x.id === d.get("item"));
      await setLabels(it, [...it.labels, k.label]);
      route();
    })) : null;
  // The colour of a card is a value of the task's own, in the tone the
  // kanban gives it or that of its place among the values (ADR-0134).
  const colorField = k.colorField ? type.fields.find((f) => f.name === k.colorField && f.kind === "enum") : null;
  const layout = await cardBoard({
    id: "kanban", status, label: k.label, mayEdit: () => canEdit,
    sections: [{ type, items: cards.items, columns: board.columns, place, takes, create, ordered: true,
      foldKey: `wwbb.kanban.folded.${space}/${id}`, laneKey: `wwbb.kanban.lanes.${space}/${id}` }],
    cardFields: () => k.cardFields,
    colorField: colorField ? colorField.name : "", colorValues: colorField ? colorField.values : [],
    toneOf: (v) => k.colors?.[v] || kanbanTones[Math.max(0, colorField.values.indexOf(v)) % kanbanTones.length],
    laneField: k.laneField || "",
    // Taking a card off is done here, on the one card in view, not on
    // every card of the board.
    detailTail: (item, close) => (canEdit ? h("button", { type: "button", class: "danger kanban-off", id: "kanban-detail-off", text: t("kanban.takeOff"), onclick: () => {
      close();
      action(() => setLabels(item, item.labels.filter((l) => l !== k.label)), status);
    } }) : ""),
  });
  show(
    h("p", {}, link("#/tasks", t("nav.tasks")), " / ", link("#/kanbans", t("kanban.tab")), " / " + k.name),
    h("div", { class: "page-head" }, h("div", { class: "kanban-title" }, kanbanIcon({ ...k, space }, 48), h("h1", { text: k.name })),
      h("div", { class: "actions" }, h("span", { class: "muted", text: t("kanban.metaLong", { space, type: k.type, label: k.label }) }),
        settings ? h("button", { type: "button", class: "secondary kanban-settings-open", id: "kanban-settings-open", onclick: () => settings.showModal() },
          lineIcon("M4 7h9M17 7h3M4 17h3M11 17h9M15 5v4M9 15v4"), t("kanban.settings")) : null)),
    tools,
    settings,
    cards.truncated ? h("p", { class: "notice warning", text: t("board.truncated", { n: cards.items.length }) }) : null,
    status,
    layout,
  );
}

// --- boards of cards (ADR-0132, ADR-0134, ADR-0137, ADR-0139) -----------------

// cardBoard lays out cards in the columns of their workflow, for a kanban
// and for the boards of a type and of a label, which keep nothing
// (ADR-0139). All show the same cards, filter, legend, folded columns,
// finished cards by day, lanes, and the details of a card beside the
// board. What differs comes in o:
//   id          the element id of the boards with their filter
//   sections    a board per type: { type, items, columns, heading,
//               place(it, col, before), takes(col), create(col, title,
//               properties), ordered, foldKey, laneKey }; a column is
//               { id, name, state, limit, cards }, its cards the IDs of
//               its items; ordered tells that the cards keep the order
//               they are dropped in
//   cardFields  the fields a card of a type shows: cardFields(type)
//   label       the label the board stands for, not repeated on its cards
//   colorField, colorValues, toneOf, laneField  colour and lanes by a
//               field of the task, named
//   mayEdit     whether one may change an item: mayEdit(it)
//   detailTail  what the details of an item end with: detailTail(item, close)
//   status      where errors are said
// It answers the layout, with the card the address names open.
async function cardBoard(o) {
  const { status } = o;
  const sectionOf = new Map(o.sections.flatMap((s) => s.items.map((it) => [it.id, s])));
  const byID = new Map(o.sections.flatMap((s) => s.items.map((it) => [it.id, it])));
  const items = [...byID.values()];
  const typeOf = (it) => sectionOf.get(it.id).type;
  const fieldOf = (type, name, kinds) => type.fields.find((f) => f.name === name && (!kinds || kinds.includes(f.kind))) || null;
  const namesOf = (type, kind) => type.fields.filter((f) => f.kind === kind).map((f) => f.name);
  const peopleOf = (it) => namesOf(typeOf(it), "user").map((n) => it.properties[n]).filter(Boolean);
  await learnNames(items.flatMap(peopleOf));
  const params = new URLSearchParams(location.hash.split("?")[1] || "");
  const byName = (a, b) => (a === me.user.id ? -1 : b === me.user.id ? 1 : person(a).localeCompare(person(b)));

  // The open card is one part of the address; the filter keeps its own.
  const setCard = (key) => {
    const [route, query = ""] = location.hash.split("?");
    const q = new URLSearchParams(query);
    if (key) q.set("card", key); else q.delete("card");
    const rest = q.toString();
    history.replaceState(null, "", route + (rest ? "?" + rest : ""));
  };
  const select = (it) => {
    setCard(it.key);
    layout.querySelectorAll(".card-item.selected").forEach((el) => el.classList.remove("selected"));
    layout.querySelector(`.card-item[data-id="${it.id}"]`)?.classList.add("selected");
    openDetail(it);
  };
  // dueOf sorts a card for the filter by its dates: overdue, within a
  // week, or without a date; a finished card is none of the first two.
  const dueOf = (it) => {
    const type = typeOf(it);
    const days = namesOf(type, "date").map((n) => it.properties[n]).filter(Boolean).map((v) => daysFromToday(String(v).slice(0, 10))).filter((d) => d !== null);
    if (!days.length) return "none";
    if (finished(type.workflow, it.state)) return "";
    if (days.some((d) => d < 0)) return "late";
    return days.some((d) => d <= 7) ? "week" : "";
  };
  // A card's colour is the value of the field the board colours by: the
  // task's own, so it shows wherever the task does (ADR-0134).
  const colorOf = (it) => (fieldOf(typeOf(it), o.colorField, ["enum"]) ? it.properties[o.colorField] || "" : "");

  // A card says at a glance what it is, who has it and what is behind it
  // (ADR-0132): its title first; the fields the board names as chips, a
  // person as a face; at its foot its key, whether it has a text and how
  // far its checklist is.
  const cardEl = (it) => {
    const type = typeOf(it);
    const chips = [], people = [];
    for (const n of o.cardFields(type)) {
      if (n === "labels") {
        chips.push(...it.labels.filter((l) => l !== o.label).map((l) => h("span", { class: "card-chip card-label", title: `${n}: ${l}`, text: l })));
        continue;
      }
      const f = fieldOf(type, n), v = n === "state" ? it.state : it.properties[n];
      if (v === undefined || v === null || v === "" || v === false) continue;
      if (f?.kind === "user") {
        people.push(h("span", { class: "card-person", role: "img", title: `${n}: ${person(v)}`, "aria-label": `${n}: ${person(v)}` }, face(v, 24)));
      } else {
        chips.push(cardChip(n, f, v, finished(type.workflow, it.state)));
      }
    }
    // A card that waits on unfinished work it depends on, or that blocks
    // it, says so first (ADR-0137).
    if (it.waitingOn) chips.unshift(h("span", { class: "card-chip late card-waiting", title: t("card.waitingHint", { n: it.waitingOn }) },
      lineIcon("M7 4h10M7 20h10M8 4c0 5 8 6 8 8s-8 3-8 8M16 4c0 5-8 6-8 8"), t("card.waiting", { n: it.waitingOn })));
    const color = colorOf(it);
    const who = peopleOf(it);
    const li = h("li", { class: "card-item kanban-card" + (color ? ` toned tone-${o.toneOf(color)}` : ""), draggable: o.mayEdit(it) ? "true" : "false", "data-id": it.id, tabindex: "0",
      "data-filter-text": [it.key, it.title, ...it.labels, ...who.map(person), ...type.fields.filter((f) => f.kind !== "user").map((f) => it.properties[f.name] ?? "")].join(" "),
      "data-f-who": who.join("|"), "data-f-label": it.labels.filter((l) => l !== o.label).join("|"), "data-f-due": dueOf(it), "data-f-color": color, "data-f-waiting": it.waitingOn ? "yes" : "",
      ondragstart: (ev) => { ev.dataTransfer.setData("text/plain", it.id); ev.dataTransfer.effectAllowed = "move"; },
      onclick: (ev) => { if (!ev.target.closest("a, button")) select(it); },
      onkeydown: (ev) => { if (ev.key === "Enter" && ev.target === li) select(it); } },
    h("div", { class: "card-title", text: it.title }),
    chips.length ? h("div", { class: "card-chips" }, chips) : "",
    h("div", { class: "card-foot" }, h("span", { class: "card-key", text: it.key }), cardSigns(it),
      people.length ? h("span", { class: "card-people" }, people) : ""));
    return li;
  };

  // Columns and lanes one folds away stay folded for them, in this
  // browser only.
  const kept = (key) => {
    try { return new Set(JSON.parse(localStorage.getItem(key) || "[]")); } catch { return new Set(); }
  };
  const keep = (key, set) => {
    try { localStorage.setItem(key, JSON.stringify([...set])); } catch { /* kept for this view only */ }
  };
  let filteredBoard = null;

  // sectionBoard is the board of one type: its columns, or its lanes.
  const sectionBoard = (s) => {
    const { type } = s, wf = type.workflow;
    const folded = kept(s.foldKey), foldedLanes = kept(s.laneKey);
    const takes = (col) => Boolean(s.takes?.(col));
    const addCard = (col, el, list, lane) => {
      const open = el.querySelector(".card-new input");
      if (open) return open.focus();
      const input = h("input", { name: "title", required: true, placeholder: t("kanban.newCard"), "aria-label": t("kanban.newCardIn", { column: col.name }) });
      const f = form({ class: "card-new" }, [input, h("button", { type: "submit", class: "primary", text: t("kanban.add") })], async (d) => {
        // A card made in a lane takes the lane's value (ADR-0137).
        await s.create(col, d.get("title").trim(), lane ? { [laneField.name]: lane } : {});
        route();
      });
      input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") f.remove(); });
      el.insertBefore(f, list);
      input.focus();
    };
    const fold = (col, el, button) => {
      const shut = !folded.has(col.id);
      if (shut) folded.add(col.id); else folded.delete(col.id);
      keep(s.foldKey, folded);
      el.classList.toggle("folded", shut);
      button.setAttribute("aria-expanded", String(!shut));
    };
    // A finished state's column lists its cards by the day they got
    // there, the newest first, and the most recent only until asked for
    // all (ADR-0134).
    const cardsOf = (col, ids = col.cards || []) => {
      const list = ids.map((cid) => byID.get(cid)).filter(Boolean);
      if (!col.state || !finished(wf, col.state)) return list.map(cardEl);
      list.sort((a, b) => (Date.parse(b.stateSince) || 0) - (Date.parse(a.stateSince) || 0));
      const out = [];
      let day = null;
      list.forEach((it, i) => {
        const more = i >= recentDone ? " card-more" : "";
        const d = it.stateSince ? localDay(it.stateSince) : "";
        if (d && d !== day) out.push(h("li", { class: "card-day" + more, "data-day": d, text: dayName(d) }));
        day = d;
        const el = cardEl(it);
        if (more) el.classList.add("card-more");
        out.push(el);
      });
      if (list.length > recentDone) {
        out.push(h("li", { class: "card-show-all" }, h("button", { type: "button", class: "link", text: t("kanban.showAll", { n: list.length }),
          onclick: (ev) => ev.target.closest(".board-column").classList.add("expanded") })));
      }
      return out;
    };
    // A column's count names its limit beside it, and says when the
    // column holds as many cards or more (ADR-0134); a limit is a signal,
    // the move is never refused for it.
    const countOf = (col) => {
      const n = (col.cards || []).length;
      if (!col.limit) return h("span", { class: "column-count", text: String(n) });
      return h("span", { class: "column-count" + (n > col.limit ? " over" : n === col.limit ? " full" : ""),
        title: t("kanban.limitHint", { n, limit: col.limit }), text: `${n} / ${col.limit}` });
    };
    // Swimlanes (ADR-0137): rows by the value of a person or choice field
    // of the task. The column heads stand once above them; each lane has
    // a cell per column.
    const laneField = fieldOf(type, o.laneField, ["user", "enum"]);
    const editor = s.items.some((it) => o.mayEdit(it)) || s.columns.some(takes);
    const lanes = !laneField ? [] : laneField.kind === "enum" ? [...laneField.values, ""]
      : [...[...new Set([...s.items.map((it) => it.properties[laneField.name]).filter(Boolean), ...(editor ? [me.user.id] : [])])].sort(byName), ""];
    const laneOf = (it) => {
      const v = laneField ? it.properties[laneField.name] ?? "" : "";
      return lanes.includes(v) ? v : "";
    };
    const laneName = (v) => (v === "" ? t("kanban.laneNone", { field: laneField.name }) : laneField.kind === "user" ? person(v) : v);
    // A card moves into another lane only where it can move into the
    // column, so a refused move leaves its value as it was.
    const canMove = (it, col) => !col.state || col.state === it.state || wf.transitions.filter((tr) => tr.to === col.state && tr.from.includes(it.state)).length === 1;
    // A linked card moved to a finished state is completed at its engine,
    // in the task dialog (ADR-0151).
    const place = (it, col, before) => (col.state && finishing(wf, it, col.state)
      ? taskDialog(it, col.state).then((done) => done && route()) : s.place(it, col, before));
    const moveToLane = async (it, col, before, lane) => {
      if (!canMove(it, col)) return place(it, col, before);
      try {
        await put(`/api/items/${it.id}/properties/${encodeURIComponent(laneField.name)}`, { value: lane === "" ? null : lane });
      } catch (err) {
        status.replaceChildren(errorBox(err));
        return;
      }
      if (!s.ordered && col.state === it.state) return route();
      await place(it, col, before);
    };
    // dropZone makes a column, or a column's cell in a lane, take cards
    // of this type: dropped on a card, before it; elsewhere, last. Into
    // another lane the card first takes the lane's value, a change of the
    // item (ADR-0137). Where cards keep no order, a drop into their own
    // column changes nothing.
    const dropZone = (el, col, lane) => {
      el.addEventListener("dragover", (ev) => { if (!editor) return; ev.preventDefault(); el.classList.add("drop"); });
      el.addEventListener("dragleave", () => el.classList.remove("drop"));
      el.addEventListener("drop", (ev) => {
        ev.preventDefault();
        el.classList.remove("drop");
        const it = byID.get(ev.dataTransfer.getData("text/plain"));
        if (!it || sectionOf.get(it.id) !== s || !o.mayEdit(it)) return;
        const target = ev.target.closest(".card-item");
        const before = target && target.dataset.id !== it.id ? target.dataset.id : "";
        if (lane !== undefined && laneOf(it) !== lane) return moveToLane(it, col, before, lane);
        if (!s.ordered && col.state === it.state) return;
        place(it, col, before);
      });
      return el;
    };
    const columnEl = (col) => {
      const list = h("ul", { class: "board-cards" }, cardsOf(col));
      const shut = folded.has(col.id);
      const el = dropZone(h("section", { class: "board-column" + (col.state ? "" : " own-column") + (shut ? " folded" : ""), "data-state": col.state || "", "data-column": col.id }), col);
      const toggle = h("button", { type: "button", class: "link column-fold", "aria-expanded": String(!shut),
        title: t("kanban.fold", { column: col.name }), "aria-label": t("kanban.fold", { column: col.name }), onclick: () => fold(col, el, toggle) }, lineIcon("M15 6l-6 6 6 6"));
      // The state a column stands for is its tooltip; the name is what
      // counts.
      el.append(h("div", { class: "column-head" },
        h("h2", { title: col.state ? t("kanban.stateColumnHint", { state: col.state }) : t("kanban.ownColumnHint"), text: col.name }),
        countOf(col),
        takes(col) ? h("button", { type: "button", class: "link column-add", title: t("kanban.newCardIn", { column: col.name }),
          "aria-label": t("kanban.newCardIn", { column: col.name }), onclick: () => addCard(col, el, list) }, lineIcon("M12 5v14M5 12h14")) : "",
        toggle),
      list);
      return el;
    };
    const boardEl = h("div", { class: "board" + (laneField ? " board-lanes" : ""), "data-type": type.name });
    const laneBoard = () => {
      const cols = s.columns;
      boardEl.style.gridTemplateColumns = cols.map((c) => (folded.has(c.id) ? "44px" : "minmax(220px, 400px)")).join(" ");
      const heads = cols.map((col) => {
        const shut = folded.has(col.id);
        const toggle = h("button", { type: "button", class: "link column-fold", "aria-expanded": String(!shut),
          title: t("kanban.fold", { column: col.name }), "aria-label": t("kanban.fold", { column: col.name }), onclick: () => {
            if (shut) folded.delete(col.id); else folded.add(col.id);
            keep(s.foldKey, folded);
            laneBoard();
            filteredBoard?.applyFilter();
          } }, lineIcon("M15 6l-6 6 6 6"));
        return h("div", { class: "lane-col-head" + (shut ? " folded" : ""), "data-column": col.id, "data-state": col.state || "" },
          h("div", { class: "column-head" },
            h("h2", { title: col.state ? t("kanban.stateColumnHint", { state: col.state }) : t("kanban.ownColumnHint"), text: col.name }),
            countOf(col), toggle));
      });
      const rows = lanes.flatMap((lane) => {
        const mine = s.items.filter((it) => laneOf(it) === lane).map((it) => it.id);
        const shut = foldedLanes.has(lane);
        const toggle = h("button", { type: "button", class: "link lane-fold", "aria-expanded": String(!shut),
          title: t("kanban.foldLane", { lane: laneName(lane) }), "aria-label": t("kanban.foldLane", { lane: laneName(lane) }), onclick: () => {
            if (shut) foldedLanes.delete(lane); else foldedLanes.add(lane);
            keep(s.laneKey, foldedLanes);
            laneBoard();
            filteredBoard?.applyFilter();
          } }, lineIcon("M6 9l6 6 6-6"));
        const head = h("div", { class: "lane-head" + (shut ? " lane-folded" : ""), "data-lane": lane },
          toggle, laneField.kind === "user" && lane ? face(lane, 24) : null, h("h3", { text: laneName(lane) }),
          h("span", { class: "column-count", text: String(mine.length) }));
        const cells = cols.map((col) => {
          const ids = (col.cards || []).filter((cid) => mine.includes(cid));
          const list = h("ul", { class: "board-cards" }, cardsOf(col, ids));
          const cell = dropZone(h("section", { class: "board-column lane-cell" + (col.state ? "" : " own-column") + (folded.has(col.id) ? " folded" : "") + (shut ? " lane-folded" : ""),
            "data-column": col.id, "data-state": col.state || "", "data-lane": lane }), col, lane);
          if (takes(col)) {
            cell.append(h("button", { type: "button", class: "link cell-add", title: t("kanban.newCardInLane", { column: col.name, lane: laneName(lane) }),
              "aria-label": t("kanban.newCardInLane", { column: col.name, lane: laneName(lane) }), onclick: () => addCard(col, cell, list, lane) }, lineIcon("M12 5v14M5 12h14")));
          }
          cell.append(list);
          return cell;
        });
        return [head, ...cells];
      });
      boardEl.replaceChildren(...heads, ...rows);
    };
    if (laneField) laneBoard(); else boardEl.append(...s.columns.map(columnEl));
    return boardEl;
  };
  const boards = h("div", { class: "board-sections" }, o.sections.map((s) => h("section", { class: "board-section", "data-type": s.type.name },
    s.heading ? h("h2", { text: s.heading }) : null, sectionBoard(s))));

  // The details of a card beside the board: its fields, state, labels and
  // text, as on its own page; changes are saved at once.
  const detail = h("aside", { class: "kanban-detail", id: "kanban-detail", hidden: true, "aria-label": t("kanban.details"),
    onkeydown: (ev) => { if (ev.key === "Escape") closeDetail(); } });
  const closeDetail = () => {
    detail.hidden = true;
    layout.querySelectorAll(".card-item.selected").forEach((el) => el.classList.remove("selected"));
    setCard("");
  };
  const openDetail = async (it) => {
    const type = typeOf(it);
    detail.hidden = false;
    detail.replaceChildren(h("p", { class: "muted", text: t("kanban.loading") }));
    let item, embeds, out, inc;
    try {
      [{ item }, { embeds }, out, inc] = await Promise.all([get("/api/items/" + it.id), get(`/api/items/${it.id}/embeds`),
        get(`/api/items/${it.id}/relations?direction=outgoing`), get(`/api/items/${it.id}/relations?direction=incoming`)]);
    } catch (err) {
      detail.replaceChildren(errorBox(err));
      return;
    }
    const path = "/api/items/" + item.id;
    const editable = o.mayEdit(it) && !item.managed;
    const saved = h("span", { class: "save-mark", role: "status" });
    const props = h("table", { class: "props" }, type.fields.map((f) => {
      const input = propertyInput(f, item.properties[f.name]);
      input.disabled = !editable;
      if (editable) {
        input.addEventListener("change", async () => {
          saved.textContent = t("common.saving");
          try {
            await put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: readProperty(f, input) });
            saved.textContent = "✓ " + t("common.saved");
            route();
          } catch (err) {
            saved.textContent = err.message;
          }
        });
      }
      return h("tr", { "data-field": f.name }, h("th", { text: f.name }), h("td", {}, input));
    }));
    const stateBar = statusBar(type.workflow, item.state, editable ? (tr) => moveItem(type.workflow, item, tr, () => post(path + "/transitions", { name: tr.name }), status) : null);
    detail.replaceChildren(
      h("div", { class: "kanban-detail-head" },
        h("span", { class: "muted", text: item.key }),
        h("button", { type: "button", class: "link", id: "kanban-detail-close", "aria-label": t("common.close"), text: "×", onclick: closeDetail })),
      h("h2", {}, link("#/item/" + encodeURIComponent(item.key), item.title || item.key)),
      stateBar,
      h("p", {}, saved),
      type.fields.length ? props : "",
      labelsLine(path, item, editable),
      type.body ? h("section", { class: "kanban-detail-body" }, item.body.length ? readOnlyBody(item, embeds) : h("p", { class: "muted", text: t("kanban.noText") })) : "",
      // What the card depends on, blocks or relates to (ADR-0137).
      await relationsSection("/api/items/" + item.id, out.relations, inc.relations, item.id, editable),
      h("p", { class: "actions-row" }, link("#/item/" + encodeURIComponent(item.key), t("kanban.openItem"), "button"), o.detailTail ? o.detailTail(item, closeDetail) : ""));
  };

  // The filter of the board (ADR-0098, ADR-0134): by text, person, label,
  // due date and colour, kept in the address.
  const choices = [
    o.sections.some((s) => namesOf(s.type, "user").length)
      ? { key: "who", label: t("kanban.who"), options: [...new Set(items.flatMap(peopleOf))].sort(byName).map((v) => [v, person(v)]) } : null,
    { key: "label", label: t("kanban.label") },
    o.sections.some((s) => namesOf(s.type, "date").length)
      ? { key: "due", label: t("kanban.due"), options: [["late", t("kanban.dueLate")], ["week", t("kanban.dueWeek")], ["none", t("kanban.dueNone")]] } : null,
    o.colorValues.length ? { key: "color", label: o.colorField, options: o.colorValues.map((v) => [v, v]) } : null,
    items.some((it) => it.waitingOn) ? { key: "waiting", label: t("kanban.waiting"), options: [["yes", t("kanban.waitingYes")]] } : null,
  ].filter(Boolean);
  // The legend says what a colour stands for; a click shows only its cards.
  const legend = o.colorValues.length ? h("div", { class: "kanban-legend", id: "kanban-legend", role: "group", "aria-label": o.colorField },
    h("span", { class: "muted", text: o.colorField }),
    o.colorValues.map((v) => h("button", { type: "button", class: `legend-chip tone-${o.toneOf(v)}`, "data-value": v, "aria-pressed": "false", title: t("kanban.legendShow", { value: v }),
      onclick: () => {
        const sel = filteredBoard.querySelector("select[data-choice=color]");
        sel.value = sel.value === v ? "" : v;
        sel.dispatchEvent(new Event("change"));
      } }, h("span", { class: "tone-swatch" }), v))) : null;
  filteredBoard = filtered(boards, { id: "cards", label: t("kanban.cardList"), choices, rows: () => [...boards.querySelectorAll(".card-item")],
    onApply: () => {
      // A day without a card shown hides its heading; a filter shows every
      // card it finds, also the older ones of a finished column.
      for (const head of boards.querySelectorAll(".card-day")) {
        let next = head.nextElementSibling;
        while (next && !next.classList.contains("card-day") && !(next.classList.contains("card-item") && !next.hidden)) next = next.nextElementSibling;
        head.hidden = !next || next.classList.contains("card-day");
      }
      const on = Boolean(filteredBoard && !filteredBoard.querySelector(".filter-reset").hidden);
      boards.querySelectorAll(".board").forEach((b) => b.classList.toggle("filtering", on));
      const shown = filteredBoard?.querySelector("select[data-choice=color]")?.value;
      legend?.querySelectorAll(".legend-chip").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.value === shown)));
    } });
  filteredBoard.applyFilter();
  const layout = h("div", { class: "kanban-layout" },
    h("div", { id: o.id, class: "kanban-board" }, legend, filteredBoard),
    detail);
  const chosen = params.get("card") && items.find((it) => it.key === params.get("card"));
  if (chosen) select(chosen);
  return layout;
}

// moveRefused says that the workflow lacks a move. An administrator adds
// it to the type at once and move() makes it (ADR-0076); others are told
// to ask one.
function moveRefused(status, err, type, it, state, move) {
  status.replaceChildren(errorBox(err), me.admin
    ? h("p", {}, h("button", { type: "button", class: "secondary", id: "kanban-add-transition", text: t("kanban.addTransition", { from: it.state, to: state, type: type.name }),
      onclick: () => action(async () => {
        const { type: now } = await post(`/api/types/${encodeURIComponent(type.name)}/transitions`, { from: it.state, to: state });
        await move(now);
      }, status) }))
    : h("p", { class: "muted", text: t("kanban.askAdmin") }));
}

// cardChip shows one field of a kanban card without its name, which is
// the chip's tooltip and what a screen reader says first (ADR-0132): a
// value of an enumeration or a state as it is, a date by how far off it
// is, a yes by the field's name, and text, numbers and links after a
// muted name, since alone they say little. done tells whether the card's
// state is finished.
function cardChip(name, f, v, done) {
  if (f?.kind === "date") return dateChip(name, String(v).slice(0, 10), done);
  const chip = (...content) => h("span", { class: "card-chip", title: `${name}: ${v}` }, h("span", { class: "visually-hidden", text: `${name}: ` }), ...content);
  if (f?.kind === "bool") return chip(`✓ ${name}`);
  if (!f || f.kind === "enum") return chip(String(v));
  return chip(h("span", { class: "card-chip-name", "aria-hidden": "true", text: name }), " ", String(v));
}

// dateChip tells a date by how far off it is: today, tomorrow, in a few
// days, else the date itself. On a card not yet done, a date that has
// passed is overdue and today or tomorrow is near; a done card's dates
// are only dates.
function dateChip(name, day, done) {
  const tip = `${name}: ${day}`, said = h("span", { class: "visually-hidden", text: `${name}: ` });
  const days = daysFromToday(day);
  if (days === null) return h("span", { class: "card-chip", title: tip }, said, day);
  const [y, mo, d] = day.split("-").map(Number);
  let text = new Date(y, mo - 1, d).toLocaleDateString(language(), { day: "numeric", month: "short", year: y === new Date().getFullYear() ? undefined : "numeric" });
  let tone = "";
  if (!done && days < 0) [text, tone] = [t("card.overdue", { n: -days }), "late"];
  else if (days === 0) [text, tone] = [t("card.today"), done ? "" : "near"];
  else if (days === 1) [text, tone] = [t("card.tomorrow"), done ? "" : "near"];
  else if (days > 1 && days < 7) text = t("card.inDays", { n: days });
  return h("span", { class: "card-chip card-date" + (tone ? " " + tone : ""), title: tip }, lineIcon("M5 6h14v13H5zM5 10h14M9 4v4M15 4v4"), said, text);
}

// daysFromToday counts the days from today to a date (YYYY-MM-DD) in the
// reader's calendar: negative when it has passed, null for no date.
function daysFromToday(day) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!m) return null;
  const now = new Date();
  return Math.round((Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])) - Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())) / 86400000);
}

// localDay is the day of a moment in the reader's calendar, YYYY-MM-DD.
function localDay(iso) {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// dayName names a day: today, yesterday, else its weekday and date.
function dayName(day) {
  const n = daysFromToday(day);
  if (n === 0) return t("kanban.dayToday");
  if (n === -1) return t("kanban.dayYesterday");
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(language(), { weekday: "long", day: "numeric", month: "long", year: y === new Date().getFullYear() ? undefined : "numeric" });
}

// kanbanTones are the tones a kanban colours its cards with, named as the
// server names them, in the order values take them (ADR-0134).
const kanbanTones = ["blue", "green", "yellow", "orange", "red", "purple", "teal", "gray"];

// toneNames names the tones in the reader's language.
const toneNames = () => ({ blue: t("tone.blue"), green: t("tone.green"), yellow: t("tone.yellow"), orange: t("tone.orange"),
  red: t("tone.red"), purple: t("tone.purple"), teal: t("tone.teal"), gray: t("tone.gray") });

// recentDone is how many cards a finished column shows before one asks
// for all of them (ADR-0134).
const recentDone = 20;

// cardSigns tell what is behind a card (ADR-0132, ADR-0134, ADR-0137): a
// text, how far its checklist is, which are the task items of its text,
// and how many comments, relations and files it has.
function cardSigns(it) {
  const blocks = it.body || [];
  const checks = blocks.filter((b) => b.kind === "task");
  const ticked = checks.filter((b) => b.checked).length;
  const text = blocks.some((b) => b.kind !== "task" && !(b.kind === "paragraph" && !b.text));
  return [
    text ? h("span", { class: "card-sign card-has-text", role: "img", title: t("card.hasText"), "aria-label": t("card.hasText") },
      lineIcon("M5 6h14M5 10h14M5 14h14M5 18h9")) : null,
    checks.length ? h("span", { class: "card-sign card-checklist" + (ticked === checks.length ? " complete" : ""),
      title: t("card.checklist", { done: ticked, total: checks.length }) },
    lineIcon("M4 7l2 2 3-3M12 7h8M4 15l2 2 3-3M12 15h8"), `${ticked}/${checks.length}`) : null,
    it.commentCount ? h("span", { class: "card-sign card-comments", title: t("card.comments", { n: it.commentCount }) },
      lineIcon("M5 5h14v10H10l-5 4z"), h("span", { class: "visually-hidden", text: t("card.comments", { n: it.commentCount }) }),
      h("span", { "aria-hidden": "true", text: String(it.commentCount) })) : null,
    it.relationCount ? h("span", { class: "card-sign card-relations", title: t("card.relations", { n: it.relationCount }) },
      lineIcon("M10 14a4 4 0 0 0 6 0l3-3a4 4 0 0 0-6-6l-1 1M14 10a4 4 0 0 0-6 0l-3 3a4 4 0 0 0 6 6l1-1"), h("span", { class: "visually-hidden", text: t("card.relations", { n: it.relationCount }) }),
      h("span", { "aria-hidden": "true", text: String(it.relationCount) })) : null,
    it.fileCount ? h("span", { class: "card-sign card-files", title: t("card.files", { n: it.fileCount }) },
      lineIcon("M15 7l-6 6a2 2 0 0 0 3 3l6-6a4 4 0 0 0-6-6l-6 6a6 6 0 0 0 8 8l5-5"), h("span", { class: "visually-hidden", text: t("card.files", { n: it.fileCount }) }),
      h("span", { "aria-hidden": "true", text: String(it.fileCount) })) : null,
  ];
}

// kanbanSettings is the dialog that sets a kanban's name, picture,
// columns — the workflow's states, renamed and ordered, and columns of
// its own — and the fields its cards show (ADR-0076), its colours and
// its columns' limits (ADR-0134), and removes it (ADR-0132).
function kanbanSettings(k, type, columns, kpath) {
  const status = h("div", { class: "status" });
  const rows = h("ol", { class: "kanban-columns-edit", id: "kanban-columns" });
  const fill = (cols) => rows.replaceChildren(...cols.map((c, i) => {
    const name = h("input", { value: c.name, "aria-label": t("kanban.columnName"), required: true, maxlength: 40 });
    name.addEventListener("input", () => { c.name = name.value; });
    const move = (d) => () => { const j = i + d; if (j < 0 || j >= cols.length) return; [cols[i], cols[j]] = [cols[j], cols[i]]; fill(cols); };
    // A limit of cards, a signal when the column holds more (ADR-0134).
    const limit = h("input", { type: "number", class: "column-limit", min: 0, max: 999, step: 1, value: c.limit || "", placeholder: t("kanban.noLimit"),
      "aria-label": t("kanban.columnLimit", { column: c.name }), title: t("kanban.columnLimit", { column: c.name }) });
    limit.addEventListener("input", () => { c.limit = Math.max(0, Math.trunc(Number(limit.value) || 0)); });
    return h("li", { "data-column": c.id || "" }, name, limit,
      h("span", { class: "muted", text: c.state ? t("kanban.stateColumn", { state: c.state }) : t("kanban.ownColumn") }),
      h("button", { type: "button", class: "link", text: "↑", "aria-label": t("kanban.up"), onclick: move(-1) }),
      h("button", { type: "button", class: "link", text: "↓", "aria-label": t("kanban.down"), onclick: move(1) }),
      c.state ? "" : h("button", { type: "button", class: "link danger", text: t("kanban.removeColumn"), onclick: () => { cols.splice(i, 1); fill(cols); } }));
  }));
  const cols = columns.map((c) => ({ id: c.id, name: c.name, state: c.state || "", limit: c.limit || 0 }));
  fill(cols);
  const name = h("input", { id: "kanban-name", value: k.name, required: true, maxlength: 80 });
  const newColumn = h("input", { id: "kanban-new-column", placeholder: t("kanban.newColumn"), "aria-label": t("kanban.newColumn"), maxlength: 40 });
  const choices = [...type.fields.map((f) => f.name), "labels", "state"];
  const boxes = choices.map((n) => h("input", { type: "checkbox", value: n, checked: k.cardFields.includes(n) }));
  // The colour of the cards: a choice field of the type, and a tone for
  // each of its values (ADR-0134). The value is the task's own.
  const enums = type.fields.filter((f) => f.kind === "enum");
  const colorField = h("select", { id: "kanban-color-field", "aria-label": t("kanban.colors") },
    h("option", { value: "", text: t("kanban.colorNone") }), enums.map((f) => h("option", { value: f.name, text: f.name, selected: f.name === k.colorField })));
  const toneRows = h("div", { class: "kanban-tones", id: "kanban-tones" });
  let toneOf = {};
  const fillTones = () => {
    const f = enums.find((x) => x.name === colorField.value);
    toneOf = {};
    toneRows.replaceChildren(...(f ? f.values : []).map((v, i) => {
      toneOf[v] = (f.name === k.colorField && k.colors?.[v]) || kanbanTones[i % kanbanTones.length];
      const swatch = h("span", { class: `tone-swatch tone-${toneOf[v]}` });
      const pick = h("select", { "data-value": v, "aria-label": t("kanban.colorOf", { value: v }) },
        kanbanTones.map((tone) => h("option", { value: tone, text: toneNames()[tone], selected: tone === toneOf[v] })));
      pick.addEventListener("change", () => { toneOf[v] = pick.value; swatch.className = `tone-swatch tone-${pick.value}`; });
      return h("label", { class: "kanban-tone" }, swatch, h("span", { text: v }), pick);
    }));
  };
  colorField.addEventListener("change", fillTones);
  fillTones();
  // Swimlanes by a person or choice field of the task (ADR-0137).
  const laneChoices = type.fields.filter((f) => f.kind === "user" || f.kind === "enum");
  const laneField = h("select", { id: "kanban-lane-field", "aria-label": t("kanban.lanes") },
    h("option", { value: "", text: t("kanban.lanesNone") }), laneChoices.map((f) => h("option", { value: f.name, text: f.name, selected: f.name === k.laneField })));
  const picker = h("input", { type: "file", id: "kanban-icon-file", accept: "image/png,image/jpeg,image/gif,image/webp,image/svg+xml", "aria-label": t("kanban.iconChoose") });
  picker.addEventListener("change", () => action(async () => {
    const file = picker.files[0];
    if (!file) return;
    const res = await fetch(kpath + "/icon", { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: file });
    if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
  }, status));
  const dialog = h("dialog", { class: "kanban-settings", id: "kanban-settings", "aria-labelledby": "kanban-settings-title" },
    h("div", { class: "kanban-settings-head" },
      h("h2", { id: "kanban-settings-title", text: t("kanban.settings") }),
      h("button", { type: "button", class: "link", id: "kanban-settings-close", "aria-label": t("common.close"), text: "×", onclick: () => dialog.close() })),
    field(t("kanban.name"), name),
    h("h3", { text: t("kanban.icon") }),
    h("div", { class: "avatar-row", id: "kanban-icon" }, k.icon ? kanbanIcon(k, 64) : h("span", { class: "muted", text: t("kanban.iconNone") }),
      h("div", {},
        h("p", { class: "muted", text: t("kanban.iconHelp") }),
        h("label", { class: "upload" }, h("span", { text: t("kanban.iconChoose") }), picker),
        k.icon ? h("button", { type: "button", class: "link danger", id: "kanban-icon-remove", text: t("common.delete"), onclick: () => action(() => del(kpath + "/icon"), status) }) : "")),
    h("h3", { text: t("kanban.cardFields") }),
    h("p", { class: "muted", text: t("kanban.cardFieldsHelp") }),
    h("div", { class: "format-list", id: "kanban-card-fields" }, choices.map((n, i) => h("label", { class: "check" }, boxes[i], " ", n))),
    h("h3", { text: t("kanban.colors") }),
    h("p", { class: "muted", text: t("kanban.colorsHelp") }),
    enums.length ? h("div", {}, colorField, toneRows) : h("p", { class: "muted", text: t("kanban.colorsNoField", { type: type.name }) }),
    h("h3", { text: t("kanban.lanes") }),
    h("p", { class: "muted", text: t("kanban.lanesHelp") }),
    laneChoices.length ? laneField : h("p", { class: "muted", text: t("kanban.lanesNoField", { type: type.name }) }),
    h("h3", { text: t("kanban.columns") }),
    h("p", { class: "muted", text: t("kanban.columnsHelp") + " " + t("kanban.limitHelp") }),
    rows,
    h("p", { class: "inline-form" }, newColumn, h("button", { type: "button", id: "kanban-add-column", text: t("kanban.addColumn"), onclick: () => {
      if (!newColumn.value.trim()) return;
      cols.push({ name: newColumn.value.trim(), state: "", limit: 0 });
      newColumn.value = "";
      fill(cols);
    } })),
    status,
    h("p", { class: "actions-row" },
      h("button", { type: "button", class: "primary", id: "kanban-settings-save", text: t("common.save"), onclick: () => action(() => put(kpath, {
        name: name.value.trim(), cardFields: boxes.filter((b) => b.checked).map((b) => b.value),
        columns: cols.map((c) => ({ id: c.state ? "" : c.id || "", name: c.name.trim(), state: c.state, limit: c.limit || 0 })),
        colorField: colorField.value, colors: colorField.value ? toneOf : {}, laneField: laneField.value,
      }), status) }),
      h("button", { type: "button", text: t("common.cancel"), onclick: () => dialog.close() })),
    h("section", { class: "kanban-settings-remove" },
      h("h3", { text: t("kanban.remove") }),
      h("p", { class: "muted", text: t("kanban.removeHelp") }),
      h("button", { type: "button", class: "danger", id: "kanban-remove", text: t("kanban.remove"), onclick: () => confirm(t("kanban.removeConfirm", { name: k.name }))
        && action(async () => { await del(kpath); location.hash = "#/kanbans"; }, status) })));
  return dialog;
}

// albumView shows the images and drawings of every item one may read
// (ADR-0054); each leads to its item.
function albumView() {
  screen()(h("h1", { text: t("album.title") }), h("p", { class: "muted", text: t("album.intro") }), h("section", { id: "album" }, albumBrowser()));
}

// infoTip is an "i" that explains what it stands beside. text holds a
// first line and, after it, one line per point. It opens on a click (so it
// works on touch screens and from the keyboard) and closes on a second
// click, Escape or a click elsewhere. note, if given, closes it as a
// paragraph of its own.
let infoTips = 0;
function infoTip(label, text, note) {
  const [intro, ...points] = text.split("\n");
  const id = "info-tip-" + ++infoTips;
  const pop = h("div", { class: "info-pop", id, role: "note", hidden: true },
    h("p", { text: intro }), points.length ? h("ul", {}, points.map((p) => h("li", { text: p }))) : null,
    note ? h("p", { class: "muted", text: note }) : null);
  const button = h("button", { type: "button", class: "info-btn", "aria-label": t("common.infoAbout", { what: label }), "aria-expanded": "false", "aria-controls": id, text: "i" });
  const wrap = h("span", { class: "info-tip" }, button, pop);
  const close = () => { pop.hidden = true; button.setAttribute("aria-expanded", "false"); document.removeEventListener("click", outside, true); document.removeEventListener("keydown", esc); };
  const outside = (e) => { if (!wrap.contains(e.target)) close(); };
  const esc = (e) => { if (e.key === "Escape") { close(); button.focus(); } };
  button.addEventListener("click", () => {
    if (!pop.hidden) return close();
    pop.hidden = false;
    button.setAttribute("aria-expanded", "true");
    document.addEventListener("click", outside, true);
    document.addEventListener("keydown", esc);
  });
  return wrap;
}

// governanceOverview shows the governance check of every space one holds
// a role in, as counts that lead to each space's check (ADR-0040).
async function governanceOverview() {
  const show = screen();
  const { spaces } = await get("/api/spaces");
  const reports = await Promise.all(spaces.map((s) => get(`/api/spaces/${encodeURIComponent(s.key)}/governance`)));
  show(
    h("h1", { text: t("gov.title") }),
    h("p", { class: "muted", text: t("gov.intro") }),
    h("section", { id: "governance-overview" }, spaces.length
      ? filtered(h("table", {}, h("thead", {}, h("tr", {}, h("th", { text: t("col.space") }),
        h("th", {}, t("gov.colSensitive"), " ", infoTip(t("gov.colSensitive"), t("gov.infoSensitive"), t("gov.infoSensitiveNote"))),
        h("th", {}, t("gov.colStale"), " ", infoTip(t("gov.colStale"), t("gov.infoStale", { days: 365 }))),
        h("th", {}, t("gov.colImplausible"), " ", infoTip(t("gov.colImplausible"), t("gov.infoImplausible"))),
        h("th", {}, t("gov.colPersonal"), " ", infoTip(t("gov.colPersonal"), t("gov.infoPersonal"))),
        h("th", {}, t("gov.colReview"), " ", infoTip(t("gov.colReview"), t("gov.infoReview"))))),
        h("tbody", {}, spaces.map((s, i) => h("tr", {},
          h("td", {}, link("#/governance/" + encodeURIComponent(s.key), s.key), " ", h("span", { class: "muted", text: s.name })),
          h("td", { text: String(reports[i].sensitive.length) }),
          h("td", { text: String(reports[i].stale.length) }),
          h("td", { text: String(reports[i].implausible.length) }),
          h("td", { text: String((reports[i].personal || []).length) }),
          h("td", { text: String((reports[i].reviews || []).length) }),
        )))), { id: "governance-spaces", label: t("gov.title") })
      : h("p", { class: "muted", text: t("gov.noSpaces") })),
  );
}

// --- governance --------------------------------------------------------------

// governanceView checks a space (ADR-0040): sensitive data, shown masked;
// items not changed for a while; formal contradictions. Each finding names
// who is to act. Saving sensitive data has already restricted the item to
// its author; an administrator lifts that here after checking.
async function governanceView(key) {
  const show = screen();
  const days = Number(new URLSearchParams(location.hash.split("?")[1] || "").get("staleDays")) || 365;
  const rep = await get(`/api/spaces/${encodeURIComponent(key)}/governance?staleDays=${days}`);
  const reviews = rep.reviews || [];
  await learnNames([...rep.sensitive, ...rep.stale, ...rep.implausible, ...rep.personal, ...reviews].map((f) => f.responsible));
  const { regulations } = await get("/api/regulations").catch(() => ({ regulations: [] }));
  const regName = new Map(regulations.map((r) => [r.id, r.name.split(" – ")[0]]));
  const status = h("div", { class: "status" });
  const who = person;
  const itemCell = (f) => h("td", {}, link("#/item/" + encodeURIComponent(f.key), f.title || f.key), h("div", { class: "muted", text: f.key }));
  const lift = async (f) => {
    const { item } = await get("/api/items/" + f.item);
    await put(`/api/items/${f.item}/restriction`, { read: [], edit: item.restriction?.edit || [] });
  };
  // Each kind of finding is a list of its own, with its filter (ADR-0098).
  const table = (id, label, cols, rows) => rows.length
    ? filtered(h("table", {}, h("thead", {}, h("tr", {}, cols.map((c) => h("th", { text: c })))), h("tbody", {}, rows)),
      { id, label, choices: [{ key: "responsible", label: t("col.responsible") }] })
    : empty(t("common.nothing"));
  const daysInput = h("input", { type: "number", min: 1, max: 3650, value: days, "aria-label": t("gov.days"), class: "days" });
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("gov.crumb")),
    h("h1", { text: t("gov.heading", { key }) }),
    h("section", { id: "governance" },
      h("p", {}, h("button", { text: t("gov.checkAgain"), onclick: () => {
        location.hash = `#/governance/${encodeURIComponent(key)}?staleDays=${Number(daysInput.value) || 365}`;
        route();
      } }), t("gov.staleBefore"), daysInput, t("gov.staleAfter")),
      status,
      h("h2", {}, t("gov.sensitive", { n: rep.sensitive.length }), " ", infoTip(t("gov.colSensitive"), t("gov.infoSensitive"), t("gov.infoSensitiveNote"))),
      h("p", { class: "muted", text: t("gov.sensitiveNote") }),
      table("gov-sensitive", t("gov.colSensitive"), [t("col.item"), t("gov.where"), t("gov.what"), t("col.responsible"), t("gov.access")], rep.sensitive.map((f) => h("tr", { "data-f-responsible": who(f.responsible) },
        itemCell(f), h("td", { text: f.where }), h("td", {}, h("div", { text: f.kind }), h("code", { text: f.masked })),
        h("td", { text: who(f.responsible) }),
        h("td", {}, f.restricted
          ? [h("span", { text: t("gov.restricted") + " " }), h("button", { class: "link", text: t("gov.lift"), onclick: () => action(() => lift(f), status) })]
          : t("gov.open"))))),
      h("h2", {}, t("gov.stale", { days: rep.staleDays, n: rep.stale.length }), " ", infoTip(t("gov.colStale"), t("gov.infoStale", { days: rep.staleDays }))),
      table("gov-stale", t("gov.colStale"), [t("col.item"), t("gov.lastChange"), t("col.responsible")], rep.stale.map((f) => h("tr", { "data-f-responsible": who(f.responsible) },
        itemCell(f), h("td", { text: when(f.lastChange) }), h("td", { text: who(f.responsible) })))),
      h("h2", {}, t("gov.implausible", { n: rep.implausible.length }), " ", infoTip(t("gov.colImplausible"), t("gov.infoImplausible"))),
      table("gov-implausible", t("gov.colImplausible"), [t("col.item"), t("col.problem"), t("col.responsible")], rep.implausible.map((f) => h("tr", { "data-f-responsible": who(f.responsible) },
        itemCell(f), h("td", { text: f.problem }), h("td", { text: who(f.responsible) })))),
      // Items whose type asks for a periodic review (ADR-0074); whoever
      // may edit one records the review here.
      h("h2", { id: "reviews" }, t("gov.reviews", { n: reviews.length }), " ", infoTip(t("gov.colReview"), t("gov.infoReview"))),
      table("gov-reviews", t("gov.colReview"), [t("col.item"), t("gov.lastReview"), t("col.responsible"), ""], reviews.map((f) => h("tr", { "data-key": f.key, "data-f-responsible": who(f.responsible) },
        itemCell(f), h("td", { text: f.lastReview ? t("gov.reviewedDue", { last: when(f.lastReview), due: when(f.due) }) : t("gov.neverReviewed") }),
        h("td", { text: who(f.responsible) }),
        h("td", {}, h("button", { type: "button", class: "secondary mark-reviewed", text: t("gov.markReviewed"),
          onclick: () => action(() => post(`/api/items/${f.item}/reviewed`), status) }))))),
      h("h2", { id: "personal-data" }, t("gov.personal", { n: rep.personal.length }), " ", infoTip(t("gov.colPersonal"), t("gov.infoPersonal"))),
      table("gov-personal", t("gov.colPersonal"), [t("col.item"), t("gov.what"), t("dp.status"), t("col.responsible")], rep.personal.map((f) => h("tr", { class: f.held ? "held" : "", "data-f-responsible": who(f.responsible) },
        itemCell(f),
        h("td", {}, f.findings.map((x) => h("div", {}, h("span", { class: "chip", text: x.indicator }), ` ${x.category} (${regName.get(x.regulation) || x.regulation})`))),
        h("td", {}, f.held ? [h("strong", { text: t("dp.statusHeld") }), f.mayRelease ? [" · ", link("#/item/" + encodeURIComponent(f.key), t("dp.toRelease"))] : null] : t("dp.statusRestricted")),
        h("td", { text: who(f.responsible) })))),
    ),
  );
}

// --- link check --------------------------------------------------------------

// linksView shows the latest link check of a space (ADR-0039): the broken
// web links of items one may read, with who is to fix each. A space
// administrator starts a run; while it runs the view refreshes itself.
async function linksView(key) {
  const show = screen();
  const path = `/api/spaces/${encodeURIComponent(key)}/linkcheck`;
  const report = await get(path);
  await learnNames((report.broken || []).map((b) => b.responsible));
  const status = h("div", { class: "status" });
  const who = person;
  let body;
  if (!report.enabled) {
    body = h("p", { class: "notice", text: t("links.off") });
  } else {
    const start = h("button", { text: report.running ? t("links.checking") : t("links.check"), disabled: report.running,
      onclick: () => action(() => post(path), status) });
    const summary = !report.started ? t("links.never")
      : report.running ? t("links.since", { when: when(report.started) })
        : t(report.truncated ? "links.doneTruncated" : "links.done", { n: report.checked, when: when(report.finished) });
    body = h("div", {},
      h("p", {}, start, " ", h("span", { class: "muted", text: summary })),
      status,
      report.finished && !report.running
        ? (report.broken.length
          ? filtered(h("table", { class: "links" }, h("thead", {}, h("tr", {}, [t("links.usedIn"), t("links.link"), t("col.problem"), t("col.responsible")].map((c) => h("th", { text: c })))),
            h("tbody", {}, report.broken.map((b) => h("tr", { "data-f-responsible": b.responsible ? who(b.responsible) : "" },
              // The item the link is used in, and where in its body.
              h("td", {}, link("#/item/" + encodeURIComponent(b.key), b.title || b.key),
                h("div", { class: "muted", text: b.field === "origin" ? t("links.origin", { key: b.key })
                  : b.field ? t("links.field", { key: b.key, field: b.field }) : t("links.block", { key: b.key, n: b.block + 1 }) })),
              h("td", { class: "href", text: b.href }),
              h("td", { text: b.problem }),
              h("td", { text: b.responsible ? who(b.responsible) : "—" }),
            )))), { id: "links", label: t("links.link"), choices: [{ key: "responsible", label: t("col.responsible") }] })
          : empty(t("links.none")))
        : null,
    );
    if (report.running) {
      const timer = setTimeout(route, 2000);
      onLeave(() => clearTimeout(timer));
    }
  }
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("links.crumb")),
    h("h1", { text: t("links.heading", { key }) }),
    h("section", { id: "linkcheck" }, body),
  );
}

// propertyInput is the input for a field of a type; it reads back with
// readProperty.
function propertyInput(f, value) {
  const name = "p:" + f.name;
  switch (f.kind) {
    case "bool":
      return h("input", { name, type: "checkbox", checked: value === true });
    case "number":
      return h("input", { name, type: "number", step: "any", value: value ?? "" });
    case "date":
      return h("input", { name, type: "date", value: value ? String(value).slice(0, 10) : "" });
    case "enum":
      return h("select", { name },
        h("option", { value: "", text: "—" }),
        f.values.map((v) => h("option", { value: v, text: v, selected: v === value })));
    case "user":
      return userInput(name, value);
    case "url":
      return h("input", { name, type: "url", value: value ?? "", placeholder: "https://…" });
    default:
      return h("input", { name, value: value ?? "" });
  }
}

// userInput chooses a person by typing a few letters of the username; the
// field keeps the person's ID (ADR-0013) and shows the name. One list of
// suggestions serves every such field.
function userInput(name, value) {
  let options = document.getElementById("user-options");
  if (!options) {
    options = h("datalist", { id: "user-options" });
    document.body.append(options);
  }
  const input = h("input", { name, list: "user-options", value: value ? person(value) : "", autocomplete: "off", placeholder: t("user.search"), class: "user-input" });
  input.dataset.userId = value || "";
  const found = new Map();
  let timer = 0;
  input.addEventListener("input", () => {
    const typed = input.value.trim();
    input.dataset.userId = found.get(typed) || "";
    clearTimeout(timer);
    if (!typed || found.has(typed)) return;
    timer = setTimeout(async () => {
      const { users } = await get("/api/users/search?q=" + encodeURIComponent(typed)).catch(() => ({ users: [] }));
      for (const u of users) {
        found.set(u.username, u.id);
        names.set(u.id, u.username);
      }
      options.replaceChildren(...users.map((u) => h("option", { value: u.username })));
      input.dataset.userId = found.get(input.value.trim()) || "";
    }, 150);
  });
  return input;
}

// mentionable suggests people while "@name" is typed in a comment, and a
// choice completes the name; a comment that names someone so tells them
// under Notifications (ADR-0084). Without the right to look people up,
// names are typed in full.
function mentionable(area) {
  const list = h("ul", { class: "mention-list", hidden: true });
  let timer = 0;
  const typed = () => area.value.slice(0, area.selectionStart).match(/(?:^|\s)@([^\s@]{0,64})$/u)?.[1] ?? null;
  const choose = (name) => {
    const at = area.selectionStart;
    const before = area.value.slice(0, at).replace(/@[^\s@]*$/u, "@" + name + " ");
    area.value = before + area.value.slice(at);
    area.selectionStart = area.selectionEnd = before.length;
    list.hidden = true;
    area.focus();
  };
  area.addEventListener("input", () => {
    clearTimeout(timer);
    const q = typed();
    if (q === null) {
      list.hidden = true;
      return;
    }
    timer = setTimeout(async () => {
      const { users } = await get("/api/users/search?q=" + encodeURIComponent(q)).catch(() => ({ users: [] }));
      list.replaceChildren(...users.slice(0, 8).map((u) =>
        h("li", {}, h("button", { type: "button", class: "link", text: "@" + u.username, onclick: () => choose(u.username) }))));
      list.hidden = !users.length || typed() === null;
    }, 150);
  });
  area.addEventListener("keydown", (ev) => { if (ev.key === "Escape") list.hidden = true; });
  return h("div", { class: "mention-box" }, area, list);
}

function readProperty(f, el) {
  if (f.kind === "bool") return el.checked;
  if (f.kind === "user") {
    if (!el.value.trim()) return null;
    if (el.dataset.userId) return el.dataset.userId;
    throw new Error(t("user.unknown", { name: el.value.trim() }));
  }
  const v = el.value;
  if (v === "") return null;
  if (f.kind === "number") return Number(v);
  return v;
}

// newItemForm creates an item in a space, below parent (an item) if one is
// given; type, "page" by default, is preselected when it exists. With spaces, the
// space is chosen in the form.
function newItemForm({ space, spaces = null, types, parent = null, heading = t("item.new"), id = "new-item", type = "page" }) {
  if (types.length === 0) return null;
  const spaceSelect = spaces && spaces.length > 1
    ? h("select", { name: "space" }, favouritesFirst(spaces).map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})`, selected: s.key === space })))
    : null;
  const under = parent ? h("input", { type: "checkbox", name: "under", checked: true }) : null;
  // A page lies below its parent only in the parent's space.
  if (spaceSelect && under) {
    spaceSelect.addEventListener("change", () => {
      under.disabled = spaceSelect.value !== parent.space;
      if (under.disabled) under.checked = false;
    });
  }
  const fields = h("div");
  const typeSelect = h("select", { name: "type" }, types.map((ty) => h("option", { value: ty.name, text: ty.name, selected: ty.name === type })));
  const showFields = () => {
    const ty = types.find((x) => x.name === typeSelect.value);
    fields.replaceChildren(...ty.fields.map((f) => field(f.name + (f.required ? " *" : ""), propertyInput(f))));
  };
  typeSelect.addEventListener("change", showFields);
  showFields();
  const f = form({ class: "card", id }, [
    h("h2", { text: heading }),
    spaceSelect ? field(t("col.space"), spaceSelect) : null,
    under ? h("label", { class: "check" }, under, " ", t("new.under", { title: parent.title || parent.key })) : null,
    field(t("col.type"), typeSelect),
    field(t("col.title"), h("input", { name: "title", required: true })),
    fields,
    h("button", { type: "submit", text: t("common.create") }),
  ], async () => {
    const ty = types.find((x) => x.name === typeSelect.value);
    const properties = {};
    for (const fd of ty.fields) {
      const v = readProperty(fd, f.querySelector(`[name="p:${CSS.escape(fd.name)}"]`));
      if (v !== null) properties[fd.name] = v;
    }
    const target = spaceSelect ? spaceSelect.value : space;
    const below = under && under.checked && !under.disabled ? parent.id : "";
    const { item } = await post("/api/items", { space: target, type: ty.name, parent: below, title: f.querySelector("[name=title]").value, properties });
    // A new item with a body opens for writing (ADR-0051).
    location.hash = "#/item/" + encodeURIComponent(item.key) + (ty.body ? "?edit" : "");
  });
  return f;
}

// --- one item ---------------------------------------------------------------

// itemView shows an item for reading; with ?edit its title and body are
// edited in place, in the live session (ADR-0051). The column on the right
// holds what belongs to the item besides its text: owner and details,
// comments, files, relations, access and history.
async function itemView(ref, params = new URLSearchParams()) {
  const show = screen();
  const { item, editable, may = {} } = await get("/api/items/" + encodeURIComponent(ref));
  const path = "/api/items/" + item.id;
  const [{ types }, history, out, inc, embeds, comments, { attachments }, { watches }, { spaces }, myNotes] = await Promise.all([
    get("/api/types"),
    get(path + "/history"),
    get(path + "/relations?direction=outgoing"),
    get(path + "/relations?direction=incoming"),
    get(path + "/embeds"),
    get(path + "/comments"),
    get(path + "/attachments"),
    get("/api/me/watches"),
    get("/api/spaces"),
    get(path + "/notes").catch(() => ({ notes: [] })),
  ]);
  const spaceAdmin = spaces.find((sp) => sp.key === item.space)?.role === "space-admin";
  const type = types.find((ty) => ty.name === item.type) || { fields: [], workflow: null, body: false };
  const level = `/api/spaces/${encodeURIComponent(item.space)}/tree` + (item.parent ? "?parent=" + item.parent : "");
  const [, open, siblings] = await Promise.all([
    learnNames([...history.events.map((e) => e.actor), ...type.fields.filter((f) => f.kind === "user").map((f) => item.properties[f.name]), ...comments.comments.flatMap((c) => c.resolvedBy ? [c.author, c.resolvedBy] : [c.author]), ...attachments.map((a) => a.author),
      ...(item.secrets || []).flatMap((sc) => [sc.by, ...(sc.readers || []), ...(sc.requests || [])]).filter((p) => !p.startsWith("@"))]),
    ancestors(item),
    item.presence === "live" ? get(level) : { items: [] },
  ]);
  const status = h("div", { class: "status" });
  const live = item.presence === "live";
  // Changes are offered only to who may make them (role, restriction,
  // import hold); the server decides again on every request.
  const canEdit = live && editable;
  // The text is changed by those who read all of its protected passages,
  // so nothing they cannot read is lost (ADR-0088); fields, state, labels
  // and comments stay theirs to change.
  const readsAll = item.readsAllSecrets !== false;
  const editing = canEdit && readsAll && params.has("edit");
  const itemHref = "#/item/" + encodeURIComponent(item.key);
  here.space = item.space;
  here.parent = item.id;
  const threads = commentThreads(path, comments, canEdit);
  // One's own notes on passages, seen by no one else (ADR-0107).
  const notes = live ? personalNotes(path, myNotes.notes) : null;
  // Who edits the text now, as faces beside the bell, for readers too
  // (ADR-0115).
  const editors = h("span", { class: "live-editors", id: "live-editors", role: "status", hidden: true });
  const showEditors = async (participants) => {
    const others = participants.filter((p) => p.principal !== me.user.id);
    await learnNames(others.map((p) => p.principal)).catch(() => {});
    editors.replaceChildren(...others.map((p) => {
      const name = t("live.holder", { name: p.name });
      const f = face(p.principal, 32);
      return h("span", { class: "live-editor", "data-principal": p.principal, title: name, role: "img", "aria-label": name }, f);
    }));
    editors.hidden = others.length === 0;
  };

  // Moving up or down among the siblings: the keyboard's way to reorder,
  // besides dragging in the tree (ADR-0041).
  const at = siblings.items.findIndex((n) => n.id === item.id);
  const place = (before) => action(() => post(path + "/move", { space: item.space, parent: item.parent || "", before }), status);
  const reorder = [];
  if (canEdit && at > 0) reorder.push(h("button", { text: t("item.moveUp"), onclick: () => place(siblings.items[at - 1].id) }));
  if (canEdit && at >= 0 && (at + 1 < siblings.items.length)) {
    const after = siblings.items[at + 2];
    if (after || !siblings.next) reorder.push(h("button", { text: t("item.moveDown"), onclick: () => place(after ? after.id : "") }));
  }

  const restricted = (item.restriction?.read?.length || 0) + (item.restriction?.edit?.length || 0) > 0;
  // While editing, the title is a field saved with Done, or with Enter.
  const titleInput = editing ? h("input", { name: "title", value: item.title, class: "title", required: true, "aria-label": t("col.title") }) : null;
  const saveTitle = async () => {
    const next = titleInput.value.trim();
    if (next && next !== item.title) {
      await put(path + "/title", { title: next });
      item.title = next;
    }
  };
  const title = editing
    ? form({ class: "inline", id: "title" }, [titleInput], saveTitle)
    : h("h1", { class: "item-title" }, restricted ? lockIcon(true) : null, item.title || t("item.erased"));

  // A field is saved as soon as it changes; a mark beside it says so.
  const props = h("table", { class: "props" }, type.fields.map((f) => {
    const input = propertyInput(f, item.properties[f.name]);
    input.disabled = !canEdit;
    const mark = h("span", { class: "save-mark", role: "status" });
    if (canEdit) {
      input.addEventListener("change", async () => {
        mark.className = "saving";
        mark.textContent = t("common.saving");
        try {
          await put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: readProperty(f, input) });
          mark.className = "saved";
          mark.textContent = "✓ " + t("common.saved");
        } catch (err) {
          mark.className = "error";
          mark.textContent = err.message;
        }
      });
    }
    return h("tr", { "data-field": f.name },
      h("th", { text: f.name }),
      h("td", {}, input),
      h("td", {}, mark,
        // A user field names a person by ID; show who it is, and let one
        // put oneself in (ADR-0049).
        f.kind === "user" && item.properties[f.name] ? h("span", { class: "muted", text: " " + person(item.properties[f.name]) }) : null,
        f.kind === "user" && canEdit && item.properties[f.name] !== me.user.id
          ? h("button", { class: "link", text: t("tasks.assignMe"), onclick: () => action(() => put(`${path}/properties/${encodeURIComponent(f.name)}`, { value: me.user.id }), status) }) : null,
        // A web address opens in a new tab (ADR-0074).
        f.kind === "url" && item.properties[f.name]
          ? h("a", { href: item.properties[f.name], target: "_blank", rel: "noopener noreferrer", class: "prop-link", text: t("item.openLink") }) : null),
    );
  }));

  const stateBar = type.workflow && item.state ? statusBar(type.workflow, item.state, canEdit
    ? (tr) => moveItem(type.workflow, item, tr, () => post(path + "/transitions", { name: tr.name }), status) : null) : null;

  // Only what the server allows is offered (may): trashing needs every
  // page below editable, purging a space admin.
  const actions = canEdit
    ? [may.trash ? h("button", { text: t("item.trash"), onclick: () => action(() => post(path + "/trash"), status) }) : null].filter(Boolean)
    : item.presence === "trashed"
      ? [may.restore ? h("button", { text: t("item.restore"), onclick: () => action(() => post(path + "/restore"), status) }) : null,
        may.purge ? h("button", { class: "danger", text: t("item.purge"), onclick: () => confirm(t("item.purgeConfirm")) && action(() => post(path + "/purge"), status) }) : null].filter(Boolean)
      : [];

  const lastRestriction = history.events.filter((e) => e.kind === "RestrictionSet").pop();
  const guarded = item.restriction?.read?.length && lastRestriction?.actor === "governance";
  // The tree opens down to the page and shows the page's own children.
  const tree = pageTree({ space: item.space, current: item.id, open: [...open, item.id], onMoved: () => route(), onError: (err) => status.replaceChildren(errorBox(err)) });
  // Leaving the editor stores what is not yet saved, then shows the page.
  let session = null;
  const done = h("button", { type: "button", id: "edit-done", class: "primary", text: t("item.done"), onclick: async () => {
    done.disabled = true;
    // A title that cannot be saved keeps the editor open, with the reason.
    try {
      await saveTitle();
    } catch (err) {
      status.replaceChildren(errorBox(err));
      done.disabled = false;
      return;
    }
    try { if (session) await session.finish(); } finally { location.hash = itemHref; }
  } });
  // The item's buttons, and the bell always last (ADR-0112).
  const headActions = h("p", { class: "actions" },
    // Editing is offered, and ended, in the bar where the text begins
    // (ADR-0091); an item without a text ends it here.
    editing && !type.body ? done : null,
    canEdit && !editing ? link(newHref(item.space, item.id), t("item.newChild"), "button", "item-new-child") : null,
    !editing ? exportMenu(item) : null,
    live && type.body ? editors : null,
    live ? watchButton(path, watches.some((w) => w.item === item.id), status) : null);
  let body = null;
  const protecting = h("div", { id: "protect-passage" });
  if (!type.body && canEdit && readsAll && !editing) {
    // An item without a text still has its bar, with Edit, in the same place.
    body = h("section", { id: "body", class: "reading no-body" },
      h("div", { class: "pm-toolbar pm-readbar", role: "toolbar" }, link(itemHref + "?edit", t("item.edit"), "button primary", "edit")));
  }
  if (type.body) {
    // Protected passages show readable or veiled; one who may change the
    // text protects a passage from what they read (ADR-0088).
    const secretOf = new Map((item.secrets || []).map((sc) => [sc.id, sc]));
    setSecretClass((ref) => (secretOf.get(ref)?.readable ? "secret-readable" : "secret-veiled"));
    const protect = canEdit && readsAll && live && !editing ? (at) => protectPassageForm(protecting, path, item, at, status) : null;
    const edit = editing ? done : canEdit && readsAll ? link(itemHref + "?edit", t("item.edit"), "button primary", "edit") : null;
    const b = bodySection(item, embeds.embeds, editing, threads, attachments, protect, edit, notes, showEditors);
    body = b.element;
    session = b.session;
    // A reader follows the text: who edits it, and each version saved by
    // someone else shows without a reload (ADR-0115). A selection made to
    // comment or note is not taken away; the text follows after it.
    if (!editing && live && readsAll) {
      let timer = null;
      const redraw = async () => {
        const sel = document.getSelection();
        if (sel && !sel.isCollapsed && body.contains(sel.anchorNode)) return void (timer = setTimeout(redraw, 2000));
        const [{ item: fresh }, fresher] = await Promise.all([get(path), get(path + "/embeds")]);
        const next = bodySection(fresh, fresher.embeds, false, threads, attachments, protect, edit, notes, showEditors).element;
        body.replaceWith(next);
        body = next;
      };
      const stop = watchLive(item.id, {
        onPresence: (ps) => { showEditors(ps); },
        onCommitted: (e) => { if (e.by !== me.user.id) { clearTimeout(timer); timer = setTimeout(() => redraw().catch(() => {}), 300); } },
      });
      onLeave(() => { stop(); clearTimeout(timer); });
    }
  }
  const card = (id, ...content) => h("section", { class: "side-card", id }, ...content);
  const details = card("details",
    h("h2", { text: t("item.properties") }),
    stateBar,
    type.fields.length ? props : null,
    labelsLine(path, item, canEdit));
  // The task of a linked item, worked at its engine as oneself (ADR-0151).
  const engineTask = item.linked && live ? taskPanel(item, () => route()) : null;
  // Copying and moving a page with its child pages, to any place one may
  // create in (ADR-0067); dragging in the tree moves within a space.
  const relocate = h("div", { id: "relocate" });
  const relocateButtons = live ? [
    h("button", { type: "button", id: "item-copy", text: t("copy.open"), onclick: () => relocateForm(relocate, item, "copy", spaces, status) }),
    canEdit ? h("button", { type: "button", id: "item-move", text: t("move.open"), onclick: () => relocateForm(relocate, item, "move", spaces, status) }) : null,
  ] : [];
  const more = reorder.length || actions.length || relocateButtons.length
    ? card("item-actions", h("p", { class: "actions-row" }, reorder, relocateButtons, actions), relocate) : null;
  show(withTree(tree,
    h("p", { class: "crumbs" }, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(item.space), item.space), " / " + item.key),
    h("div", { class: "page-head" }, title, headActions),
    guarded ? h("p", { class: "notice", text: t("item.guarded") }) : null,
    item.hold ? await holdBanner(item, path, canEdit) : null,
    h("p", { class: "muted" }, t("item.meta", { type: item.type, presence: item.presence }), item.state ? t("item.state", { state: item.state }) : "", t("item.version", { n: item.version })),
    // An item mirrored from another system says where it is kept, the
    // system of record (ADR-0023, ADR-0074).
    item.origin?.system === "git:file" ? fileOrigin(item, path, canEdit, status) : item.origin ? h("p", { class: "notice origin", id: "item-origin" },
      t("item.origin", { system: item.origin.system }), " ",
      item.origin.url ? h("a", { href: item.origin.url, target: "_blank", rel: "noopener noreferrer", text: t("item.originOpen") }) : "",
      item.managed ? " · " + t("item.managed") : "") : "",
    status,
    h("div", { class: "item-layout" },
      // The text has the main column to itself; the owner and details
      // lead the column beside it (ADR-0102).
      h("div", { class: "item-main" }, protecting, body || h("p", { class: "muted", text: t("item.noBody") })),
      h("aside", { class: "item-side", "aria-label": t("item.side") },
        columnToggle("side"),
        engineTask,
        details,
        secretsSection(path, item, status),
        threads.section,
        notes ? notes.section : null,
        attachmentsSection(path, attachments, canEdit),
        await relationsSection(path, out.relations, inc.relations, item.id, canEdit),
        await accessSection(path, item, canEdit, spaceAdmin),
        historySection(path, history.events, canEdit, readsAll),
        more)),
  ));
}

// relocateForm copies or moves an item (ADR-0067): to a space one may edit
// in, below a page there or at its top; a copy takes a title and, if
// wished, the child pages. The parent list holds the space's pages, found
// by typing.
async function relocateForm(box, item, mode, spaces, status) {
  const editable = spaces.filter((s) => mayEdit(s.role));
  const spaceSelect = h("select", { name: "space", id: "relocate-space" }, favouritesFirst(editable).map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})`, selected: s.key === item.space })));
  const parentSelect = h("select", { name: "parent", id: "relocate-parent" });
  const fillParents = async () => {
    const { items } = await get(`/api/items?space=${encodeURIComponent(spaceSelect.value)}&limit=500`);
    parentSelect.replaceChildren(
      h("option", { value: "", text: t("move.top") }),
      ...items.filter((it) => it.id !== item.id).sort((a, b) => a.title.localeCompare(b.title))
        .map((it) => h("option", { value: it.id, text: `${it.title} (${it.key})`, selected: mode === "copy" && it.id === item.parent })));
  };
  spaceSelect.addEventListener("change", () => action(fillParents, status));
  await fillParents();
  const copying = mode === "copy";
  box.replaceChildren(form({ id: "relocate-form", class: "relocate-form" }, [
    h("h3", { text: copying ? t("copy.title", { title: item.title }) : t("move.title", { title: item.title }) }),
    field(t("col.space"), spaceSelect),
    field(t("move.parent"), parentSelect),
    copying ? field(t("col.title"), h("input", { name: "title", id: "relocate-title", required: true, value: t("copy.defaultTitle", { title: item.title }) })) : "",
    copying ? h("label", { class: "check" }, h("input", { type: "checkbox", name: "children", id: "relocate-children", checked: true }), " ", t("copy.children")) : "",
    copying ? "" : h("p", { class: "muted", text: t("move.note") }),
    h("p", { class: "actions-row" },
      h("button", { type: "submit", text: copying ? t("copy.submit") : t("move.submit") }),
      h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => box.replaceChildren() })),
  ], async (data) => {
    if (copying) {
      const res = await post(`/api/items/${item.id}/copy`, { space: data.get("space"), parent: data.get("parent"), title: data.get("title").trim(), children: Boolean(data.get("children")) });
      location.hash = "#/item/" + encodeURIComponent(res.item.key);
    } else {
      await post(`/api/items/${item.id}/move`, { space: data.get("space"), parent: data.get("parent"), before: "" });
      route();
    }
  }));
  box.querySelector("select")?.focus();
}

// bodySection shows an item's body: for reading, with the passages
// commented on highlighted and commenting on a selection offered; or, when
// editing, in its live session (ADR-0010), where changes reach everyone at
// once and are saved as one leaves a block.
function bodySection(item, embeds, editing, threads, attachments, protect, edit, notes, onPresence) {
  const resolve = async (ref) => (await get("/api/items/" + encodeURIComponent(ref))).item;
  if (!editing) {
    // The files are known, so a drawing shows its processes live (ADR-0142).
    const view = createEditor({ blocks: item.body, embeds, editable: false, resolve, childPages: childPagesOf(item), files: filesOf(item, attachments),
      extra: item.presence === "live" ? [threads.plugin, ...(notes ? [notes.plugin] : [])] : [], comment: item.presence === "live" ? threads.start : undefined,
      note: notes ? notes.start : undefined, protect: protect || undefined,
      actions: edit ? [edit] : [], embedContent: embedLoader([item.id]), tasks: taskTools(item, false) });
    const section = h("section", { id: "body", class: "reading" }, view.element);
    // A click on a protected passage shows it in the list of passages.
    const open = (ev) => {
      const el = ev.target.closest?.(".pm-secret");
      if (!el || (ev.type === "keydown" && ev.key !== "Enter")) return;
      const entry = document.querySelector(`#secrets li[data-secret="${CSS.escape(el.dataset.secret)}"]`);
      if (!entry) return;
      entry.scrollIntoView({ block: "nearest" });
      entry.classList.add("secret-focus");
      setTimeout(() => entry.classList.remove("secret-focus"), 1500);
      entry.querySelector("button")?.focus();
    };
    section.addEventListener("click", open);
    section.addEventListener("keydown", open);
    return { element: section, session: null };
  }
  const session = liveBody({ ref: item.id, embeds, resolve, childPages: childPagesOf(item), files: filesOf(item, attachments),
    extra: [threads.plugin], comment: threads.start, actions: edit ? [edit] : [], embedContent: embedLoader([item.id]), tasks: taskTools(item, true), markdown: markdownTools(item), onPresence });
  onLeave(session.close);
  return { element: h("section", { id: "body", class: "editing" }, session.element), session };
}

// principalPicker chooses people and groups, found by typing, as the
// protection of an item does (ADR-0078); values() are the user IDs and
// @groups chosen.
function principalPicker(id, initial) {
  let chosen = [...initial];
  const list = h("ul", { class: "access-entries", id: id + "-entries" });
  const draw = () => list.replaceChildren(...chosen.map((p) => h("li", { class: "chip access-entry" + (p.startsWith("@") ? " group" : ""), "data-principal": p },
    h("span", { text: p.startsWith("@") ? p : person(p) }),
    h("button", { type: "button", class: "link", text: "×", "aria-label": t("access.removeWho", { who: p.startsWith("@") ? p : person(p) }),
      onclick: () => { chosen = chosen.filter((x) => x !== p); draw(); } }))));
  draw();
  const options = h("datalist", { id: id + "-options" });
  const found = new Map();
  const who = h("input", { id: id + "-who", list: id + "-options", autocomplete: "off", placeholder: t("access.whoPlaceholder"), "aria-label": t("access.who") });
  let timer = 0;
  who.addEventListener("input", () => {
    clearTimeout(timer);
    const typed = who.value.trim().replace(/^@/, "");
    if (!typed || found.has(who.value.trim())) return;
    timer = setTimeout(async () => {
      const q = encodeURIComponent(typed);
      const [{ users }, { groups }] = await Promise.all([
        get("/api/users/search?q=" + q).catch(() => ({ users: [] })), get("/api/groups/search?q=" + q).catch(() => ({ groups: [] }))]);
      for (const u of users) { found.set(u.username, u.id); names.set(u.id, u.username); }
      for (const g of groups) found.set("@" + g, "@" + g);
      options.replaceChildren(...users.map((u) => h("option", { value: u.username })), ...groups.map((g) => h("option", { value: "@" + g, label: t("access.group", { name: g }) })));
    }, 150);
  });
  const note = h("p", { class: "muted", role: "status" });
  const add = () => {
    const p = found.get(who.value.trim());
    if (!p) { note.textContent = t("access.unknown"); return; }
    note.textContent = "";
    if (!chosen.includes(p)) chosen.push(p);
    who.value = "";
    draw();
  };
  who.addEventListener("keydown", (ev) => { if (ev.key === "Enter") { ev.preventDefault(); add(); } });
  return {
    element: h("div", { class: "principal-picker" }, list,
      h("p", { class: "inline-form" }, who, options, h("button", { type: "button", id: id + "-add", text: t("access.add"), onclick: add })), note),
    values: () => chosen,
  };
}

// protectPassageForm protects the selected passage (ADR-0088): it names
// the text, and who reads it besides the one who protects it.
function protectPassageForm(box, path, item, at, status) {
  const text = Array.from(item.body[at.block]?.text || "").slice(at.start, at.end).join("");
  const picker = principalPicker("secret-readers", []);
  box.replaceChildren(h("section", { class: "card protect-passage" },
    h("h2", { text: t("secret.protectTitle") }),
    h("blockquote", { class: "comment-quote", text: text }),
    h("p", { class: "muted", text: t("secret.protectHelp") }),
    picker.element,
    h("p", { class: "actions-row" },
      h("button", { type: "button", class: "primary", id: "protect-save", text: t("secret.protect"),
        onclick: () => action(() => post(path + "/secrets", { ...at, readers: picker.values() }), status) }),
      h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => box.replaceChildren() }))));
  box.scrollIntoView({ block: "nearest" });
}

// secretsSection lists an item's protected passages as the reader sees
// them (ADR-0088): who reads each; for one veiled, who decides and a
// button to ask; for one who decides, the requests to answer, the readers
// to change and the protection to take off.
function secretsSection(path, item, status) {
  const secrets = item.secrets || [];
  if (!secrets.length) return null;
  const named = (p) => (p.startsWith("@") ? t("access.group", { name: p.slice(1) }) : person(p));
  const base = (sc) => `${path}/secrets/${encodeURIComponent(sc.id)}`;
  const entry = (sc, i) => {
    const readers = [sc.by, ...(sc.readers || [])].map(named).join(", ");
    const parts = [h("strong", { text: t("secret.passage", { n: i + 1 }) }), " ",
      h("span", { class: "chip " + (sc.readable ? "secret-chip-readable" : "secret-chip-veiled"), text: sc.readable ? t("secret.readable") : t("secret.veiled") })];
    if (sc.readable || sc.decides) parts.push(h("p", { class: "muted", text: t("secret.readers", { who: readers }) }));
    if (!sc.readable) {
      parts.push(h("p", { class: "muted", text: t("secret.decidedBy", { who: named(sc.by) }) }));
      parts.push(sc.requested
        ? h("p", { class: "notice", text: t("secret.waiting") })
        : h("button", { type: "button", class: "secondary secret-request", text: t("secret.request"), onclick: () => action(() => post(base(sc) + "/request"), status) }));
    }
    if (sc.decides) {
      for (const p of sc.requests || []) {
        parts.push(h("p", { class: "secret-ask", "data-principal": p }, t("secret.asks", { who: named(p) }), " ",
          h("button", { type: "button", class: "primary secret-grant", text: t("secret.grant"), onclick: () => action(() => post(`${base(sc)}/requests/${encodeURIComponent(p)}`, { grant: true }), status) }),
          h("button", { type: "button", class: "secret-decline", text: t("secret.decline"), onclick: () => action(() => post(`${base(sc)}/requests/${encodeURIComponent(p)}`, { grant: false }), status) })));
      }
      const picker = principalPicker("secret-" + sc.id, sc.readers || []);
      parts.push(h("details", { class: "secret-edit" }, h("summary", { text: t("secret.change") }),
        picker.element,
        h("p", { class: "actions-row" },
          h("button", { type: "button", text: t("common.save"), onclick: () => action(() => put(base(sc), { readers: picker.values() }), status) }),
          h("button", { type: "button", class: "danger secret-unprotect", text: t("secret.unprotect"),
            onclick: () => confirm(t("secret.unprotectConfirm")) && action(() => del(base(sc)), status) }),
          // Erasing destroys the passage's own key: no version holds it
          // any more (ADR-0089).
          h("button", { type: "button", class: "danger secret-erase", text: t("secret.erase"),
            onclick: () => confirm(t("secret.eraseConfirm")) && action(() => post(base(sc) + "/erase"), status) }))));
    }
    return h("li", { class: "secret-entry", "data-secret": sc.id }, ...parts);
  };
  return h("section", { id: "secrets" },
    h("h2", { text: t("secret.title", { n: secrets.length }) }),
    item.readsAllSecrets === false ? h("p", { class: "muted", text: t("secret.cannotEdit") }) : null,
    filtered(h("ul", { class: "list secret-list" }, secrets.map(entry)), { id: "secrets", label: t("secret.title", { n: secrets.length }) }));
}

// relationsSection lists an item's relations both ways, with the related
// items' keys and titles. One who may edit the item relates it to another
// by key and removes relations made by hand; those derived from a body
// change with the body.
async function relationsSection(path, out, inc, self, canEdit) {
  const others = [...new Set([...out.map((r) => r.to), ...inc.map((r) => r.from)])].slice(0, 50);
  const known = new Map(await Promise.all(others.map(async (id) => [id, await get("/api/items/" + id).then((x) => x.item).catch(() => null)])));
  const label = (id) => { const it = known.get(id); return it ? `${it.key} ${it.title}` : id.slice(0, 8); };
  const status = h("div", { class: "status" });
  const row = (r, other, outgoing) => h("li", { "data-f-type": r.type },
    h("span", { class: "muted", text: (outgoing ? r.type : t("rel.incoming", { type: r.type })) + (r.derived ? t("rel.fromBody") : "") + " " }),
    link("#/item/" + encodeURIComponent(known.get(other)?.key || other), label(other)),
    canEdit && outgoing && !r.derived ? h("button", { type: "button", class: "link", text: "×", title: t("rel.remove"), "aria-label": t("rel.remove"),
      onclick: () => action(() => del("/api/relations/" + encodeURIComponent(r.id)), status) }) : null);
  const add = canEdit ? form({ id: "add-relation", class: "inline" }, [
    h("input", { name: "type", required: true, list: "relation-types", placeholder: t("rel.type"), "aria-label": t("rel.type"), size: 12 }),
    h("datalist", { id: "relation-types" }, ["relates-to", "blocks", "implements", "duplicates", "depends-on"].map((v) => h("option", { value: v }))),
    h("input", { name: "target", required: true, placeholder: t("rel.target"), "aria-label": t("rel.target"), size: 12 }),
    h("button", { type: "submit", text: t("rel.add") }),
  ], async (d) => {
    const target = (await get("/api/items/" + encodeURIComponent(d.get("target").trim()))).item;
    await post("/api/relations", { type: d.get("type").trim(), from: self, to: target.id });
    route();
  }) : null;
  if (out.length + inc.length === 0 && !add) return null;
  return h("section", { id: "relations" },
    h("h2", { text: t("rel.title") }),
    out.length + inc.length ? filtered(h("ul", { class: "list" }, out.map((r) => row(r, r.to, true)), inc.map((r) => row(r, r.from, false))),
      { id: "relations", label: t("rel.title"), choices: [{ key: "type", label: t("rel.type") }] })
      : empty(t("rel.none")),
    add, status);
}

// accessSection shows who may read and edit an item and lets one who may
// edit it protect it: for people and groups, either for editing only or
// for reading and editing, without locking themselves out; it holds for
// the item's children too (ADR-0022, ADR-0078). Holders of the erasure
// role redact an item at once (ADR-0026).
async function accessSection(path, item, canEdit, spaceAdmin) {
  const r = item.restriction || { read: [], edit: [] };
  const read = r.read || [], edit = r.edit || [];
  await learnNames([...read, ...edit].filter((p) => !p.startsWith("@")));
  const status = h("div", { class: "status" });
  const named = (p) => p.startsWith("@") ? t("access.group", { name: p.slice(1) }) : person(p);
  const listed = (list) => list.length ? list.map(named).join(", ") : t("access.everyone");
  const set = (next) => action(() => put(path + "/restriction", next), status);
  const restricted = read.length + edit.length > 0;
  return h("section", { id: "access", class: "side-card " + (restricted ? "access-restricted" : "access-open") },
    // Open is the normal case; a restriction shows as a closed lock on a
    // coloured bar (ADR-0051).
    h("h2", { class: "access-bar" }, lockIcon(restricted), restricted ? t("access.restricted") : t("access.open")),
    h("p", {}, t("access.read", { who: listed(read) }), h("br"), t("access.edit", { who: listed(edit) })),
    canEdit ? h("p", { class: "actions-row" },
      h("button", { type: "button", class: "secondary", text: t("access.onlyMe"), onclick: () => set({ read: [me.user.id], edit: [me.user.id] }) }),
      h("button", { type: "button", class: "secondary", text: t("access.onlyMeEdits"), onclick: () => set({ read: read, edit: [me.user.id] }) }),
      restricted ? h("button", { type: "button", class: "secondary", text: t("access.lift"), onclick: () => set({ read: [], edit: [] }) }) : null) : null,
    canEdit ? protectForm(path, read, edit, spaceAdmin, named) : null,
    me.erasure && item.presence !== "purged" ? h("p", {}, h("button", { type: "button", class: "danger", text: t("access.redact"), onclick: () => {
      const reason = prompt(t("access.redactReason"));
      if (reason && reason.trim() && confirm(t("access.redactConfirm", { key: item.key }))) action(() => post(path + "/redact", { reason: reason.trim() }), status);
    } })) : null,
    status);
}

// protectForm chooses who an item is protected for: people and groups,
// found by typing, each allowed to read only or to edit too. "Edit only"
// leaves reading open to the space. One who is not a space administrator
// stays on the lists, as the server refuses a lock-out.
function protectForm(path, read, edit, spaceAdmin, named) {
  // An entry is a person (user ID) or a group ("@name") that may edit or
  // only read.
  let mode = read.length ? "both" : edit.length ? "edit" : "both";
  let entries = (mode === "both" ? read : edit).map((p) => ({ p, edit: mode === "edit" || !edit.length || edit.includes(p) }));
  for (const p of edit) if (!entries.some((e) => e.p === p)) entries.push({ p, edit: true });
  const list = h("ul", { class: "access-entries", id: "access-entries" });
  const modes = h("div", { class: "access-modes" }, ["both", "edit"].map((m) => h("label", { class: "check" },
    h("input", { type: "radio", name: "access-mode", value: m, checked: m === mode, onchange: () => { mode = m; draw(); } }), " ", { both: t("access.mode.both"), edit: t("access.mode.edit") }[m])));
  const draw = () => list.replaceChildren(...entries.map((e) => h("li", { class: "chip access-entry" + (e.p.startsWith("@") ? " group" : ""), "data-principal": e.p },
    h("span", { text: named(e.p) }),
    mode === "both" ? h("label", { class: "check" }, h("input", { type: "checkbox", checked: e.edit, onchange: (ev) => { e.edit = ev.target.checked; } }), " ", t("access.mayEditShort")) : "",
    h("button", { type: "button", class: "link", text: "×", "aria-label": t("access.removeWho", { who: named(e.p) }), onclick: () => { entries = entries.filter((x) => x !== e); draw(); } }))));
  draw();
  // People and groups are offered as one list while typing.
  const options = h("datalist", { id: "access-options" });
  const found = new Map();
  const who = h("input", { id: "access-who", list: "access-options", autocomplete: "off", placeholder: t("access.whoPlaceholder"), "aria-label": t("access.who") });
  let timer = 0;
  who.addEventListener("input", () => {
    clearTimeout(timer);
    const typed = who.value.trim().replace(/^@/, "");
    if (!typed || found.has(who.value.trim())) return;
    timer = setTimeout(async () => {
      const q = encodeURIComponent(typed);
      const [{ users }, { groups }] = await Promise.all([
        get("/api/users/search?q=" + q).catch(() => ({ users: [] })), get("/api/groups/search?q=" + q).catch(() => ({ groups: [] }))]);
      for (const u of users) { found.set(u.username, u.id); names.set(u.id, u.username); }
      for (const g of groups) found.set("@" + g, "@" + g);
      options.replaceChildren(...users.map((u) => h("option", { value: u.username })), ...groups.map((g) => h("option", { value: "@" + g, label: t("access.group", { name: g }) })));
    }, 150);
  });
  const added = h("p", { class: "muted", role: "status" });
  const add = () => {
    const p = found.get(who.value.trim());
    if (!p) { added.textContent = t("access.unknown"); return; }
    added.textContent = "";
    if (!entries.some((e) => e.p === p)) entries.push({ p, edit: true });
    who.value = "";
    draw();
  };
  who.addEventListener("keydown", (ev) => { if (ev.key === "Enter") { ev.preventDefault(); add(); } });
  return h("details", { class: "access-edit", id: "access-edit", open: read.length + edit.length > 0 },
    h("summary", { text: t("access.protect") }),
    modes,
    list,
    h("p", { class: "inline-form" }, who, options, h("button", { type: "button", id: "access-add", text: t("access.add"), onclick: add })),
    added,
    h("p", { class: "muted", text: spaceAdmin ? t("access.helpAdmin") : t("access.help") }),
    form({ id: "access-save", class: "actions-row" }, [h("button", { type: "submit", class: "primary", text: t("common.save") })], async () => {
      let all = entries.map((e) => e.p);
      let editors = mode === "edit" ? all : entries.filter((e) => e.edit).map((e) => e.p);
      // One who is not a space administrator keeps reading and editing;
      // an empty list of editors would let every reader edit, so it holds
      // at least oneself.
      if (all.length && (!spaceAdmin || !editors.length)) {
        if (!all.includes(me.user.id)) all = [...all, me.user.id];
        if (!editors.includes(me.user.id)) editors = [...editors, me.user.id];
      }
      await put(path + "/restriction", mode === "edit" ? { read: [], edit: editors } : { read: all, edit: editors });
      route();
    }));
}

// commentThreads shows an item's comments (ADR-0035) as threads: a first
// comment, on the whole item or on a passage of its body, and the replies
// to it (ADR-0047). Text is inserted as text (ADR-0014); one's own
// comments can be edited and deleted, and a thread resolved and reopened.
// It returns the section, a plugin highlighting the passages of the open
// threads in the body, and start(anchor), which begins a comment on a
// passage.
function commentThreads(path, page, live) {
  const author = (c) => person(c.author);
  const roots = page.comments.filter((c) => !c.reply);
  const replies = new Map();
  for (const c of page.comments.filter((c) => c.reply)) replies.set(c.reply, [...(replies.get(c.reply) || []), c]);
  const anchored = roots.filter((c) => c.anchor && !c.deleted && !c.resolved);
  const threadEls = new Map(); // root id -> its element

  const row = (c) => {
    const li = h("li", { class: "comment", id: "comment-" + c.id });
    const head = h("div", { class: "muted comment-head" }, face(c.author), `${author(c)} · ${when(c.created)}`, c.edited ? t("comment.edited") : "");
    if (c.deleted) {
      li.append(head, h("p", { class: "muted", text: t("comment.deleted") }));
      return li;
    }
    const mine = live && c.author === me.user.id;
    const status = h("div", { class: "status" });
    const edit = () => li.replaceChildren(head, form({ class: "comment-form" }, [
      mentionable(h("textarea", { name: "text", rows: 3, "aria-label": t("comment.editLabel"), value: c.text })),
      h("p", {}, h("button", { type: "submit", text: t("comment.save") }), h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => route() })),
    ], async (data) => { await put(`${path}/comments/${c.id}`, { text: data.get("text") }); route(); }));
    li.append(head, h("p", { class: "comment-text", text: c.text }), status, mine
      ? h("p", {},
        h("button", { class: "link", text: t("common.edit"), onclick: edit }), " · ",
        h("button", { class: "link danger", text: t("common.delete"), onclick: () => confirm(t("comment.deleteConfirm")) && action(() => del(`${path}/comments/${c.id}`), status) }))
      : ""); // append() would write a null out as the word "null"
    return li;
  };

  // reveal scrolls to a passage in the body and marks it for a moment.
  const reveal = (id) => {
    const marks = document.querySelectorAll(`.comment-anchor[data-comment="${CSS.escape(id)}"]`);
    if (!marks.length) return;
    marks[0].scrollIntoView({ block: "center" });
    marks.forEach((m) => m.classList.add("comment-focus"));
    setTimeout(() => marks.forEach((m) => m.classList.remove("comment-focus")), 1500);
  };

  const thread = (c) => {
    const status = h("div", { class: "status" });
    const quote = c.anchor && !c.deleted
      ? h("blockquote", { class: "comment-quote" },
        h("button", { type: "button", class: "link", text: c.anchor.quote, title: t("comment.showPassage"), onclick: () => reveal(c.id) }))
      : null;
    const gone = h("p", { class: "muted comment-gone", hidden: true, text: t("comment.passageGone") });
    const answers = h("ul", { class: "comments replies" }, (replies.get(c.id) || []).map(row));
    const replyForm = h("div");
    const reply = () => {
      replyForm.replaceChildren(form({ class: "comment-form" }, [
        mentionable(h("textarea", { name: "text", rows: 2, "aria-label": t("comment.replyLabel"), required: true })),
        h("p", {}, h("button", { type: "submit", text: t("comment.reply") }),
          h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => replyForm.replaceChildren() })),
      ], async (data) => { await post(path + "/comments", { text: data.get("text"), reply: c.id }); route(); }));
      replyForm.querySelector("textarea").focus();
    };
    const tools = live && !c.deleted
      ? h("p", { class: "thread-tools" },
        h("button", { class: "link", text: t("comment.reply"), onclick: reply }), " · ",
        c.resolved
          ? h("button", { class: "link", text: t("comment.reopen"), onclick: () => action(() => del(`${path}/comments/${c.id}/resolved`), status) })
          : h("button", { class: "link", text: t("comment.resolve"), onclick: () => action(() => put(`${path}/comments/${c.id}/resolved`), status) }))
      : null;
    const el = h("li", { class: "thread" + (c.anchor ? " inline" : "") },
      quote, gone, h("ul", { class: "comments" }, row(c)), answers,
      c.resolved ? h("p", { class: "muted", text: t("comment.resolvedBy", { name: person(c.resolvedBy), when: when(c.resolvedAt) }) }) : null,
      tools, replyForm, status);
    threadEls.set(c.id, el);
    return el;
  };

  const pending = h("div", { id: "inline-comment" });
  const start = (anchor) => {
    pending.replaceChildren(form({ class: "comment-form card" }, [
      h("blockquote", { class: "comment-quote", text: anchor.quote }),
      mentionable(h("textarea", { name: "text", rows: 3, placeholder: t("comment.placeholder"), "aria-label": t("comment.inlineLabel"), required: true })),
      h("p", {}, h("button", { type: "submit", text: t("comment.add") }),
        h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => pending.replaceChildren() })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text"), anchor }); route(); }));
    pending.scrollIntoView({ block: "nearest" });
    pending.querySelector("textarea").focus();
  };

  const open = roots.filter((c) => !c.resolved), done = roots.filter((c) => c.resolved);
  const add = live
    ? form({ id: "add-comment", class: "comment-form" }, [
      mentionable(h("textarea", { name: "text", rows: 3, placeholder: t("comment.placeholder"), "aria-label": t("comment.label"), required: true })),
      h("p", {}, h("button", { type: "submit", text: t("comment.add") })),
    ], async (data) => { await post(path + "/comments", { text: data.get("text") }); route(); })
    : null;
  const section = h("section", { id: "comments" },
    h("h2", { text: t("comment.title", { n: page.total.value }) }),
    pending,
    open.length ? filtered(h("ul", { class: "threads" }, open.map(thread)), { id: "threads", label: t("comment.title", { n: open.length }) }) : empty(t("comment.none")),
    done.length ? h("details", { class: "resolved-threads" }, h("summary", { text: t("comment.resolvedThreads", { n: done.length }) }),
      h("ul", { class: "threads" }, done.map(thread))) : null,
    page.next ? h("p", { class: "muted", text: t("comment.truncated", { n: page.comments.length }) }) : null,
    add,
  );

  const plugin = commentPlugin({
    threads: () => anchored,
    // A thread whose passage is gone says so.
    found: (ids) => anchored.forEach((c) => { const g = threadEls.get(c.id)?.querySelector(".comment-gone"); if (g) g.hidden = ids.has(c.id); }),
    open: (id) => {
      const el = threadEls.get(id);
      if (!el) return;
      el.scrollIntoView({ block: "nearest" });
      el.classList.add("comment-focus");
      setTimeout(() => el.classList.remove("comment-focus"), 1500);
    },
  });
  return { section, plugin, start };
}

// historySection lists an item's history. A change of the body can be
// compared with the body before it and, on a live item, restored
// (ADR-0043).
function historySection(path, events, live, versions = true) {
  const shown = h("div", { id: "version" });
  const bodies = events.filter((e) => e.kind === "BodyCommitted" && !e.erased);
  const version = async (seq) => (await get(`${path}/versions/${seq}`)).body;
  const compare = async (e) => {
    const i = bodies.indexOf(e);
    const [before, after] = await Promise.all([i > 0 ? version(bodies[i - 1].seq) : [], version(e.seq)]);
    mount(shown, h("section", { class: "card" },
      h("h3", { text: t("version.title", { when: when(e.at), who: person(e.actor) }) }),
      renderDiff(before, after),
      h("p", {},
        // No confirmation: restoring is undone by restoring the version before.
        live ? h("button", { text: t("version.restore"), onclick: () => action(() => post(`${path}/versions/${e.seq}/restore`), shown) }) : null,
        h("button", { class: "link", text: t("common.close"), onclick: () => shown.replaceChildren() })),
    ));
    shown.scrollIntoView({ block: "nearest" });
  };
  const details = (e) => {
    if (e.erased) return t("history.erased");
    if (e.kind === "BodyCommitted") {
      // Earlier versions of a text with protected passages are shown to
      // those who read them all (ADR-0088).
      if (!versions) return "";
      return h("button", { class: "link", text: t("version.changes"), onclick: () => compare(e).catch((err) => mount(shown, errorBox(err))) });
    }
    return [e.title, e.field && `${e.field} = ${JSON.stringify(e.value ?? null)}`, e.to, e.comment && t("history.comment", { id: e.comment.slice(0, 8) })].filter(Boolean).join(" ");
  };
  return h("section", {},
    h("h2", { text: t("history.title") }),
    shown,
    filtered(h("table", { class: "history" }, events.slice().reverse().map((e) => h("tr", { "data-f-kind": e.kind, "data-f-who": person(e.actor), "data-filter-extra": names.get(e.actor) || "" },
      h("td", { text: when(e.at) }),
      h("td", {}, h("span", { class: "who" }, face(e.actor, 20), person(e.actor))),
      h("td", { text: e.kind }),
      h("td", {}, details(e)),
    ))), { id: "history", label: t("history.title"), choices: [{ key: "kind", label: t("filter.kind") }, { key: "who", label: t("filter.who") }] }),
  );
}

// --- account -----------------------------------------------------------------

async function accountView() {
  const show = screen();
  const [{ tokens }, contacts] = await Promise.all([get("/api/tokens"), get("/api/me/contacts")]);
  const secret = h("div");
  show(
    h("h1", { text: me.user.username }),
    h("p", { class: "muted", text: t("account.id", { id: me.user.id, via: me.via }) }),
    avatarCard(),
    contactsCard(contacts),
    // One's connections to process engines (ADR-0151).
    connectionsCard(),
    form({ class: "card", id: "language" }, [
      field(t("account.language"), h("select", { name: "language", onchange: (ev) => ev.target.form.requestSubmit() },
        languages.filter((l) => !me.languages || me.languages.includes(l.code))
          .map((l) => h("option", { value: l.code, text: l.name, selected: l.code === language() })))),
    ], async (data) => {
      me.language = data.get("language");
      await put("/api/me/language", { language: me.language });
      setLanguage(me.language);
      route();
    }),
    form({ class: "card", id: "password" }, [
      h("h2", { text: t("account.password") }),
      field(t("account.current"), h("input", { name: "current", type: "password", autocomplete: "current-password", required: true })),
      field(t("account.new"), h("input", { name: "new", type: "password", autocomplete: "new-password", minlength: 12, required: true })),
      h("button", { type: "submit", text: t("account.change") }),
    ], async (data) => {
      await put("/api/me/password", { current: data.get("current"), new: data.get("new") });
      me = null;
      route();
    }),
    h("section", { class: "card" },
      h("h2", { text: t("account.tokens") }),
      filtered(h("table", {}, tokens.map((tok) => h("tr", { "data-f-scope": tok.scope },
        h("td", { text: tok.name }), h("td", { text: tok.scope }), h("td", { text: t("account.expires", { when: when(tok.expires) }) }),
        h("td", {}, h("button", { type: "button", class: "link danger", text: t("account.revoke"),
          onclick: () => confirm(t("account.revokeConfirm")) && action(() => del("/api/tokens/" + tok.id)) })),
      ))), { id: "tokens", label: t("account.tokens"), choices: [{ key: "scope", label: t("account.scope") }] }),
      form({ id: "new-token" }, [
        field(t("field.name"), h("input", { name: "name", required: true })),
        field(t("account.scope"), h("select", { name: "scope" }, h("option", { value: "read", text: "read" }), h("option", { value: "full", text: "full" }),
          me.admin ? h("option", { value: "metrics", text: t("account.scopeMetrics") }) : null)),
        field(t("account.days"), h("input", { name: "days", type: "number", min: 1, max: 366, value: 30 })),
        h("button", { type: "submit", text: t("account.createToken") }),
      ], async (data) => {
        const res = await post("/api/tokens", { name: data.get("name"), scope: data.get("scope"), days: Number(data.get("days")) });
        secret.replaceChildren(h("p", { class: "notice success" }, t("account.copy"), h("code", { text: res.secret })));
      }),
      secret,
      h("p", { class: "muted" }, t("account.mcpHint"), " ", link("#/mcp", t("nav.mcp"))),
    ),
  );
}

// avatarCard sets or removes one's profile picture (ADR-0050). The browser
// cuts the picture to a square and scales it down to 256 pixels before
// sending it, so a photo from a phone fits the server's budget.
function avatarCard() {
  const status = h("div", { class: "status" });
  const picker = h("input", { type: "file", id: "avatar-file", accept: "image/png,image/jpeg,image/gif,image/webp", "aria-label": t("account.avatarChoose") });
  picker.addEventListener("change", () => action(async () => {
    const file = picker.files[0];
    if (!file) return;
    const blob = await squareImage(file, 256);
    const res = await fetch("/api/me/avatar", { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: blob });
    if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
    me.avatar = (await res.json()).avatar;
  }, status));
  return h("section", { class: "card", id: "avatar" },
    h("h2", { text: t("account.avatar") }),
    h("div", { class: "avatar-row" }, avatar(me.user.id, me.user.username, me.avatar, 64),
      h("div", {},
        h("p", { class: "muted", text: me.avatar ? t("account.avatarSet") : t("account.avatarNone") }),
        h("label", { class: "upload" }, h("span", { text: t("account.avatarChoose") }), picker),
        me.avatar ? h("button", { type: "button", class: "link", text: t("account.avatarRemove"),
          onclick: () => action(async () => { await del("/api/me/avatar"); me.avatar = ""; }, status) }) : null)),
    status);
}

// contactsCard sets the channels messages reach one on (ADR-0061): a mail
// address and the webhook addresses of Slack, Microsoft Teams or another
// service. A field left empty is a channel one does not have.
function contactsCard(contacts) {
  const channel = (name, label, type, hint) => field(label, h("input", { name, type, value: contacts[name] || "", placeholder: hint, autocomplete: name === "email" ? "email" : "off" }));
  const state = h("p", { class: "muted", id: "channels" });
  const summary = (c) => {
    const on = [["email", t("contacts.email")], ["slack", "Slack"], ["teams", "Microsoft Teams"], ["webhook", t("contacts.webhook")]].filter(([k]) => c[k]).map(([, l]) => l);
    state.textContent = on.length ? t("contacts.available", { list: on.join(", ") }) : t("contacts.none");
  };
  summary(contacts);
  const outcome = h("div", { id: "delivery" });
  const resultName = { email: t("contacts.email"), slack: "Slack", teams: "Microsoft Teams", webhook: t("contacts.webhook") };
  const showResults = (results, at) => outcome.replaceChildren(results?.length ? h("ul", { class: "delivery-results" },
    at ? h("li", { class: "muted", text: t("contacts.lastAt", { when: when(at) }) }) : "",
    ...results.map((r) => h("li", { class: r.ok ? "ok" : "failed" },
      h("strong", { text: (resultName[r.channel] || r.channel) + ": " }), r.ok ? t("contacts.delivered") : r.problem))) : "");
  showResults(contacts.last?.results, contacts.last?.at);
  const test = h("button", { type: "button", id: "contacts-test", class: "secondary", text: t("contacts.test"), onclick: () => action(async () => {
    test.disabled = true;
    try {
      const { results } = await post("/api/me/contacts/test");
      showResults(results);
    } finally { test.disabled = false; }
  }, outcome) });
  return form({ class: "card", id: "contacts" }, [
    h("h2", { text: t("contacts.title") }),
    h("p", { class: "muted", text: t("contacts.help") }),
    channel("email", t("contacts.email"), "email", "name@example.org"),
    contacts.mail ? "" : h("p", { class: "muted", id: "no-mail", text: t("contacts.noMailServer") }),
    channel("slack", "Slack", "url", "https://hooks.slack.com/services/…"),
    channel("teams", "Microsoft Teams", "url", "https://…"),
    channel("webhook", t("contacts.webhook"), "url", "https://…"),
    state,
    h("p", { class: "actions-row" }, h("button", { type: "submit", text: t("common.save") }), test),
    outcome,
  ], async (data, st) => {
    const saved = await put("/api/me/contacts", { email: data.get("email"), slack: data.get("slack"), teams: data.get("teams"), webhook: data.get("webhook") });
    summary(saved);
    st.replaceChildren(h("p", { class: "notice success", text: t("contacts.saved") }));
  });
}

// squareImage cuts the middle square out of an image file and scales it to
// size pixels, as a PNG.
async function squareImage(file, size) {
  const bitmap = await createImageBitmap(file).catch(() => { throw new Error(t("account.avatarNotImage")); });
  const side = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = Math.min(size, side);
  canvas.getContext("2d").drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error(t("account.avatarNotImage")))), "image/png"));
}

// --- AI access (MCP, OAuth) ------------------------------------------------------

// KNOWN are the assistants whose redirect address can be filled in without
// asking. Short on purpose: a wrong address here would hand out
// credentials that cannot work; every other assistant shows its own in its
// connector dialog (ADR-0068).
const KNOWN = [
  { id: "claude", name: "Claude", redirect: "https://claude.ai/api/mcp/auth_callback", hint: "aiaccess.claudeHint" },
];

// copyable is a value to move into another window, with a copy button.
function copyable(text, id) {
  const pre = h("pre", { class: "snippet", text, id: id || null });
  const button = h("button", { type: "button", class: "secondary", text: t("mcp.copy"), onclick: async () => {
    try {
      await navigator.clipboard.writeText(pre.textContent);
      button.textContent = t("mcp.copied");
    } catch {
      button.textContent = t("mcp.copyFailed");
    }
  } });
  return h("div", { class: "copyable" }, pre, button);
}

// discover reads what this server publishes about itself; both documents
// are public.
async function discover() {
  const [as, rs] = await Promise.all([
    get("/.well-known/oauth-authorization-server").catch(() => null),
    get("/.well-known/oauth-protected-resource/mcp").catch(() => null),
  ]);
  return { issuer: as?.issuer || "", registration: as?.registration_endpoint || "", mcp: rs?.resource || location.origin + "/mcp" };
}

// mcpView is the AI access page (ADR-0012, ADR-0068). A hosted assistant
// such as Claude connects with OAuth: an administrator registers it here
// and gets the three values its connector dialog asks for; each person
// approves it once and can withdraw the approval below. A local client
// connects with an API token instead. Either way the assistant acts with
// the person's rights, and its changes carry the person as author.
async function mcpView() {
  const show = screen();
  const [disc, listed, grants, clients] = await Promise.all([
    discover(),
    api("POST", "/mcp", { jsonrpc: "2.0", id: 1, method: "tools/list" }).catch(() => null),
    get("/api/oauth/grants").catch(() => ({ grants: [] })),
    me.admin ? get("/api/oauth/clients").catch(() => null) : null,
  ]);
  const tools = listed?.result?.tools || [];
  const endpoint = disc.mcp;

  // The address the server publishes must be one a hosted assistant can
  // reach; behind a proxy that is not trusted it is the http one.
  const originNote = disc.issuer && disc.issuer !== location.origin
    ? h("p", { class: "notice warning", id: "aiaccess-origin", text: t("aiaccess.originMismatch", { published: disc.issuer, here: location.origin }) })
    : null;

  // Connecting a hosted assistant: administrators register it here.
  const result = h("div", { id: "aiaccess-result" });
  const appList = h("tbody", {});
  const clientRow = (c) => h("tr", {},
    h("td", {}, h("strong", { text: c.name }), " ", h("span", { class: c.self ? "badge danger" : "badge", text: c.self ? t("aiaccess.selfRegistered") : t("aiaccess.addedHere") }),
      h("div", { class: "muted", text: c.id })),
    h("td", { class: "muted", text: c.redirects.join(", ") }),
    h("td", { class: "muted", text: when(c.created) }),
    h("td", {}, h("button", { type: "button", class: "link danger", text: t("aiaccess.remove"), onclick: async () => {
      if (!confirm(t("aiaccess.removeConfirm", { name: c.name }))) return;
      await del("/api/oauth/clients/" + encodeURIComponent(c.id));
      route();
    } })));
  const fillApps = (list) => appList.replaceChildren(...(list.length ? list.map(clientRow) : [h("tr", {}, h("td", { colspan: 4, class: "muted", text: t("aiaccess.noApps") }))]));
  if (clients) fillApps(clients.clients);
  // The apps change as they are added, so the filter applies again then.
  const appsBox = clients ? filtered(h("table", {}, h("thead", {}, h("tr", {}, h("th", { text: t("aiaccess.app") }), h("th", { text: t("aiaccess.redirect") }), h("th", { text: t("aiaccess.since") }), h("th", {}))), appList),
    { id: "aiaccess-apps", label: t("aiaccess.appsTitle") }) : null;
  const redirectField = h("input", { name: "redirect", type: "url", required: true, value: KNOWN[0].redirect, readOnly: true });
  const nameField = h("input", { name: "name", required: true, value: KNOWN[0].name });
  const hint = h("p", { class: "muted", text: t(KNOWN[0].hint) });
  const choose = h("select", { name: "app", onchange: () => {
    const k = KNOWN.find((x) => x.id === choose.value);
    redirectField.readOnly = !!k;
    redirectField.value = k ? k.redirect : "";
    nameField.value = k ? k.name : "";
    hint.textContent = k ? t(k.hint) : t("aiaccess.otherHint");
  } }, ...KNOWN.map((k) => h("option", { value: k.id, text: k.name })), h("option", { value: "other", text: t("aiaccess.other") }));

  const connect = h("section", { class: "card", id: "aiaccess-connect" },
    h("h2", { text: t("aiaccess.connectTitle") }),
    h("p", { text: t("aiaccess.connectIntro") }),
    originNote,
    disc.registration ? h("p", { class: "notice", text: t("aiaccess.registrationOpen") }) : null,
    me.admin
      ? form({ id: "aiaccess-register" }, [
        field(t("aiaccess.app"), choose),
        field(t("field.name"), nameField),
        field(t("aiaccess.redirect"), redirectField),
        hint,
        h("button", { type: "submit", class: "primary", text: t("aiaccess.create") }),
      ], async (data) => {
        const res = await post("/api/oauth/clients", { name: data.get("name"), redirects: [data.get("redirect")] });
        result.replaceChildren(
          h("p", { class: "notice success", text: t("aiaccess.created") }),
          h("h3", { text: t("aiaccess.mcpUrl") }), copyable(endpoint, "aiaccess-url"),
          h("h3", { text: t("aiaccess.clientId") }), copyable(res.client.id, "aiaccess-client-id"),
          h("h3", { text: t("aiaccess.clientSecret") }), copyable(res.secret, "aiaccess-client-secret"),
          h("p", { class: "muted", text: t("aiaccess.afterwards") }));
        clients.clients.push(res.client);
        fillApps(clients.clients);
        appsBox?.applyFilter();
      })
      : h("p", { class: "muted" }, t("aiaccess.askAdmin"), " ", h("code", { text: endpoint })),
    result);

  const apps = clients ? h("section", { class: "card", id: "aiaccess-apps" },
    h("h2", { text: t("aiaccess.appsTitle") }),
    h("p", { class: "muted", text: t("aiaccess.appsNote") }),
    appsBox) : null;

  // Everybody's own approvals, to see and to withdraw.
  const approvals = h("section", { class: "card", id: "aiaccess-grants" },
    h("h2", { text: t("aiaccess.grantsTitle") }),
    h("p", { class: "muted", text: t("aiaccess.grantsNote") }),
    grants.grants.length
      ? filtered(h("table", {}, h("thead", {}, h("tr", {}, h("th", { text: t("aiaccess.app") }), h("th", { text: t("aiaccess.access") }), h("th", { text: t("aiaccess.approved") }), h("th", { text: t("aiaccess.lastUsed") }), h("th", {}))),
        h("tbody", {}, grants.grants.map((g) => h("tr", {},
          h("td", {}, h("strong", { text: g.name || g.client }), g.self ? [" ", h("span", { class: "badge danger", text: t("aiaccess.selfRegistered") })] : null),
          h("td", { text: g.scope === "read" ? t("mcp.scopeRead") : t("mcp.scopeFull") }),
          h("td", { class: "muted", text: when(g.created) }),
          h("td", { class: "muted", text: when(g.last_used) }),
          h("td", {}, h("button", { type: "button", class: "link danger", text: t("aiaccess.withdraw"), onclick: async () => {
            await del("/api/oauth/grants/" + encodeURIComponent(g.client));
            route();
          } })))))), { id: "aiaccess-grants", label: t("aiaccess.grantsTitle") })
      : h("p", { class: "muted", text: t("aiaccess.noGrants") }));

  // Local clients: an API token.
  const token = "<TOKEN>";
  const snippets = (tok) => [
    [t("mcp.claudeCode"), `claude mcp add --transport http wikiwikibangbang ${endpoint} --header "Authorization: Bearer ${tok}"`],
    [t("mcp.json"), JSON.stringify({ mcpServers: { wikiwikibangbang: { type: "http", url: endpoint, headers: { Authorization: `Bearer ${tok}` } } } }, null, 2)],
    [t("mcp.stdio"), `WWBB_TOKEN=${tok} wikiwikibangbang mcp -url ${location.origin}`],
  ];
  const setup = h("div", { id: "mcp-setup" });
  const fill = (tok) => setup.replaceChildren(...snippets(tok).map(([label, text]) => h("section", {}, h("h3", { text: label }), copyable(text))));
  fill(token);
  const local = h("details", { class: "card", id: "aiaccess-token" },
    h("summary", { text: t("aiaccess.tokenTitle") }),
    h("p", { class: "muted", text: t("mcp.tokenNote") }),
    form({ id: "mcp-token" }, [
      field(t("field.name"), h("input", { name: "name", required: true, value: t("mcp.tokenName") })),
      field(t("account.scope"), h("select", { name: "scope" },
        h("option", { value: "full", text: t("mcp.scopeFull") }), h("option", { value: "read", text: t("mcp.scopeRead") }))),
      field(t("account.days"), h("input", { name: "days", type: "number", min: 1, max: 366, value: 30 })),
      h("button", { type: "submit", class: "primary", text: t("account.createToken") }),
    ], async (data, status) => {
      const res = await post("/api/tokens", { name: data.get("name"), scope: data.get("scope"), days: Number(data.get("days")) });
      fill(res.secret);
      status.replaceChildren(h("p", { class: "notice success", text: t("mcp.tokenMade") }));
    }),
    setup,
    h("p", { class: "muted" }, t("mcp.tokensAt"), " ", link("#/account", t("nav.account"))));

  show(
    h("h1", { text: t("mcp.title") }),
    h("p", { text: t("mcp.intro") }),
    connect,
    apps,
    approvals,
    local,
    h("section", { class: "card" },
      h("h2", { text: t("mcp.tools", { n: tools.length }) }),
      h("p", { class: "muted", text: t("mcp.toolsNote") }),
      filtered(h("ul", { class: "list tools" }, tools.map((tool) => h("li", {}, h("code", { text: tool.name }), " — ", (tool.description || "").split(". ")[0]))),
        { id: "mcp-tools", label: t("mcp.tools", { n: tools.length }) })),
  );
}

// oauthView is the consent page an assistant sends a person to (ADR-0068):
// it names the application, says when nobody vetted it, and returns the
// person to the application with a code, or with a refusal.
async function oauthView(params) {
  const show = screen();
  if (params.get("error")) {
    return show(h("section", { class: "card narrow", id: "oauth-consent" },
      h("h1", { text: t("oauth.title") }), h("p", { class: "error", role: "alert", text: t("oauth.badClient") })));
  }
  const keys = ["client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method", "resource"];
  const req = Object.fromEntries(keys.map((k) => [k, params.get(k) || ""]));
  let info;
  try {
    info = await get("/api/oauth/request?" + new URLSearchParams(req));
  } catch (err) {
    return show(h("section", { class: "card narrow", id: "oauth-consent" }, h("h1", { text: t("oauth.title") }), errorBox(err)));
  }
  const status = h("div", { class: "status" });
  const answer = async (deny) => {
    status.replaceChildren();
    try {
      const access = document.querySelector("#oauth-consent input[name=access]:checked")?.value || "read";
      const res = await post("/api/oauth/approve", { ...req, deny, access });
      location.href = res.redirect;
    } catch (err) {
      status.replaceChildren(errorBox(err));
    }
  };
  show(h("section", { class: "card narrow", id: "oauth-consent" },
    h("h1", { text: t("oauth.title") }),
    h("p", { class: "login-lead", text: t("oauth.asks", { name: info.client.name, user: me.user.username }) }),
    info.client.self ? h("p", { class: "notice warning", id: "oauth-self", text: t("oauth.selfRegistered") }) : null,
    // The person chooses what the application may do; reading only is
    // preselected, as the smaller right.
    h("fieldset", { class: "field", id: "oauth-access" },
      h("legend", { text: t("oauth.accessLegend") }),
      h("label", { class: "check" }, h("input", { type: "radio", name: "access", value: "read", checked: true }), t("oauth.accessRead")),
      h("label", { class: "check" }, h("input", { type: "radio", name: "access", value: "full" }), t("oauth.accessFull"))),
    h("ul", {},
      h("li", { text: t("oauth.only") }),
      h("li", { text: t("oauth.withdraw") })),
    h("p", { class: "muted", text: t("oauth.returnsTo", { host: info.redirect_host }) }),
    h("div", { class: "actions-row" },
      h("button", { type: "button", class: "primary", id: "oauth-allow", text: t("oauth.allow"), onclick: () => answer(false) }),
      h("button", { type: "button", class: "secondary", id: "oauth-deny", text: t("oauth.deny"), onclick: () => answer(true) })),
    status));
}

// --- administration ----------------------------------------------------------

// filteredGroups filters the groups: each row names its group in a
// heading cell (ADR-0098).
function filteredGroups(table) {
  return filtered(table, { id: "admin-groups", label: t("admin.groups"), rows: () => [...table.rows] });
}

async function adminView() {
  const show = screen();
  const [{ users }, { spaces }, { types }, auditPage, { groups }, { holders }, { requests }, access, sso, delivery, { spaces: trashed }] = await Promise.all([
    get("/api/users"), get("/api/spaces"), get("/api/types"), get("/api/audit?limit=50"), get("/api/groups"), get("/api/system-roles"),
    get("/api/account-requests"), get("/api/settings/access"), get("/api/oidc").catch(() => ({ enabled: false })),
    get("/api/settings/delivery").catch(() => ({ status: {} })), get("/api/spaces/trash").catch(() => ({ spaces: [] })),
  ]);
  for (const u of users) names.set(u.id, u.username);
  const status = h("div", { class: "status" });
  const userOptions = () => users.map((u) => h("option", { value: u.id, text: u.username }));
  const rolesOf = (id) => holders.find((x) => x.principal === id)?.roles || [];

  // The table of contents scrolls to a section; the hash stays the route.
  const sections = [];
  const section = (id, title, ...content) => {
    const el = h("section", { class: "card", id: "admin-" + id }, h("h2", { text: title }), ...content);
    sections.push([el, title]);
    return el;
  };

  // Deleting a space takes two steps (ADR-0069): into the trash, where it
  // can be restored, then for good after its key is typed again.
  const spaceStatus = h("div", { class: "status", id: "space-status" });
  const spaceAction = (fn) => async () => {
    spaceStatus.replaceChildren();
    try {
      if (await fn() !== false) route();
    } catch (err) {
      spaceStatus.replaceChildren(errorBox(err));
    }
  };
  const spacesSection = section("spaces", t("admin.spaces"),
    spaces.length ? filtered(h("table", {}, spaces.map((sp) => h("tr", {},
      h("td", {}, link("#/space/" + encodeURIComponent(sp.key), sp.name)), h("td", { class: "muted", text: sp.key }),
      h("td", {}, link("#/members/" + encodeURIComponent(sp.key), t("admin.members"))),
      h("td", {}, h("button", { type: "button", class: "link danger", "data-trash-space": sp.key, text: t("admin.trashSpace"), onclick: spaceAction(async () => {
        if (!confirm(t("admin.trashSpaceConfirm", { name: sp.name }))) return false;
        await del("/api/spaces/" + encodeURIComponent(sp.key));
      }) }))))), { id: "admin-spaces", label: t("admin.spaces") }) : empty(t("admin.none")),
    spaceStatus,
    trashed.length ? h("div", { id: "space-trash" },
      h("h3", { text: t("admin.spaceTrash") }),
      h("p", { class: "muted", text: t("admin.spaceTrashNote") }),
      filtered(h("table", {}, trashed.map((sp) => h("tr", {},
        h("td", { text: sp.name }), h("td", { class: "muted", text: sp.key }),
        h("td", { class: "muted", text: t("admin.spaceItems", { n: sp.items }) }),
        h("td", {}, h("button", { type: "button", class: "link", text: t("admin.restoreSpace"), onclick: spaceAction(() => post("/api/spaces/" + encodeURIComponent(sp.key) + "/restore")) }),
          " ", h("button", { type: "button", class: "link danger", text: t("admin.purgeSpace"), onclick: spaceAction(async () => {
            const typed = prompt(t("admin.purgeSpaceConfirm", { name: sp.name, key: sp.key }));
            if (typed === null) return false;
            await post("/api/spaces/" + encodeURIComponent(sp.key) + "/purge", { confirm: typed.trim() });
          }) }))))), { id: "admin-trash", label: t("admin.spaceTrash") })) : null,
    form({ id: "new-space" }, [
      h("h3", { text: t("admin.newSpace") }),
      field(t("admin.key"), h("input", { name: "key", required: true, pattern: "[A-Z][A-Z0-9]*", title: t("admin.keyHint") })),
      field(t("field.name"), h("input", { name: "name", required: true })),
      h("button", { type: "submit", text: t("admin.createSpace") }),
    ], async (d) => { await post("/api/spaces", { key: d.get("key"), name: d.get("name") }); route(); }));

  const typeText = h("textarea", { name: "type", rows: 8, required: true },
    '{"name":"page","body":true,"fields":[{"name":"owner","kind":"user"}]}');
  const describe = (ty) => [
    ty.body ? t("admin.typeBody") : null,
    ty.fields.length ? t("admin.typeFields", { list: ty.fields.map((f) => `${f.name} (${f.kind}${f.required ? ", *" : ""})`).join(", ") }) : null,
    ty.workflow ? t("admin.typeStates", { list: ty.workflow.states.join(" → ") }) : null,
  ].filter(Boolean).join(" · ");
  const typesSection = section("types", t("admin.types"),
    setupCard(types),
    types.length ? filtered(h("table", { id: "types" }, types.map((ty) => h("tr", { "data-f-kind": ty.workflow ? t("admin.typeWithWorkflow") : t("admin.typeWithoutWorkflow") },
      h("td", {}, h("strong", { text: ty.name }), ` v${ty.version}`), h("td", { class: "muted", text: describe(ty) }),
      h("td", {}, h("button", { type: "button", class: "link", text: t("common.edit"), onclick: () => {
        const { version, ...def } = ty;
        typeText.value = JSON.stringify(def, null, 2);
        typeText.scrollIntoView({ block: "center" });
        typeText.focus();
      } }), " ",
      // A type no item and no kanban uses can be removed (ADR-0094).
      h("button", { type: "button", class: "link danger", text: t("admin.removeType"), onclick: () =>
        confirm(t("admin.removeTypeConfirm", { name: ty.name })) &&
        action(async () => { await del(`/api/types/${encodeURIComponent(ty.name)}`); route(); }, status) }))))),
      { id: "admin-types", label: t("admin.types"), choices: [{ key: "kind", label: t("filter.kind") }] }) : null,
    form({ id: "new-type" }, [
      h("h3", { text: t("admin.defineType") }),
      h("p", { class: "muted", text: t("admin.typeHelp") }),
      field(t("admin.typeJson"), typeText),
      h("button", { type: "submit", text: t("admin.define") }),
    ], async (d) => {
      let def;
      try {
        def = JSON.parse(d.get("type"));
      } catch (err) {
        throw new Error(t("admin.badJson", { error: err.message }));
      }
      await post("/api/types", def);
      route();
    }));

  const sysToggle = (u, role, label) => {
    const box = h("input", { type: "checkbox", checked: rolesOf(u.id).includes(role), "aria-label": `${label}: ${u.username}` });
    box.addEventListener("change", () => action(() => (box.checked ? put : del)(`/api/system-roles/${role}/${encodeURIComponent(u.id)}`), status));
    return h("label", { class: "check" }, box, " ", label);
  };
  // Account requests made at the sign-in page (ADR-0061): whether they
  // are accepted, and the ones waiting for a decision.
  const openBox = h("input", { type: "checkbox", id: "requests-open", checked: access.accountRequests });
  openBox.addEventListener("change", () => action(() => put("/api/settings/access", { accountRequests: openBox.checked }), status));
  // What the sign-in page offers, and what is missing for the rest.
  const mailReset = delivery.status?.mail && delivery.status?.publicURL;
  // Single sign-on and mail are set in the server's environment; the help
  // says where, and every row still missing one opens it.
  const loginHelp = envHelp("login-env-help");
  const setupRow = (id, label, on, text, env) => h("li", { class: on ? "on" : "off", id },
    h("span", { class: "setup-dot", "aria-hidden": "true" }), h("strong", { text: label }),
    h("span", { class: "muted" }, text, !on && env ? [" ", infoButton(loginHelp)] : ""));
  const loginSetup = h("div", { class: "login-setup" },
    h("h3", { text: t("loginsetup.title") }),
    h("p", { class: "muted" }, t("loginsetup.help"), " ", infoButton(loginHelp)),
    h("ul", { class: "setup-list" },
      setupRow("setup-sso", t("loginsetup.sso"), sso.enabled, sso.enabled
        ? t("loginsetup.ssoOn", { name: (sso.providers || [{ name: sso.name }]).map((p) => p.name).join(", ") }) : t("loginsetup.ssoOff"), true),
      setupRow("setup-reset", t("loginsetup.reset"), mailReset, mailReset ? t("loginsetup.resetOn", { host: delivery.status.mailHost }) : t("loginsetup.resetOff"), true),
      setupRow("setup-requests", t("loginsetup.requests"), access.accountRequests, access.accountRequests ? t("loginsetup.requestsOn") : t("loginsetup.requestsOff"))),
    loginHelp);
  const requestsSection = section("requests", t("requests.title"),
    loginSetup,
    h("p", { class: "muted", text: t("requests.help") }),
    h("label", { class: "check" }, openBox, " ", t("requests.open")),
    requests.length ? filtered(h("table", { id: "account-requests" },
      h("tr", {}, h("th", { text: t("field.username") }), h("th", { text: t("contacts.email") }), h("th", { text: t("signup.reason") }), h("th", { text: t("requests.when") }), h("th")),
      requests.map((rq) => h("tr", { "data-username": rq.username },
        h("td", { text: rq.username }), h("td", { text: rq.email }), h("td", { class: "muted", text: rq.reason }), h("td", { class: "muted", text: when(rq.created) }),
        h("td", {},
          h("button", { type: "button", class: "approve", text: t("requests.approve"), onclick: () => action(() => post(`/api/account-requests/${rq.id}/approve`), status) }), " ",
          h("button", { type: "button", class: "link", text: t("requests.reject"),
            onclick: () => confirm(t("requests.rejectConfirm", { name: rq.username })) && action(() => del(`/api/account-requests/${rq.id}`), status) }))))),
      { id: "admin-requests", label: t("requests.title") }) :
      empty(t("requests.none"), "no-requests"));

  // A reset link to hand over, for someone without a mail address or when
  // mail is not set up (ADR-0064).
  const resetCell = (u) => h("td", {}, h("button", { type: "button", class: "link", text: t("reset.link"), "aria-label": t("reset.linkFor", { name: u.username }),
    onclick: () => action(async () => {
      const { token, expires } = await post(`/api/users/${encodeURIComponent(u.id)}/password-reset`);
      const url = `${location.origin}/#/reset/${token}`;
      status.replaceChildren(h("div", { class: "notice", id: "reset-handed" },
        h("p", { text: t("reset.linkMade", { name: u.username, when: when(expires) }) }),
        h("code", { class: "copyable", text: url })));
    }, status) }));
  const emailCell = (u) => h("td", {},
    h("span", { class: "muted", text: u.email || t("contacts.noEmail") }), " ",
    h("button", { type: "button", class: "link", text: t("common.edit"), "aria-label": t("contacts.editEmail", { name: u.username }), onclick: () => {
      const email = prompt(t("contacts.editEmail", { name: u.username }), u.email || "");
      if (email !== null) action(() => put(`/api/users/${encodeURIComponent(u.id)}/email`, { email: email.trim() }), status);
    } }));
  const usersSection = section("users", t("admin.users"),
    h("p", { class: "muted", text: t("admin.usersHelp") }),
    filtered(h("table", { id: "users" }, users.map((u) => h("tr", { "data-filter-text": `${u.username} ${u.email || ""}`,
      "data-f-role": [rolesOf(u.id).includes("admin") ? t("admin.roleAdmin") : "", rolesOf(u.id).includes("erasure") ? t("admin.roleErasure") : ""].filter(Boolean).join("|") },
      h("td", { text: u.username }),
      emailCell(u),
      h("td", {}, sysToggle(u, "admin", t("admin.roleAdmin"))),
      h("td", {}, sysToggle(u, "erasure", t("admin.roleErasure"))),
      resetCell(u)))), { id: "admin-users", label: t("admin.users"), choices: [{ key: "role", label: t("admin.role") }] }),
    form({ id: "new-user" }, [
      h("h3", { text: t("admin.newUser") }),
      field(t("field.username"), h("input", { name: "username", required: true })),
      field(t("field.password"), h("input", { name: "password", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.createUser") }),
    ], async (d) => { await post("/api/users", { username: d.get("username"), password: d.get("password") }); route(); }));

  const groupsSection = section("groups", t("admin.groups"),
    h("p", { class: "muted", text: t("admin.groupsHelp") }),
    groups.length ? filteredGroups(h("table", { id: "groups" }, groups.map((g) => h("tr", {},
      h("th", { text: "@" + g.name }),
      h("td", {}, g.members.map((m) => h("span", { class: "member" }, person(m),
        h("button", { type: "button", class: "link", text: "×", title: t("admin.removeMember", { name: person(m), group: g.name }),
          "aria-label": t("admin.removeMember", { name: person(m), group: g.name }),
          onclick: () => action(() => del(`/api/groups/${encodeURIComponent(g.name)}/members/${encodeURIComponent(m)}`), status) })))))))) : h("p", { class: "muted", text: t("admin.noGroups") }),
    form({ id: "add-member" }, [
      field(t("admin.group"), h("input", { name: "group", required: true, pattern: "[A-Za-z0-9][A-Za-z0-9._\\-]*", list: "group-names" })),
      h("datalist", { id: "group-names" }, groups.map((g) => h("option", { value: g.name }))),
      field(t("admin.user"), h("select", { name: "user" }, userOptions())),
      h("button", { type: "submit", text: t("admin.addMember") }),
    ], async (d) => { await put(`/api/groups/${encodeURIComponent(d.get("group"))}/members/${encodeURIComponent(d.get("user"))}`); route(); }));

  const grantSection = section("roles", t("admin.grant"),
    h("p", { class: "muted", text: t("admin.grantHelp") }),
    form({ id: "grant" }, [
      field(t("col.space"), h("select", { name: "space" }, favouritesFirst(spaces).map((s) => h("option", { value: s.key, text: `${s.name} (${s.key})` })))),
      field(t("admin.user"), h("select", { name: "user" }, userOptions(), groups.map((g) => h("option", { value: "@" + g.name, text: "@" + g.name })))),
      field(t("admin.role"), h("select", { name: "role" }, [["viewer", t("role.viewer")], ["editor", t("role.editor")], ["space-admin", t("role.space-admin")]].map(([r, label]) => h("option", { value: r, text: label })))),
      h("button", { type: "submit", text: t("admin.grantButton") }),
    ], async (d, st) => {
      await put(`/api/spaces/${encodeURIComponent(d.get("space"))}/roles/${encodeURIComponent(d.get("user"))}`, { role: d.get("role") });
      st.replaceChildren(h("p", { class: "notice", text: t("admin.granted") }));
    }));

  const backupSection = section("backup", t("admin.backup"),
    form({ id: "backup" }, [
      h("p", { class: "muted", text: t("admin.backupNote") }),
      field(t("admin.passphrase"), h("input", { name: "passphrase", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      field(t("admin.repeat"), h("input", { name: "repeat", type: "password", minlength: 12, required: true, autocomplete: "new-password" })),
      h("button", { type: "submit", text: t("admin.download") }),
    ], async (d, st) => {
      if (d.get("passphrase") !== d.get("repeat")) throw new Error(t("admin.differ"));
      const { blob, name } = await download("/api/backup", { passphrase: d.get("passphrase") });
      const url = URL.createObjectURL(blob);
      const a = h("a", { download: name, text: name });
      a.href = url; // a blob made here, not user content; safeHref would refuse the scheme
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
      st.replaceChildren(h("p", { class: "notice", text: t("admin.saved", { name }) }));
    }),
    autoBackupCard());

  const storageSection = section("storage", t("storage.title"), await storageCard());
  const albumSection = section("album", t("retention.title"), await retentionCard());
  const catalogSection = section("connectors", t("connector.title"), connectorsCard(spaces, users));
  const enginesSection = section("engines", t("engine.title"), enginesCard());
  const designSection = section("design", t("design.title"), await designCard());
  const regulationsSection = section("regulations", t("dp.adminTitle"), await regulationsCard());

  const importSection = section("import", t("admin.import"),
    h("p", { text: t("admin.importText") }),
    h("pre", { class: "snippet", text: "wikiwikibangbang import -help" }));

  // The trail is paged, so the server filters it (ADR-0098).
  const auditRows = h("tbody");
  const auditMore = h("div");
  const auditNone = h("p", { class: "empty filter-empty", id: "audit-filter-empty", text: t("filter.none"), hidden: true });
  const auditURL = (state, before) => {
    const p = filterParams(new URLSearchParams({ limit: 50 }), state);
    if (before) p.set("before", before);
    return "/api/audit?" + p;
  };
  const addAudit = (page) => {
    auditRows.append(...page.entries.map((e) => h("tr", {},
      h("td", { text: when(e.at) }), h("td", { text: e.event }), h("td", { text: e.actor ? person(e.actor) : "" }),
      h("td", { text: e.addr || "" }), h("td", { text: e.subject ? person(e.subject) : "" }),
      h("td", { text: Object.entries(e.detail || {}).map(([k, v]) => `${k}=${v}`).join(" ") }))));
    auditMore.replaceChildren(page.next ? h("button", { type: "button", id: "audit-more", text: t("common.more"),
      onclick: async () => addAudit(await get(auditURL(auditFilter.state, page.next))) }) : "");
  };
  const auditFilter = serverFilter({ id: "audit", label: t("admin.auditTitle"),
    choices: [
      { key: "event", label: t("admin.auditEvent"), options: [...new Set(auditPage.entries.map((e) => e.event))].sort().map((e) => [e, e]) },
      { key: "actor", label: t("filter.who"), options: users.map((u) => [u.id, u.username]) },
    ],
    load: async (state) => {
      auditRows.replaceChildren();
      const page = await get(auditURL(state));
      addAudit(page);
      auditNone.hidden = page.entries.length > 0;
      return page.entries.length + (page.next ? "+" : "");
    } });
  addAudit(auditFilter.active() ? await get(auditURL(auditFilter.state)) : auditPage);
  // The chain is checked on request: it reads the whole journal (ADR-0123).
  const auditChain = h("div", { id: "audit-chain" });
  const brokenBecause = {
    gap: () => t("admin.auditBroken.gap"), start: () => t("admin.auditBroken.start"),
    marker: () => t("admin.auditBroken.marker"), "too-young": () => t("admin.auditBroken.too-young"),
    link: () => t("admin.auditBroken.link"), unlinked: () => t("admin.auditBroken.unlinked"),
  };
  const verifyAudit = async () => {
    const c = await get("/api/audit/verify");
    const lines = c.broken
      ? [h("p", { class: "notice warning", text: t("admin.auditChainBroken", { seq: c.broken, reason: (brokenBecause[c.reason] || (() => c.reason))() }) })]
      : [h("p", { class: "notice success", text: t("admin.auditChainOk", { n: c.entries, seq: c.headSeq || 0 }) }),
        h("p", { class: "muted" }, t("admin.auditChainHead") + " ", h("code", { text: c.head || "" }))];
    if (c.unchained) lines.push(h("p", { class: "muted", text: t("admin.auditChainUnchained", { n: c.unchained }) }));
    auditChain.replaceChildren(...lines);
  };
  const auditSection = section("audit", t("admin.auditTitle"),
    h("p", { class: "muted", text: t("admin.auditVerifyHint") }),
    h("div", { class: "actions" }, h("button", { type: "button", id: "audit-verify", text: t("admin.auditVerify"), onclick: verifyAudit })),
    auditChain,
    auditFilter.el,
    h("table", { class: "history", id: "audit" }, auditRows),
    auditNone,
    auditMore);

  // The contents stay on the left; the section in view is marked.
  const tocLinks = sections.map(([el, title]) =>
    h("button", { type: "button", class: "link", text: title, onclick: () => el.scrollIntoView({ block: "start", behavior: "smooth" }) }));
  if ("IntersectionObserver" in window) {
    const seen = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        const at = sections.findIndex(([el]) => el === e.target);
        tocLinks.forEach((b, i) => b.classList.toggle("current", i === at));
      }
    }, { rootMargin: "-20% 0px -70% 0px" });
    sections.forEach(([el]) => seen.observe(el));
    leaving.push(() => seen.disconnect());
  }
  show(
    h("h1", { text: t("admin.title") }),
    h("p", { class: "muted", text: t("admin.lead") }),
    status,
    h("div", { class: "admin-layout" },
      h("nav", { class: "admin-toc", "aria-label": t("admin.contents") }, ...tocLinks),
      h("div", { class: "admin-sections" },
        spacesSection, typesSection, usersSection, requestsSection, groupsSection, grantSection, storageSection, backupSection, albumSection, catalogSection, enginesSection, designSection, regulationsSection, importSection, auditSection)),
  );
}

// storageLevel says a level of the storage in words.
function storageLevel(level) {
  return { warning: t("storage.level.warning"), critical: t("storage.level.critical"), full: t("storage.level.full") }[level] || t("storage.level.ok");
}

// storageCard shows how full the data directory's file system is, what
// takes the space and how fast it grows, and sets the thresholds below
// which administrators are told, uploads refused and, at the floor, the
// wiki only reads (ADR-0150).
async function storageCard() {
  const box = h("div", { id: "storage" });
  const status = h("div", { class: "status", role: "status" });
  const fail = (err) => status.replaceChildren(errorBox(err));
  const parts = [["events.wal", "storage.partLog"], ["attachments", "storage.partAttachments"], ["keys.wal", "storage.partKeys"],
    ["accounts.wal", "storage.partAccounts"], ["audit.wal", "storage.partAudit"], ["site.wal", "storage.partSite"], ["notes.wal", "storage.partNotes"]];
  const draw = (r) => {
    const used = r.total ? r.total - r.free : 0;
    const bar = h("span", { class: "storage-bar-used storage-" + r.level });
    if (r.total) bar.style.width = `${Math.min(100, (used * 100) / r.total)}%`;
    const share = r.total ? Math.round((r.free * 1000) / r.total) / 10 : null;
    const number = (name, label, value, min, max) => field(label, h("input", { type: "number", name, id: "storage-" + name, min, max, step: 1, required: true, value: String(value) }));
    box.replaceChildren(
      h("p", { id: "storage-level", class: "storage-level storage-" + r.level },
        h("strong", { text: storageLevel(r.level) }), " ",
        r.problem ? t("storage.unmeasured") : t("storage.free", { free: sizeText(r.free), total: sizeText(r.total), share })),
      r.total ? h("div", { class: "storage-bar", role: "img", "aria-label": t("storage.used", { used: sizeText(used) }) }, bar) : "",
      r.problem ? h("p", { class: "error", text: r.problem }) : "",
      h("table", { class: "history", id: "storage-parts" },
        parts.filter(([key]) => r.parts[key] !== undefined).map(([key, label]) => h("tr", {}, h("td", { text: t(label) }), h("td", { class: "num", text: sizeText(r.parts[key]) }))),
        h("tr", {}, h("th", { text: t("storage.data") }), h("th", { class: "num", text: sizeText(r.data) }))),
      h("p", { class: "muted", id: "storage-growth", text: r.growthPerDay > 0
        ? t("storage.growth", { day: sizeText(r.growthPerDay), days: r.daysLeft || "–" }) : t("storage.growthUnknown") }),
      h("p", { class: "muted", text: t("storage.help") }),
      form({ id: "storage-thresholds" }, [
        h("div", { class: "form-row" },
          number("warnPercent", t("storage.warnPercent"), r.thresholds.warnPercent, 1, 50), number("warnMB", t("storage.warnMB"), r.thresholds.warnMB, 0, 1048576),
          number("criticalPercent", t("storage.criticalPercent"), r.thresholds.criticalPercent, 1, 50), number("criticalMB", t("storage.criticalMB"), r.thresholds.criticalMB, 0, 1048576)),
        h("p", { class: "muted", text: t("storage.thresholdsHelp") }),
        h("button", { type: "submit", text: t("common.save") }),
        h("button", { type: "button", id: "storage-measure", text: t("storage.measure"), onclick: async () => {
          try { draw(await get("/api/storage?fresh=1")); } catch (err) { fail(err); }
        } }),
      ], async (d) => {
        const next = await put("/api/settings/storage", Object.fromEntries(["warnPercent", "warnMB", "criticalPercent", "criticalMB"].map((k) => [k, Number(d.get(k))])));
        draw(next);
        status.replaceChildren(h("p", { class: "notice", text: t("storage.saved") }));
      }),
    );
  };
  draw(await get("/api/storage"));
  return h("div", {}, box, status);
}

// holdBanner shows an item's open data protection review (ADR-0058): what
// was found under which regulation, who saved it, and, for a second person
// who may edit the item, the release.
async function holdBanner(item, path, canEdit) {
  const { regulations } = await get("/api/regulations").catch(() => ({ regulations: [] }));
  const reg = new Map(regulations.map((r) => [r.id, r]));
  await learnNames(item.hold.savers);
  const status = h("div", { class: "status" });
  const savedByMe = item.hold.savers.includes(me.user.id);
  const restricted = Boolean(item.restriction?.read?.length);
  const law = (id) => {
    const r = reg.get(id);
    if (!r) return id;
    return [r.url ? link(r.url, r.name) : r.name, r.document ? [" (", link(`/api/regulations/${id}/document`, t("dp.documentShort")), ")"] : null];
  };
  const release = canEdit && !savedByMe ? form({ class: "hold-release" }, [
    h("label", { class: "check" }, h("input", { type: "radio", name: "outcome", value: "restricted", checked: restricted, disabled: !restricted }), t("dp.outcomeRestricted")),
    restricted ? null : h("p", { class: "muted", text: t("dp.restrictFirst") }),
    h("label", { class: "check" }, h("input", { type: "radio", name: "outcome", value: "false-alarm", checked: !restricted }), t("dp.outcomeFalseAlarm")),
    field(t("dp.reason"), h("textarea", { name: "reason", rows: 2 })),
    h("button", { type: "submit", class: "primary", text: t("dp.release") }),
  ], async (d) => {
    await post(path + "/hold/release", { outcome: d.get("outcome"), reason: d.get("reason") || "" });
    route();
  }) : h("p", { class: "muted", text: savedByMe ? t("dp.fourEyes") : t("dp.editorsRelease") });
  return h("section", { class: "notice warning hold", id: "hold" },
    h("strong", { text: t("dp.heldTitle") }),
    h("p", { text: t("dp.heldText", { who: item.hold.savers.map(person).join(", ") }) }),
    h("ul", {}, item.hold.findings.map((f) => h("li", {}, h("span", { class: "chip", text: f.indicator }), " ", f.category, " – ", law(f.regulation)))),
    release, status);
}

// regulationsCard keeps the data protection regulations (ADR-0058): adopted
// from a template or written by hand, with a link to the law, its text as
// a PDF, and per category the indicators the content is assessed for.
async function regulationsCard() {
  const [{ regulations }, { templates }] = await Promise.all([get("/api/regulations"), get("/api/regulations/templates")]);
  const status = h("div", { class: "status" });
  const toText = (cats) => cats.map((c) => `${c.name}: ${c.indicators.join(", ")}`).join("\n");
  const fromText = (text) => text.split("\n").map((l) => l.trim()).filter(Boolean).map((l) => {
    const i = l.indexOf(":");
    if (i < 1) throw new Error(t("dp.categoryLine", { line: l }));
    return { name: l.slice(0, i).trim(), indicators: l.slice(i + 1).split(",").map((x) => x.trim()).filter(Boolean) };
  });
  const putFile = async (path, file) => {
    const res = await fetch(path, { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/octet-stream" }, body: file });
    if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
  };
  const editor = (r, isNew) => {
    const cats = h("textarea", { name: "categories", rows: 8, class: "code", spellcheck: "false", value: toText(r.categories || []), "aria-label": t("dp.categories"),
      placeholder: t("dp.categoriesExample") });
    const doc = h("input", { type: "file", accept: "application/pdf,.pdf", "aria-label": t("dp.document") });
    doc.addEventListener("change", () => doc.files[0] && action(() => putFile(`/api/regulations/${r.id}/document`, doc.files[0]), status));
    return h("div", { class: "regulation card-like", id: "regulation-" + (r.id || "new") },
      form({ class: "regulation-form" }, [
        isNew ? field(t("dp.id"), h("input", { name: "id", required: true, pattern: "[a-z0-9][a-z0-9\\-]{0,39}", placeholder: "intern" })) : h("h3", { text: r.name }),
        field(t("dp.name"), h("input", { name: "name", required: true, value: r.name || "", maxlength: 120 })),
        field(t("dp.url"), h("input", { name: "url", type: "url", value: r.url || "", placeholder: "https://" })),
        h("label", { class: "check" }, h("input", { type: "checkbox", name: "active", checked: Boolean(r.active) }), t("dp.active")),
        // Not a label: the info button inside would take the label's clicks.
        h("div", { class: "field" }, h("span", {}, t("dp.categories"), " ", infoTip(t("dp.categories"), t("dp.categoriesInfo"), t("dp.categoriesExample"))), cats),
        h("p", { class: "muted", text: t("dp.categoriesHint") }),
        h("p", { class: "actions-row" },
          h("button", { type: "submit", class: "primary", text: t("common.save") }),
          isNew ? null : h("button", { type: "button", class: "danger", text: t("common.delete"), onclick: () => confirm(t("dp.deleteConfirm", { name: r.name })) && action(() => del("/api/regulations/" + r.id), status) })),
      ], async (d) => {
        const id = isNew ? d.get("id").trim() : r.id;
        const categories = fromText(cats.value);
        if (!categories.length) throw new Error(t("dp.categoriesEmpty"));
        const bare = categories.find((c) => !c.indicators.length);
        if (bare) throw new Error(t("dp.indicatorsEmpty", { name: bare.name }));
        await put("/api/regulations/" + encodeURIComponent(id), { name: d.get("name").trim(), url: d.get("url").trim(), active: Boolean(d.get("active")), categories });
        route();
      }),
      isNew ? null : h("p", { class: "regulation-doc" },
        r.document ? [link(`/api/regulations/${r.id}/document`, t("dp.documentOpen")), " ", h("button", { type: "button", class: "link danger", text: t("common.delete"), onclick: () => action(() => del(`/api/regulations/${r.id}/document`), status) }), " · "] : null,
        h("label", { class: "upload" }, h("span", { text: r.document ? t("dp.documentReplace") : t("dp.documentAttach") }), doc)));
  };
  const have = new Set(regulations.map((r) => r.id));
  const missing = templates.filter((tpl) => !have.has(tpl.id));
  return h("div", {},
    h("p", { class: "muted", text: t("dp.adminIntro") }),
    status,
    missing.length ? h("p", { class: "actions-row" }, missing.map((tpl) => h("button", { type: "button", class: "secondary", text: t("dp.adopt", { name: tpl.name.split(" – ")[0] }),
      onclick: () => action(() => put("/api/regulations/" + tpl.id, { ...tpl, active: false }), status) }))) : null,
    regulations.length ? filtered(h("div", { class: "regulations" }, regulations.map((r) => {
      const el = editor(r, false);
      el.dataset.filterText = `${r.name} ${r.id}`;
      el.dataset.fState = r.active ? t("dp.activeShort") : t("dp.inactive");
      return el;
    })), { id: "regulations", label: t("dp.adminTitle"), choices: [{ key: "state", label: t("dp.status") }] }) : null,
    h("details", {}, h("summary", { text: t("dp.addOwn") }), editor({ categories: [] }, true)));
}

// designCard sets the corporate design (ADR-0052): name, logo, colours,
// background, font and an own stylesheet. Files are sent as they are
// chosen; the rest with Save.
// envHelp says where the settings that live in the server's environment
// are made: they are not entered in the UI, so that passwords and keys are
// in neither the data nor its backups.
function envHelp(id) {
  const snippet = (text) => h("pre", { class: "snippet", text });
  return h("details", { class: "env-help", id },
    h("summary", {}, infoIcon(), " ", t("envhelp.title")),
    h("p", { text: t("envhelp.what") }),
    h("h4", { text: t("envhelp.synology") }),
    h("p", { text: t("envhelp.synologySteps") }),
    h("h4", { text: "Docker Compose" }),
    h("p", { class: "muted", text: t("envhelp.compose") }),
    snippet([
      "services:",
      "  wikiwikibangbang:",
      "    image: ghcr.io/dbuchs7/wikiwikibangbang:latest",
      "    environment:",
      "      WWBB_PUBLIC_URL: https://wiki.example.org",
      "      WWBB_SMTP_HOST: smtp.example.org",
      "      WWBB_SMTP_PORT: \"587\"",
      "      WWBB_SMTP_USERNAME: wiki@example.org",
      "      WWBB_SMTP_PASSWORD: \"…\"",
      "      WWBB_SMTP_FROM: \"Wiki <wiki@example.org>\"",
      "      WWBB_BACKUP_PASSPHRASE: \"…\"",
    ].join("\n")),
    h("h4", { text: "docker run" }),
    snippet("docker run -d -p 8080:8080 -v wikiwikibangbang-data:/data \\\n  -e WWBB_PUBLIC_URL=https://wiki.example.org \\\n  -e WWBB_SMTP_HOST=smtp.example.org -e WWBB_SMTP_PORT=587 \\\n  -e WWBB_SMTP_USERNAME=wiki@example.org -e WWBB_SMTP_PASSWORD='…' \\\n  -e WWBB_SMTP_FROM='Wiki <wiki@example.org>' \\\n  ghcr.io/dbuchs7/wikiwikibangbang:latest"),
    h("h4", { text: t("envhelp.variables") }),
    h("ul", { class: "env-vars" },
      h("li", {}, h("strong", { text: t("envhelp.mail") }), " ", t("envhelp.mailVars")),
      h("li", {}, h("strong", { text: t("envhelp.sso") }), " ", t("envhelp.ssoVars")),
      h("li", {}, h("strong", { text: t("envhelp.backup") }), " ", t("envhelp.backupVars"))),
    h("p", { class: "muted", text: t("envhelp.restart") }));
}

// infoIcon is the small round i that marks help.
function infoIcon() {
  return h("span", { class: "info-icon", "aria-hidden": "true", text: "i" });
}

// infoButton opens a help block and brings it into view.
function infoButton(help) {
  return h("button", { type: "button", class: "info-button", title: t("envhelp.title"), "aria-label": t("envhelp.title"), onclick: () => {
    help.open = true;
    help.scrollIntoView({ block: "nearest", behavior: "smooth" });
  } }, infoIcon());
}

// enginesCard lists the process engines whose processes pages show in
// their newest version, and sets one up (ADR-0129). The credential is an
// environment variable; the card says which one and whether it is set,
// and checks the engine by listing its processes.
function enginesCard() {
  const card = h("div", { id: "engines" }, h("p", { class: "muted", text: t("autobackup.loading") }));
  let open = null;
  const load = async () => {
    let data;
    try {
      data = await get("/api/engines");
    } catch (err) {
      card.replaceChildren(errorBox(err));
      return;
    }
    const chosen = open === "new" ? { id: "", name: "", kind: data.kinds[0], baseURL: "", operations: false, isNew: true } : data.engines.find((e) => e.id === open);
    card.replaceChildren(
      h("p", { class: "muted", text: t("engine.note") }),
      data.engines.length ? h("table", { id: "engine-list" },
        h("thead", {}, h("tr", {}, [t("engine.name"), t("engine.kind"), t("engine.baseURL"), t("engine.operations"), t("engine.credential"), ""].map((x) => h("th", { text: x })))),
        h("tbody", {}, data.engines.map((e) => h("tr", { "data-engine": e.id },
          h("td", {}, h("strong", { text: e.name }), h("br"), h("span", { class: "muted", text: e.id })),
          h("td", { text: e.kind }),
          h("td", { class: "muted", text: e.baseURL }),
          h("td", { text: e.operations ? t("engine.operationsOn") : t("engine.operationsOff") }),
          h("td", { class: e.credential ? "" : "error", text: e.credential ? t("engine.credentialSet") : t("engine.credentialUnset") }),
          h("td", {}, h("button", { type: "button", class: "link", text: t("common.edit"), onclick: () => { open = e.id; load(); } })))))) : empty(t("engine.none")),
      h("p", {}, h("button", { type: "button", id: "engine-new", text: t("engine.new"), onclick: () => { open = "new"; load(); } })),
      chosen ? engineEditor(chosen, data.kinds, data.engines.map((x) => x.id), (id) => { open = id; load(); }) : "");
  };
  load();
  return card;
}

// engineEditor sets up one process engine; taken are the IDs in use, which
// a new one may not take over. reopen(id) shows the card again with id
// open, or none.
function engineEditor(e, kinds, taken, reopen) {
  const checkStatus = h("div", { class: "status", id: "engine-check" });
  const path = (id) => "/api/engines/" + encodeURIComponent(id);
  return h("div", { class: "connector-editor", id: "engine-editor" },
    h("h3", { text: e.isNew ? t("engine.newTitle") : e.name }),
    form({ id: "engine-settings" }, [
      field(t("engine.id"), h("input", { name: "id", value: e.id, required: true, readonly: !e.isNew, pattern: "[a-z0-9][a-z0-9.\\-]{0,39}", maxlength: 40 })),
      e.isNew ? h("p", { class: "muted", text: t("engine.idHelp") }) : "",
      field(t("engine.name"), h("input", { name: "name", value: e.name, required: true, maxlength: 80 })),
      field(t("engine.kind"), h("select", { name: "kind" }, kinds.map((k) => h("option", { value: k, text: k, selected: k === e.kind })))),
      field(t("engine.baseURL"), h("input", { name: "baseURL", type: "url", required: true, value: e.baseURL, placeholder: "https://atlas.example.org" })),
      h("label", { class: "check" }, h("input", { type: "checkbox", name: "operations", checked: e.operations }), " ", t("engine.allowOperations")),
      h("p", { class: "muted", text: t("engine.operationsHelp") }),
      h("p", { class: "muted", text: t("engine.publicOnly") }),
      e.isNew ? h("p", { class: "muted", text: t("engine.credentialHelpNew") })
        : h("p", { class: e.credential ? "muted" : "error", id: "engine-credential",
          text: e.credential ? t("engine.credentialIs", { name: e.credentialVar }) : t("engine.credentialMissing", { name: e.credentialVar }) }),
      e.isNew ? "" : envHelp("engine-env-help"),
      h("div", { class: "actions-row" }, h("button", { type: "submit", class: "primary", text: t("engine.save") }),
        e.isNew ? h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => reopen(null) })
          : h("button", { type: "button", class: "secondary", id: "engine-check-now", text: t("engine.check"), onclick: async () => {
            checkStatus.replaceChildren(h("p", { class: "muted", text: t("autobackup.loading") }));
            try {
              const { processes } = await get(path(e.id) + "/processes");
              checkStatus.replaceChildren(h("p", { class: "notice", text: t("engine.checkOk", { n: processes.length }) }));
            } catch (err) {
              checkStatus.replaceChildren(errorBox(err));
            }
          } }),
        e.isNew ? "" : h("button", { type: "button", class: "link danger", id: "engine-delete", text: t("engine.remove"), onclick: async () => {
          if (!confirm(t("engine.removeConfirm", { name: e.name }))) return;
          try {
            await del(path(e.id));
            reopen(null);
          } catch (err) {
            checkStatus.replaceChildren(errorBox(err));
          }
        } })),
    ], async (d) => {
      const id = d.get("id").trim();
      if (e.isNew && taken.includes(id)) throw new Error(t("engine.taken", { id }));
      const kept = await put(path(id), { name: d.get("name").trim(), kind: d.get("kind"), baseURL: d.get("baseURL").trim(), operations: d.get("operations") === "on" });
      reopen(kept.id);
    }),
    checkStatus,
    // Its user tasks become linked tasks by the rules set here (ADR-0151).
    e.isNew ? "" : taskLinksCard(e));
}

// connectorsCard lists the connectors that keep catalog spaces in step
// with external sources, such as opendata.swiss, and opens one to set it
// up and run it (ADR-0077). A dry run reports what a run would change;
// while one runs the card looks again.
function connectorsCard(spaces, users) {
  const card = h("div", { id: "connectors" }, h("p", { class: "muted", text: t("autobackup.loading") }));
  let timer = 0;
  // The connector open in the editor: its ID, or a template for a new one.
  let open = null;
  const list = (v) => v.split(/[\s,;]+/).map((x) => x.trim()).filter(Boolean);
  const load = async () => {
    clearTimeout(timer);
    if (!card.isConnected && timer) return;
    let data;
    try {
      data = await get("/api/connectors");
    } catch (err) {
      card.replaceChildren(errorBox(err));
      return;
    }
    const last = (c) => c.status.history[0];
    const rows = data.connectors.map((c) => h("tr", { "data-connector": c.id, "data-f-state": c.enabled ? t("connector.on") : t("connector.off") },
      h("td", {}, h("strong", { text: c.name }), h("br"), h("span", { class: "muted", text: c.id })),
      h("td", { text: c.kind.toUpperCase() }),
      h("td", { text: c.space || "—" }),
      h("td", { text: c.status.running ? t("catalog.running") : last(c) ? `${when(last(c).started)} · ${last(c).error ? t("connector.failed") : t("connector.entries", { n: last(c).entries })}` : "—" }),
      h("td", { text: c.enabled ? t("connector.on") : t("connector.off") }),
      h("td", {}, h("button", { type: "button", class: "link", text: t("common.edit"), onclick: () => { open = c.id; load(); } }))));
    // A new connector starts from its kind's template (ADR-0127).
    const fresh = (i) => {
      const tpl = structuredClone(data.templates[i]);
      if (data.connectors.some((c) => c.id === tpl.id)) tpl.id = "";
      return { ...tpl, status: { running: false, history: [], credential: false, credentialVar: "" }, isNew: true };
    };
    const chosen = typeof open === "number" ? fresh(open) : data.connectors.find((c) => c.id === open);
    card.replaceChildren(
      h("p", { class: "muted", text: t("connector.note") }),
      data.connectors.length ? filtered(h("table", { id: "connector-list" },
        h("thead", {}, h("tr", {}, [t("connector.name"), t("connector.kind"), t("catalog.space"), t("connector.lastRun"), t("connector.schedule"), ""].map((x) => h("th", { text: x })))),
        h("tbody", {}, rows)), { id: "connectors", label: t("connector.title"), choices: [{ key: "state", label: t("connector.schedule") }] }) : empty(t("connector.none")),
      h("p", { class: "actions-row" }, data.templates.map((tpl, i) => h("button", { type: "button", class: i ? "secondary" : null,
        id: i ? "connector-new-" + (tpl.json?.spec !== undefined ? "openapi" : tpl.kind) : "connector-new",
        text: t("connector.new", { name: tpl.name }), onclick: () => { open = i; load(); } }))),
      chosen ? connectorEditor(chosen, data, spaces, users, list, (id) => { open = id; load(); }) : "",
    );
    if (data.connectors.some((c) => c.status.running)) timer = setTimeout(load, 1500);
  };
  load();
  return card;
}

// connectorEditor sets up one connector, runs it and shows its last
// runs; reopen(id) shows the card again with id open, or none. Its source
// is set up as its kind needs; what it writes, the type and which of its
// fields take which of the source's values, alike for every kind
// (ADR-0127).
function connectorEditor(c, data, spaces, users, list, reopen) {
  const k = c.ckan || { formats: [], organizations: [], baseURL: "", portalURL: "", language: "de" };
  const j = c.json;
  const g = c.git;
  // An Atlassian import names its spaces, types and fields itself
  // (ADR-0128): it has no space, type or steward of its own.
  const a = c.atlassian;
  const path = "/api/connectors/" + encodeURIComponent(c.id);
  const st = c.status;
  const runStatus = h("div", { class: "status", id: "connector-run-status" });
  // The formats the source uses, to tick the ones that are interfaces:
  // the source names them its own way, e.g. "WMS-Dienst".
  const formatsInput = h("input", { name: "formats", value: k.formats.join(", ") });
  const formatsBox = h("div", { id: "connector-formats" });
  const editor = h("div", { class: "connector-editor", id: "connector-editor" });
  const showFormats = async () => {
    formatsBox.replaceChildren(h("p", { class: "muted", text: t("catalog.loadingFormats") }));
    try {
      const form = editor.querySelector("#connector-settings");
      const id = form.querySelector("[name=id]").value.trim() || "new";
      const { formats } = await get(`/api/connectors/${encodeURIComponent(id)}/formats?` + new URLSearchParams({
        baseURL: form.querySelector("[name=baseURL]").value.trim(), organizations: list(form.querySelector("[name=organizations]").value).join(","),
      }));
      const chosen = new Set(list(formatsInput.value).map((f) => f.toUpperCase()));
      const boxes = formats.map((f) => h("input", { type: "checkbox", value: f.format, checked: chosen.has(f.format.toUpperCase()) }));
      const apply = () => {
        const picked = boxes.filter((b) => b.checked).map((b) => b.value.toUpperCase());
        const others = list(formatsInput.value).filter((f) => !formats.some((x) => x.format.toUpperCase() === f.toUpperCase()));
        formatsInput.value = [...picked, ...others].join(", ");
      };
      boxes.forEach((b) => b.addEventListener("change", apply));
      formatsBox.replaceChildren(
        h("p", { class: "muted", text: t("catalog.formatsPick", { n: formats.length }) }),
        formats.length ? h("div", { class: "format-list" }, formats.map((f, i) => h("label", { class: "check" }, boxes[i], " ", f.format, " ",
          h("span", { class: "muted", text: `(${f.count})` })))) : h("p", { class: "muted", text: t("catalog.noFormats") }));
    } catch (err) {
      formatsBox.replaceChildren(errorBox(err));
    }
  };
  // The further values of a JSON source, one "name = path" a line.
  const valuesInput = h("textarea", { name: "values", rows: 4, placeholder: "owner = contact.name",
    value: Object.entries(j?.values || {}).map(([n, p]) => `${n} = ${p}`).join("\n") });
  const parseValues = (v) => Object.fromEntries(v.split("\n").map((l) => l.split("=")).filter((p) => p.length > 1 && p[0].trim())
    .map(([n, ...p]) => [n.trim(), p.join("=").trim()]));
  const sourceValues = () => j ? Object.keys(parseValues(valuesInput.value)).sort() : g ? data.gitValues : data.ckanValues;
  // The type the entries are written as: page, or another with a text.
  const typeNames = [data.type, ...data.types.map((tp) => tp.name).filter((n) => n !== data.type)];
  const typeSelect = h("select", { name: "type", id: "connector-type" }, typeNames.map((n) => h("option", { value: n === data.type ? "" : n, text: n,
    selected: (c.type || "") === (n === data.type ? "" : n) })));
  const mapping = h("div", { id: "connector-fields" });
  const showFields = () => {
    const tp = data.types.find((x) => x.name === (typeSelect.value || data.type));
    const fields = (tp?.fields || []).filter((f) => f.kind !== "user");
    const names = sourceValues();
    const now = Object.fromEntries([...mapping.querySelectorAll("select[data-field]")].map((x) => [x.dataset.field, x.value]));
    mapping.replaceChildren(fields.length ? h("table", { class: "connector-fields" },
      h("thead", {}, h("tr", {}, [t("connector.field"), t("connector.value")].map((x) => h("th", { text: x })))),
      h("tbody", {}, fields.map((f) => h("tr", {},
        h("td", {}, f.name, " ", h("span", { class: "muted", text: `(${f.kind}${f.required ? ", " + t("connector.required") : ""})` })),
        h("td", {}, h("select", { "data-field": f.name, "aria-label": f.name }, h("option", { value: "", text: "—" }),
          names.map((n) => h("option", { value: n, text: n, selected: (now[f.name] ?? c.fields?.[f.name]) === n }))))))))
      : h("p", { class: "muted", text: t("connector.noFields") }));
  };
  typeSelect.addEventListener("change", showFields);
  valuesInput.addEventListener("change", showFields);
  showFields();
  const path_ = (name, label, value, required) => field(label, h("input", { name, value: value || "", required: !!required, maxlength: 200 }));
  // How a long list is read (ADR-0135): by the next page's address the
  // answer names, or by a page number or an offset in the address.
  const pg = j?.paging || {};
  const pagingKind = h("select", { name: "pagingKind", id: "paging-kind" }, [["", t("paging.next")], ["page", t("paging.page")], ["offset", t("paging.offset")]]
    .map(([v, text]) => h("option", { value: v, text, selected: v === (pg.kind || "") })));
  const nextBox = h("div", {}, path_("next", t("json.next"), j?.next));
  const pagingBox = h("div", { id: "paging-params" },
    field(t("paging.param"), h("input", { name: "pagingParam", value: pg.param || "", maxlength: 64, placeholder: "page" })),
    field(t("paging.start"), h("input", { name: "pagingStart", type: "number", min: 0, max: 1000000, value: pg.start ?? 1 })),
    field(t("paging.sizeParam"), h("input", { name: "sizeParam", value: pg.sizeParam || "", maxlength: 64, placeholder: "limit" })),
    field(t("paging.size"), h("input", { name: "pageSize", type: "number", min: 0, max: 10000, value: pg.size || "" })),
    path_("totalPath", t("paging.total"), pg.total),
    h("p", { class: "muted", text: t("paging.help") }));
  const showPaging = () => {
    nextBox.hidden = pagingKind.value !== "";
    pagingBox.hidden = pagingKind.value === "";
    pagingBox.querySelector("[name=pagingParam]").required = pagingKind.value !== "";
  };
  // Only what changed since the last run (ADR-0140): the parameter that
  // asks for it, the time's format, and how often a run reads in full.
  const sc = j?.since || {};
  const sinceBox = h("fieldset", { class: "connector-target", id: "since" }, h("legend", { text: t("since.title") }),
    field(t("since.param"), h("input", { name: "sinceParam", value: sc.param || "", maxlength: 64, placeholder: "updated_since" })),
    field(t("since.format"), h("select", { name: "sinceFormat" }, [["rfc3339", "2026-10-26T13:00:00Z"], ["date", "2026-10-26"], ["unix", t("since.unix")], ["unixms", t("since.unixms")]]
      .map(([v, text]) => h("option", { value: v, text, selected: v === (sc.format || "rfc3339") })))),
    field(t("since.fullDays"), h("input", { name: "fullDays", type: "number", min: 1, max: 90, value: sc.fullDays || 7 })),
    h("p", { class: "muted", text: t("since.help") }));
  pagingKind.addEventListener("change", () => {
    // Offsets count from 0, pages mostly from 1.
    const start = pagingBox.querySelector("[name=pagingStart]");
    if (pagingKind.value === "offset" && start.value === "1") start.value = "0";
    showPaging();
  });
  showPaging();
  const ckanSource = () => [
    field(t("catalog.formats"), formatsInput),
    h("p", { class: "muted" }, t("catalog.formatsHelp", { defaults: data.templates[0].ckan.formats.join(", ") }), " ",
      h("button", { type: "button", class: "link", id: "connector-source-formats", text: t("catalog.showFormats"), onclick: showFormats })),
    formatsBox,
    field(t("connector.language"), languageSelect(k.language)),
    h("p", { class: "muted", text: t("connector.languageHelp") }),
    field(t("catalog.organizations"), h("input", { name: "organizations", value: k.organizations.join(", "), placeholder: "bundesamt-fur-statistik-bfs, …" })),
    h("p", { class: "muted", text: t("catalog.organizationsHelp") }),
  ];
  // An API description offers its lists (ADR-0130): one picked fills
  // the settings below, which stay the connector's.
  const specInput = h("input", { name: "specURL", type: "url", value: j?.spec || "", placeholder: "https://api.example/openapi.yaml", maxlength: 512 });
  const picked = { spec: j?.operation ? j.spec : "", operation: j?.operation || "" };
  const pickedNote = h("p", { class: "muted", id: "openapi-picked", text: picked.operation ? t("openapi.picked", { operation: picked.operation }) : "" });
  const listsBox = h("div", { id: "openapi-lists" });
  const showLists = async () => {
    listsBox.replaceChildren(h("p", { class: "muted", text: t("openapi.loading") }));
    try {
      const id = editor.querySelector("#connector-settings [name=id]").value.trim() || "new";
      const d = await get(`/api/connectors/${encodeURIComponent(id)}/openapi?` + new URLSearchParams({ url: specInput.value.trim() }));
      const use = (l) => {
        const form = editor.querySelector("#connector-settings");
        const set = (name, v) => { form.querySelector(`[name=${name}]`).value = v || ""; };
        const s = l.source;
        set("url", s.url); set("items", s.items); set("idPath", s.id); set("titlePath", s.title);
        set("link", s.link); set("text", s.text); set("next", s.next);
        const p = s.paging || {};
        pagingKind.value = p.kind || "";
        set("pagingParam", p.param); set("pagingStart", p.start ?? (p.kind === "offset" ? 0 : 1)); set("sizeParam", p.sizeParam);
        set("pageSize", p.size || ""); set("totalPath", p.total);
        const sn = s.since || {};
        set("sinceParam", sn.param); set("sinceFormat", sn.format || "rfc3339"); set("fullDays", sn.fullDays || 7);
        showPaging();
        valuesInput.value = Object.entries(s.values || {}).map(([n, p]) => `${n} = ${p}`).join("\n");
        showFields();
        picked.spec = s.spec; picked.operation = s.operation;
        pickedNote.textContent = t("openapi.picked", { operation: s.operation });
        listsBox.replaceChildren();
      };
      listsBox.replaceChildren(
        h("p", { class: "muted", text: t("openapi.found", { title: d.title || specInput.value.trim(), n: d.lists.length }) }),
        d.lists.length ? h("ul", { class: "openapi-lists" }, d.lists.map((l) => h("li", {},
          h("button", { type: "button", class: "link", "data-operation": l.operation, text: l.operation, onclick: () => use(l) }),
          l.summary ? h("span", { class: "muted", text: " · " + l.summary }) : ""))) : "",
        d.problems.length ? h("details", {}, h("summary", { text: t("openapi.skipped", { n: d.problems.length }) }),
          h("ul", {}, d.problems.map((p) => h("li", { class: "muted", text: p })))) : "");
    } catch (err) {
      listsBox.replaceChildren(errorBox(err));
    }
  };
  const jsonSource = () => [
    h("fieldset", { class: "connector-target", id: "openapi" }, h("legend", { text: t("openapi.title") }),
      field(t("openapi.spec"), specInput),
      h("p", { class: "muted" }, t("openapi.help"), " ",
        h("button", { type: "button", class: "link", id: "openapi-read", text: t("openapi.read"), onclick: showLists })),
      listsBox, pickedNote),
    field(t("json.url"), h("input", { name: "url", type: "url", required: true, value: j.url, maxlength: 512 })),
    h("p", { class: "muted", text: t("json.urlHelp") }),
    path_("items", t("json.items"), j.items),
    path_("idPath", t("json.id"), j.id, true),
    path_("titlePath", t("json.title"), j.title, true),
    path_("link", t("json.link"), j.link),
    path_("text", t("json.text"), j.text),
    field(t("paging.kind"), pagingKind),
    nextBox,
    pagingBox,
    sinceBox,
    h("p", { class: "muted", text: t("json.pathsHelp") }),
    field(t("json.values"), valuesInput),
    h("p", { class: "muted", text: t("json.valuesHelp") }),
    field(t("connector.language"), languageSelect(j.language)),
    h("p", { class: "muted", text: t("json.languageHelp") }),
  ];
  // A folder or file of a Git repository (ADR-0138).
  const gitSource = () => [
    field(t("git.url"), h("input", { name: "url", type: "url", required: true, value: g.url, maxlength: 512, placeholder: "https://github.com/owner/repository/tree/main/docs" })),
    h("p", { class: "muted", text: t("git.urlHelp") }),
    field(t("connector.language"), languageSelect(g.language)),
    h("p", { class: "muted", text: t("git.languageHelp") }),
  ];
  // The source accounts, groups and roles of an Atlassian import, one a
  // line: "accountId username", "source group @local-group", "username=role".
  const lines = (v) => v.split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
  const pairs = (v) => Object.fromEntries(lines(v).map((l) => {
    const i = l.search(/\s+\S*$/);
    return i < 0 ? [l, ""] : [l.slice(0, i).trim(), l.slice(i).trim()];
  }));
  const atlassianSource = () => [
    field(t("atlassian.site"), h("input", { name: "site", type: "url", required: true, value: a.site, maxlength: 512 })),
    field(t("atlassian.spaces"), h("input", { name: "spaces", value: a.spaces.join(", "), placeholder: "ENG, OPS" })),
    field(t("atlassian.projects"), h("input", { name: "projects", value: a.projects.join(", "), placeholder: "PLAT" })),
    h("p", { class: "muted", text: t("atlassian.keysHelp") }),
    field(t("atlassian.users"), h("textarea", { name: "users", rows: 4, placeholder: "5b10ac8d82e05b22cc7d4ef5 anna",
      value: Object.entries(a.users || {}).map(([id, name]) => `${id} ${name}`).join("\n") })),
    h("p", { class: "muted", text: t("atlassian.usersHelp") }),
    field(t("atlassian.groups"), h("textarea", { name: "groups", rows: 3, placeholder: "dev team @engineering",
      value: Object.entries(a.groups || {}).map(([src, local]) => `${src} ${local}`).join("\n") })),
    field(t("atlassian.grants"), h("textarea", { name: "grants", rows: 3, placeholder: "@engineering=editor", value: (a.grants || []).join("\n") })),
    h("p", { class: "muted", text: t("atlassian.grantsHelp") }),
  ];
  // The cut-over: the items become people's, the schedule stops.
  const release = () => h("button", { type: "button", class: "secondary", id: "connector-release", disabled: st.running, text: t("atlassian.release"), onclick: async () => {
    if (!confirm(t("atlassian.releaseConfirm", { name: c.name }))) return;
    runStatus.replaceChildren();
    try {
      await post(path + "/release", {});
      reopen(c.id);
    } catch (err) {
      runStatus.replaceChildren(errorBox(err));
    }
  } });
  const run = (dryRun, id, text, full) => h("button", { type: "button", class: dryRun || full ? "secondary" : "primary", id, disabled: st.running || !c.space && !a, text, onclick: async () => {
    runStatus.replaceChildren();
    try {
      await post(path + "/run", { dryRun, full: !!full });
      reopen(c.id);
    } catch (err) {
      runStatus.replaceChildren(errorBox(err));
    }
  } });
  const result = (r) => h("details", { class: "catalog-run", open: r === st.history[0],
    "data-f-outcome": r.error ? t("connector.failed") : r.dryRun ? t("catalog.dryRun") : t("catalog.run") },
    h("summary", {}, h("strong", { text: r.dryRun ? t("catalog.dryRun") : t("catalog.run") }), " · ", when(r.started), " · ",
      r.error ? h("span", { class: "error", text: r.error })
        : r.same ? t("git.same", { commit: r.version.slice(0, 8) })
          : t(g ? "git.counts" : "catalog.counts", { datasets: r.datasets, entries: r.entries, created: r.new, changed: r.changed, unchanged: r.unchanged, withdrawn: r.withdrawn, removed: r.removed || 0 })),
    r.version && !r.same && !r.error ? h("p", { class: "muted", text: t("git.read", { commit: r.version.slice(0, 8) }) }) : "",
    r.formats?.length ? h("p", { class: "muted", text: t("catalog.formatsRead", { list: r.formats.join(", ") }) }) : "",
    r.since ? h("p", { class: "muted run-since", text: t("since.run", { when: when(r.since) }) }) : "",
    r.examples.length ? h("ul", {}, r.examples.map((x) => h("li", {}, h("span", { class: "chip", text: { new: t("catalog.kind.new"), changed: t("catalog.kind.changed"), withdrawn: t("catalog.kind.withdrawn"), removed: t("catalog.kind.removed") }[x.kind] }), " ",
      x.key ? link("#/item/" + encodeURIComponent(x.key), `${x.key} ${x.title}`) : x.title))) : "",
    r.problems.length ? filtered(h("ul", { class: "catalog-problems" }, r.problems.map((p) => h("li", { class: "muted", text: p }))),
      { id: "run-problems-" + Date.parse(r.started), label: t("catalog.problems") }) : "");
  editor.replaceChildren(
    h("h3", { text: c.isNew ? t("connector.newTitle") : c.name }),
    h("p", { class: "muted", text: a ? t("atlassian.note") : j ? t("json.note") : g ? t("git.note") : t("catalog.note") }),
    h("p", { class: "muted", text: t("connector.publicOnly") }),
    form({ id: "connector-settings" }, [
      field(t("connector.id"), h("input", { name: "id", value: c.id, required: true, readonly: !c.isNew, pattern: "[a-z0-9][a-z0-9.\\-]{0,39}", maxlength: 40 })),
      c.isNew ? h("p", { class: "muted", text: t("connector.idHelp") }) : "",
      field(t("connector.name"), h("input", { name: "name", value: c.name, required: true, maxlength: 80 })),
      h("label", { class: "check" }, h("input", { type: "checkbox", name: "enabled", checked: c.enabled }), " ", t("catalog.enabled")),
      a ? "" : field(t("catalog.space"), h("select", { name: "space", required: true }, h("option", { value: "", text: "—" }),
        favouritesFirst(spaces).map((sp) => h("option", { value: sp.key, text: `${sp.name} (${sp.key})`, selected: sp.key === c.space })))),
      a ? atlassianSource() : j ? jsonSource() : g ? gitSource() : ckanSource(),
      a ? "" : h("fieldset", { class: "connector-target" }, h("legend", { text: t("connector.target") }),
        field(t("connector.type"), typeSelect),
        h("p", { class: "muted", text: t("connector.targetHelp") }),
        mapping,
        h("p", { class: "muted", text: t("connector.fieldsHelp") })),
      a ? "" : field(t("catalog.steward"), h("select", { name: "steward" }, h("option", { value: "", text: "—" }),
        users.map((u) => h("option", { value: u.id, text: u.username, selected: u.id === c.steward })))),
      a ? "" : h("p", { class: "muted", text: t("catalog.stewardHelp") }),
      field(t("catalog.interval"), h("input", { name: "intervalHours", type: "number", min: 1, max: 744, required: true, value: c.intervalHours })),
      j || a || g ? "" : h("details", {}, h("summary", { text: t("catalog.source") }),
        field(t("catalog.baseURL"), h("input", { name: "baseURL", type: "url", required: true, value: k.baseURL })),
        field(t("catalog.portalURL"), h("input", { name: "portalURL", type: "url", required: true, value: k.portalURL }))),
      c.isNew ? "" : h("p", { class: "muted", id: "connector-credential",
        text: a ? t(st.credential && st.email ? "atlassian.credentialSet" : "atlassian.credentialUnset", { email: st.emailVar, token: st.credentialVar })
          : st.credential ? t("connector.credentialSet", { name: st.credentialVar }) : t("connector.credentialUnset", { name: st.credentialVar }) }),
      h("div", { class: "actions-row" }, h("button", { type: "submit", class: "primary", text: t("autobackup.save") }),
        c.isNew ? h("button", { type: "button", class: "link", text: t("common.cancel"), onclick: () => reopen(null) })
          : h("button", { type: "button", class: "link danger", id: "connector-delete", text: t("connector.remove"), onclick: async () => {
            if (!confirm(t("connector.removeConfirm", { name: c.name }))) return;
            try {
              await del(path);
              reopen(null);
            } catch (err) {
              runStatus.replaceChildren(errorBox(err));
            }
          } })),
    ], async (d) => {
      const fields = Object.fromEntries([...mapping.querySelectorAll("select[data-field]")].filter((x) => x.value).map((x) => [x.dataset.field, x.value]));
      const body = {
        id: d.get("id").trim(), name: d.get("name").trim(), kind: c.kind, enabled: d.get("enabled") === "on", space: d.get("space") || "",
        steward: d.get("steward") || "", intervalHours: Number(d.get("intervalHours")), type: a ? "" : d.get("type"), fields: a ? {} : fields,
      };
      if (g) {
        body.git = { url: d.get("url").trim(), language: d.get("language") };
      } else if (a) {
        body.atlassian = { site: d.get("site").trim(), spaces: list(d.get("spaces")), projects: list(d.get("projects")), users: pairs(d.get("users")),
          groups: pairs(d.get("groups")), grants: lines(d.get("grants")) };
      } else if (j) {
        body.json = { url: d.get("url").trim(), items: d.get("items").trim(), id: d.get("idPath").trim(), title: d.get("titlePath").trim(), link: d.get("link").trim(),
          text: d.get("text").trim(), next: d.get("pagingKind") ? "" : d.get("next").trim(), values: parseValues(d.get("values")), language: d.get("language"),
          paging: d.get("pagingKind") ? { kind: d.get("pagingKind"), param: d.get("pagingParam").trim(), start: Number(d.get("pagingStart")) || 0,
            sizeParam: d.get("sizeParam").trim(), size: Number(d.get("pageSize")) || 0, total: d.get("totalPath").trim() } : null,
          since: d.get("sinceParam").trim() ? { param: d.get("sinceParam").trim(), format: d.get("sinceFormat"), fullDays: Number(d.get("fullDays")) || 7 } : null,
          spec: picked.spec, operation: picked.operation };
      } else {
        body.ckan = { formats: list(d.get("formats")), organizations: list(d.get("organizations")), baseURL: d.get("baseURL").trim(), portalURL: d.get("portalURL").trim(), language: d.get("language") };
      }
      const kept = c.isNew ? await post("/api/connectors", body) : await put(path, body);
      reopen(kept.id);
    }),
    c.isNew ? "" : h("p", { id: "connector-next" }, st.running ? t("catalog.running") : st.next ? t("catalog.next", { when: when(st.next) }) : t("catalog.off")),
    c.isNew ? "" : h("div", { class: "actions-row" }, run(true, "connector-dry-run", t("catalog.dryRunNow")), run(false, "connector-run-now", t("catalog.runNow")),
      j?.since ? run(false, "connector-run-full", t("since.runFull"), true) : "",
      c.space ? link("#/space/" + encodeURIComponent(c.space), t("catalog.openSpace"), "button") : "", a ? release() : ""),
    c.isNew || !a ? "" : h("p", { class: "muted", text: t("atlassian.releaseHelp") }),
    runStatus,
    c.isNew ? "" : st.history.length ? filtered(h("div", { id: "connector-history" }, st.history.map(result)),
      { id: "connector-runs", label: t("connector.lastRun"), choices: [{ key: "outcome", label: t("filter.kind") }] }) : h("p", { class: "muted", text: t("catalog.noRuns") }),
  );
  return editor;
}

// languageSelect chooses the language a connector writes its pages in.
function languageSelect(value) {
  return h("select", { name: "language" }, [["de", "Deutsch"], ["fr", "Français"], ["it", "Italiano"], ["en", "English"]]
    .map(([v, text]) => h("option", { value: v, text, selected: v === (value || "de") })));
}

// autoBackupCard sets up automatic backups and shows how they went
// (ADR-0073). It fills itself, because listing the destination may take
// a while; while a backup runs it looks again every few seconds.
function autoBackupCard() {
  const card = h("div", { id: "autobackup" }, h("h3", { text: t("autobackup.title") }), h("p", { class: "muted", text: t("autobackup.loading") }));
  let timer = 0;
  const kindText = (k) => k === "incremental" ? t("autobackup.incremental") : t("autobackup.full");
  const load = async () => {
    clearTimeout(timer);
    if (!card.isConnected && timer) return;
    let data;
    try {
      data = await get("/api/backups");
    } catch (err) {
      card.replaceChildren(h("h3", { text: t("autobackup.title") }), errorBox(err));
      return;
    }
    const { settings: s, status: st } = data;
    const destination = h("input", { name: "destination", id: "autobackup-destination", value: s.destination, placeholder: t("autobackup.destinationExample") });
    const backupHelp = envHelp("backup-destination-help");
    const checkStatus = h("div", { class: "status", id: "autobackup-check" });
    const runStatus = h("div", { class: "status", id: "autobackup-run-status" });
    const run = (kind) => h("button", { type: "button", class: "secondary", id: "autobackup-run-" + kind, disabled: !!st.running || !s.destination,
      text: t("autobackup.runNow", { kind: kindText(kind) }), onclick: async () => {
        runStatus.replaceChildren();
        try {
          await post("/api/backups/run", { kind });
          await load();
        } catch (err) {
          runStatus.replaceChildren(errorBox(err));
        }
      } });
    card.replaceChildren(
      h("h3", { text: t("autobackup.title") }),
      h("p", { class: "muted", text: t("autobackup.note") }),
      st.passphrase ? "" : h("div", { class: "notice warning", id: "autobackup-passphrase" },
        h("p", { text: t("autobackup.noPassphrase", { env: data.passphraseEnv, n: data.minPassphrase }) }), envHelp("backup-env-help")),
      form({ id: "autobackup-settings" }, [
        h("label", { class: "check" }, h("input", { type: "checkbox", name: "enabled", checked: s.enabled }), " ", t("autobackup.enabled")),
        field(t("autobackup.destination"), destination),
        h("p", { class: "muted" }, t("autobackup.destinationHelp"), " ", infoButton(backupHelp)),
        backupHelp,
        field(t("autobackup.fullDays"), h("input", { name: "fullDays", type: "number", min: 1, max: 365, required: true, value: s.fullDays })),
        field(t("autobackup.incrementalDays"), h("input", { name: "incrementalDays", type: "number", min: 0, max: 364, required: true, value: s.incrementalDays })),
        field(t("autobackup.keep"), h("input", { name: "keep", type: "number", min: 1, max: data.maxVersions, required: true, value: s.keep })),
        h("p", { class: "muted", text: t("autobackup.keepHelp", { n: data.maxVersions }) }),
        h("div", { class: "actions-row" },
          h("button", { type: "submit", class: "primary", text: t("autobackup.save") }),
          h("button", { type: "button", class: "secondary", id: "autobackup-check-button", text: t("autobackup.check"), onclick: async () => {
            checkStatus.replaceChildren(h("p", { class: "muted", text: t("autobackup.checking") }));
            try {
              const res = await post("/api/backups/check", { destination: destination.value });
              checkStatus.replaceChildren(res.ok
                ? h("p", { class: "notice", text: t("autobackup.checkOk") })
                : h("p", { class: "error", text: t("autobackup.checkFailed", { error: res.error }) }));
            } catch (err) {
              checkStatus.replaceChildren(errorBox(err));
            }
          } })),
        checkStatus,
      ], async (d, status) => {
        await put("/api/backups/settings", {
          enabled: d.get("enabled") === "on", destination: d.get("destination").trim(),
          fullDays: Number(d.get("fullDays")), incrementalDays: Number(d.get("incrementalDays")), keep: Number(d.get("keep")),
        });
        await load();
        card.querySelector("#autobackup-settings .status")?.replaceChildren(h("p", { class: "notice", text: t("autobackup.saved") }));
      }),
      h("p", { id: "autobackup-next" }, st.running
        ? t("autobackup.running", { kind: kindText(st.running) })
        : st.next ? t("autobackup.next", { when: when(st.next), kind: kindText(st.nextKind) }) : t("autobackup.off")),
      h("div", { class: "actions-row" }, run("full"), run("incremental")),
      runStatus,
      h("h4", { text: t("autobackup.history") }),
      st.history.length ? filtered(h("table", { id: "autobackup-history" },
        h("thead", {}, h("tr", {}, [t("autobackup.when"), t("autobackup.kind"), t("autobackup.file"), t("autobackup.size"), t("autobackup.outcome")].map((x) => h("th", { text: x })))),
        h("tbody", {}, st.history.map((r) => h("tr", { "data-f-kind": kindText(r.kind), "data-f-outcome": r.error ? t("autobackup.failed") : t("autobackup.succeeded") },
          h("td", { text: when(r.at) }), h("td", { text: kindText(r.kind) }), h("td", {}, r.file ? h("code", { text: r.file }) : ""),
          h("td", { text: r.size ? sizeText(r.size) : "" }),
          h("td", { class: r.error ? "error" : "", text: r.error || t("autobackup.ok", { s: Math.max(1, Math.round(r.seconds)) }) }))))),
        { id: "backup-runs", label: t("autobackup.history"), choices: [{ key: "kind", label: t("autobackup.kind") }, { key: "outcome", label: t("autobackup.outcome") }] })
        : h("p", { class: "muted", text: t("autobackup.noHistory") }),
      h("h4", { text: t("autobackup.versions") }),
      st.storedError ? h("p", { class: "error", text: t("autobackup.listFailed", { error: st.storedError }) }) : "",
      st.versions.length ? filtered(h("table", { id: "autobackup-versions" },
        h("thead", {}, h("tr", {}, [t("autobackup.created"), t("autobackup.latest"), t("autobackup.parts"), ""].map((x) => h("th", { text: x })))),
        h("tbody", {}, st.versions.map((v) => h("tr", {},
          h("td", { text: when(v.created) }), h("td", { text: when(v.latest) }), h("td", { text: String(v.backups.length) }),
          h("td", { class: v.complete ? "muted" : "error", text: v.complete ? "" : t("autobackup.incomplete") }))))),
        { id: "backup-versions", label: t("autobackup.versions") })
        : h("p", { class: "muted", text: t("autobackup.noVersions") }),
      h("details", {},
        h("summary", { text: t("autobackup.restoreTitle") }),
        h("p", { class: "muted", text: t("autobackup.restoreHelp") }),
        h("pre", { class: "snippet", text: `${data.passphraseEnv}='…' wikiwikibangbang restore -from ${s.destination || "/path/to/backups"} -data-dir data-restored` })),
    );
    if (st.running) timer = setTimeout(load, 2000);
  };
  load();
  return card;
}

async function designCard() {
  const b = await get("/branding.json");
  const status = h("div", { class: "status" });
  // contrast warns about pairs of colours below WCAG AA (ADR-0052); the
  // design is saved all the same.
  const contrast = h("div", { id: "design-contrast", role: "status" });
  const pairText = (pair, ratio) => ({
    headerText: () => t("design.contrast.headerText", { ratio }),
    textOnBackground: () => t("design.contrast.textOnBackground", { ratio }),
    accentInk: () => t("design.contrast.accentInk", { ratio }),
    accentOnSurface: () => t("design.contrast.accentOnSurface", { ratio }),
    accentOnDarkSurface: () => t("design.contrast.accentOnDarkSurface", { ratio }),
    accentOnBackground: () => t("design.contrast.accentOnBackground", { ratio }),
  }[pair] || (() => pair))();
  // swatch shows a pair as it reads; colours set through the style
  // object, which the content security policy allows (unlike attributes).
  const swatch = (fg, bg) => {
    const el = h("span", { class: "design-pair", text: "Aa", "aria-hidden": "true" });
    el.style.color = fg;
    el.style.background = bg;
    return el;
  };
  const showContrast = (findings) => contrast.replaceChildren(findings.length ? h("div", { class: "notice warning" },
    h("strong", { text: t("design.contrastTitle") }),
    h("ul", {}, findings.map((f) => h("li", {},
      swatch(f.foreground, f.background),
      " ", pairText(f.pair, f.ratio.toFixed(1)), " ", h("span", { class: "muted", text: `${f.foreground} / ${f.background}` })))),
    h("p", { class: "muted", text: t("design.contrastHint") })) : "");
  showContrast(b.contrast || []);
  const colour = (name, label) => {
    const on = h("input", { type: "checkbox", name: name + "-on", checked: Boolean(b[name]), "aria-label": t("design.use", { what: label }) });
    const pick = h("input", { type: "color", name, value: b[name] || "#0b5cff", "aria-label": label });
    return h("div", { class: "field design-colour" }, h("span", { text: label }), h("label", { class: "check" }, on, pick, t("design.useIt")));
  };
  // An asset is uploaded or deleted on its own and only its block is
  // drawn again: the rest of the form keeps what was typed into it.
  const asset = (name, label, accept, has = b.assets?.[name]) => {
    const box = h("div", { class: "design-asset", "data-asset": name });
    // A failure shows beside the file it is about, not at the top of the
    // section, where it is easily missed.
    const own = h("div", { class: "status design-asset-status", role: "status" });
    const change = (fn, removing) => async () => {
      own.replaceChildren();
      try {
        await fn();
        await applyBranding(true);
        const now = await get("/branding.json");
        // The server answers for what it holds: a file still set after
        // removing it did not go, whatever the answer was on the way.
        if (removing && now.assets?.[name]) throw new Error(t("design.stillSet"));
        // "" and not undefined: a file gone must not fall back to the
        // design the form was drawn with.
        box.replaceWith(asset(name, label, accept, now.assets?.[name] || ""));
      } catch (err) {
        own.replaceChildren(errorBox(err));
      }
    };
    const picker = h("input", { type: "file", accept, id: "design-" + name, "aria-label": label });
    picker.addEventListener("change", change(async () => {
      const file = picker.files[0];
      if (!file) return;
      const res = await fetch("/api/branding/" + name, { method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: file });
      if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => ({}))).error || res.statusText);
    }));
    box.append(
      h("strong", { text: label }),
      has && name !== "font" ? h("img", { src: `/branding/${name}?v=${encodeURIComponent(has)}`, alt: "", class: name.endsWith("-dark") ? "design-preview design-preview-dark" : "design-preview" }) : "",
      h("span", { class: "muted", text: has ? t("design.fileSet") : t("design.fileNone") }),
      h("label", { class: "upload" }, h("span", { text: t("design.choose") }), picker),
      has ? h("button", { type: "button", class: "link danger", text: t("common.delete"), onclick: change(() => del("/api/branding/" + name), true) }) : "",
      own);
    return box;
  };
  // A ready-made theme (ADR-0085), shown with its header, page and accent
  // colours; the colours and font below apply over it.
  const themes = {
    "": { name: t("design.themeNone"), note: t("design.themeNoneNote"), colours: ["#ffffff", "#f5f6f8", "#0b5cff"] },
    ocean: { name: t("design.theme.ocean"), note: t("design.theme.oceanNote"), colours: ["#0a6c8f", "#eef6f9", "#16a3b8"] },
    forest: { name: t("design.theme.forest"), note: t("design.theme.forestNote"), colours: ["#24452b", "#f3f5ef", "#b9d98a"] },
    sunset: { name: t("design.theme.sunset"), note: t("design.theme.sunsetNote"), colours: ["#f97316", "#fff7f0", "#e11d48"] },
    berry: { name: t("design.theme.berry"), note: t("design.theme.berryNote"), colours: ["#ffffff", "#f7f3fb", "#7e22ce"] },
    // The look of the Confederation's sites (ADR-0120) says the site is an
    // authority's; the choice says so too.
    swiss: { name: t("design.theme.swiss"), note: t("design.theme.swissNote"), notice: t("design.theme.swissNotice"), colours: ["#ffffff", "#2f4356", "#d8232a"] },
    night: { name: t("design.theme.night"), note: t("design.theme.nightNote"), colours: ["#070a10", "#121722", "#22d3ee"] },
    comic: { name: t("design.theme.comic"), note: t("design.theme.comicNote"), colours: ["#ffd23f", "#fff6d6", "#e3262f"] },
    sketch: { name: t("design.theme.sketch"), note: t("design.theme.sketchNote"), colours: ["#fffefa", "#2b2b2b", "#2856b6"] },
  };
  const themePicker = h("fieldset", { class: "design-themes", id: "design-themes" }, h("legend", { text: t("design.theme") }),
    ["", ...(b.themes || [])].filter((id) => themes[id]).map((id) => {
      const preview = h("span", { class: "theme-preview", "aria-hidden": "true" }, ...themes[id].colours.map((c) => {
        const dot = h("span");
        dot.style.background = c;
        return dot;
      }));
      return h("label", { class: "theme-choice theme-" + (id || "none") },
        h("input", { type: "radio", name: "theme", value: id, checked: (b.theme || "") === id }),
        preview, h("strong", { text: themes[id].name }), h("span", { class: "muted", text: themes[id].note }),
        ...(themes[id].notice ? [h("span", { class: "theme-notice", text: themes[id].notice })] : []));
    }));
  const fontNames = { system: t("design.fontSystem"), humanist: t("design.fontHumanist"), serif: t("design.fontSerif"), rounded: t("design.fontRounded"), mono: t("design.fontMono") };
  const cssPicker = h("input", { type: "file", accept: ".css,text/css", "aria-label": t("design.cssFile") });
  const cssText = h("textarea", { name: "css", rows: 8, class: "code", spellcheck: "false", "aria-label": t("design.css"), value: b.css || "" });
  cssPicker.addEventListener("change", async () => { if (cssPicker.files[0]) cssText.value = await cssPicker.files[0].text(); });
  return h("div", {},
    h("p", { class: "muted", text: t("design.help") }),
    h("div", { class: "design-assets" },
      asset("logo", t("design.logo"), "image/png,image/jpeg,image/gif,image/webp,image/svg+xml"),
      asset("logo-dark", t("design.logoDark"), "image/png,image/jpeg,image/gif,image/webp,image/svg+xml"),
      asset("background", t("design.background"), "image/png,image/jpeg,image/gif,image/webp"),
      asset("welcome", t("design.welcome"), "image/png,image/jpeg,image/gif,image/webp,image/svg+xml"),
      asset("welcome-dark", t("design.welcomeDark"), "image/png,image/jpeg,image/gif,image/webp,image/svg+xml"),
      asset("font", t("design.fontFile"), ".woff2,.woff,.ttf,.otf")),
    form({ id: "design" }, [
      themePicker,
      field(t("design.name"), h("input", { name: "name", value: b.name || "", maxlength: 80 })),
      h("div", { class: "design-colours" },
        colour("accent", t("design.accent")), colour("background", t("design.backgroundColour")),
        colour("headerBackground", t("design.headerBackground")), colour("headerText", t("design.headerText"))),
      contrast,
      field(t("design.font"), h("select", { name: "font" }, h("option", { value: "", text: t("design.fontDefault") }),
        Object.entries(fontNames).map(([v, label]) => h("option", { value: v, text: label, selected: b.font === v })),
        h("option", { value: "upload", text: t("design.fontUpload"), selected: b.font === "upload" }))),
      field(t("design.fontFamily"), h("input", { name: "fontFamily", value: b.fontFamily || "", placeholder: "Frutiger" })),
      field(t("design.css"), cssText),
      h("p", { class: "muted", id: "design-dark", text: b.dark === false ? t("design.darkOff") : t("design.darkHint") }),
      h("label", { class: "upload" }, h("span", { text: t("design.cssFile") }), cssPicker),
      h("button", { type: "submit", class: "primary", text: t("common.save") }),
    ], async (d, st) => {
      const pick = (name) => (d.get(name + "-on") ? d.get(name) : "");
      const saved = await put("/api/branding", { name: d.get("name"), accent: pick("accent"), background: pick("background"),
        headerBackground: pick("headerBackground"), headerText: pick("headerText"), font: d.get("font"), fontFamily: d.get("fontFamily").trim(), theme: d.get("theme") || "", css: cssText.value });
      showContrast(saved.contrast || []);
      document.getElementById("design-dark").textContent = saved.dark === false ? t("design.darkOff") : t("design.darkHint");
      await applyBranding(true);
      st.replaceChildren(h("p", { class: "notice", text: t("design.saved") }));
    }),
    status);
}

// membersView lists who holds which role in a space, for its
// administrators (ADR-0048): change a role, take it back, grant one to a
// person or a group.
async function membersView(key) {
  const show = screen();
  const [{ roles }, { users }, { spaces }] = await Promise.all([
    get(`/api/spaces/${encodeURIComponent(key)}/roles`), get("/api/users"), get("/api/spaces"),
  ]);
  for (const u of users) names.set(u.id, u.username);
  await learnNames(roles.map((r) => r.principal).filter((p) => !p.startsWith("@")));
  here.space = key;
  const sp = spaces.find((s) => s.key === key) || { name: key };
  const status = h("div", { class: "status" });
  const base = `/api/spaces/${encodeURIComponent(key)}/roles/`;
  const roleSelect = (value) => h("select", { name: "role" }, [["viewer", t("role.viewer")], ["editor", t("role.editor")], ["space-admin", t("role.space-admin")]]
    .map(([r, label]) => h("option", { value: r, text: label, selected: r === value })));
  const row = (g) => {
    const sel = roleSelect(g.role);
    sel.setAttribute("aria-label", t("admin.role"));
    sel.addEventListener("change", () => action(() => put(base + encodeURIComponent(g.principal), { role: sel.value }), status));
    return h("tr", { "data-f-role": g.role },
      h("td", { text: g.principal.startsWith("@") ? g.principal : person(g.principal) }),
      h("td", {}, sel),
      h("td", {}, h("button", { type: "button", class: "link danger", text: t("members.revoke"),
        onclick: () => confirm(t("members.revokeConfirm", { name: g.principal.startsWith("@") ? g.principal : person(g.principal) }))
          && action(() => del(base + encodeURIComponent(g.principal)), status) })));
  };
  const known = new Set(roles.map((r) => r.principal));
  show(
    h("p", {}, link("#/", t("nav.spaces")), " / ", link("#/space/" + encodeURIComponent(key), key), " / " + t("members.title")),
    h("h1", { text: t("members.heading", { space: sp.name }) }),
    h("p", { class: "muted", text: t("members.help") }),
    status,
    sp.role === "space-admin" ? spaceNameCard(sp, key) : null,
    sp.role === "space-admin" ? spaceIconCard(sp, status) : null,
    h("h2", { text: t("members.roles") }),
    roles.length ? filtered(h("table", { id: "members" }, roles.map(row)), { id: "members", label: t("members.roles"),
      choices: [{ key: "role", label: t("admin.role"), name: roleText }] }) : empty(t("members.none")),
    form({ class: "card", id: "add-role" }, [
      h("h2", { text: t("members.add") }),
      field(t("admin.user"), h("select", { name: "principal" }, users.filter((u) => !known.has(u.id)).map((u) => h("option", { value: u.id, text: u.username })))),
      field(t("members.orGroup"), h("input", { name: "group", placeholder: "@ops", pattern: "@?[A-Za-z0-9][A-Za-z0-9._\\-]*" })),
      field(t("admin.role"), roleSelect("viewer")),
      h("button", { type: "submit", text: t("admin.grantButton") }),
    ], async (d) => {
      const group = String(d.get("group") || "").trim();
      const principal = group ? "@" + group.replace(/^@/, "") : d.get("principal");
      if (!principal) throw new Error(t("members.nobody"));
      await put(base + encodeURIComponent(principal), { role: d.get("role") });
      route();
    }),
  );
}
