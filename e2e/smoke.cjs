// Browser smoke test of the UI (ADR-0031). It drives a running server in
// Chromium through the main flows and fails on any script error, any
// content-security-policy violation and any dialog — a dialog would mean
// user content ran as code (ADR-0014).
//
//   BASE_URL=http://127.0.0.1:8080 ADMIN_PASSWORD=... node smoke.cjs
const { chromium } = require("playwright");

const base = process.env.BASE_URL || "http://127.0.0.1:8080";
const adminPassword = process.env.ADMIN_PASSWORD;
const hostile = `<img src=x onerror="alert('xss')">`;

function check(cond, msg) {
  if (!cond) throw new Error("check failed: " + msg);
}

// mailSink is a mail server that keeps every message, for the server under
// test to send to (WWBB_SMTP_HOST=127.0.0.1, WWBB_SMTP_PORT=SMTP_SINK_PORT,
// WWBB_SMTP_SECURITY=none). Without SMTP_SINK_PORT there is none.
function mailSink(port) {
  const mails = [];
  if (!port) return { mails, close() {} };
  const server = require("net").createServer((sock) => {
    let buf = "", data = false, msg = "";
    const say = (line) => sock.write(line + "\r\n");
    say("220 sink");
    sock.on("data", (chunk) => {
      buf += chunk.toString("utf8");
      let i;
      while ((i = buf.indexOf("\r\n")) >= 0) {
        const line = buf.slice(0, i);
        buf = buf.slice(i + 2);
        if (data) {
          if (line === ".") { data = false; mails.push(msg); msg = ""; say("250 kept"); } else msg += line + "\r\n";
          continue;
        }
        const cmd = line.toUpperCase();
        if (cmd === "DATA") { data = true; say("354 go on"); } else if (cmd === "QUIT") { say("221 bye"); sock.end(); } else say("250 ok");
      }
    });
  });
  server.listen(Number(port), "127.0.0.1");
  return { mails, close: () => server.close() };
}

// mailTo waits for a mail to an address and returns its text, decoded.
async function mailTo(sink, address) {
  for (let i = 0; i < 100; i++) {
    const m = sink.mails.find((x) => x.includes(`<${address}>`));
    if (m) {
      const body = m.split("\r\n\r\n").slice(1).join("\r\n\r\n");
      return body.replace(/=\r\n/g, "").replace(/=([0-9A-F]{2})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("no mail to " + address);
}

// ckanSource answers package_search like the CKAN API of opendata.swiss,
// with one data set that publishes an API and a CSV file (ADR-0075).
function ckanSource() {
  const server = require("http").createServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ success: true, result: { count: 1, facets: { res_format: { api: 1, CSV: 1 } }, results: [{
      id: "d1", name: "luftqualitaet", title: { de: "Luftqualität", fr: "Qualité de l'air" },
      organization: { name: "bafu", title: { de: "Bundesamt für Umwelt" } },
      contact_points: [{ name: "Luft", email: "luft@example.org" }],
      resources: [{ id: "r-air", format: "api", url: "https://air.example.org/v1", documentation: ["https://air.example.org/docs"] },
        { id: "r-csv", format: "CSV", url: "https://air.example.org/data.csv" }],
    }] } }));
  });
  server.listen(0, "127.0.0.1");
  return server;
}

// feedSource answers a list in the connectors' own feed format, with one
// entry and two further values (ADR-0127).
function feedSource() {
  const server = require("http").createServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ entries: [{ id: "svc-1", title: "Adressdienst", url: "https://registry.example.org/svc-1",
      text: "Liefert die Adressen des Kantons.", owner: "Amt für Geoinformation", since: "2024-03-01" }], next: "" }));
  });
  server.listen(0, "127.0.0.1");
  return server;
}

(async () => {
  const sink = mailSink(process.env.SMTP_SINK_PORT);
  const ckan = ckanSource();
  const feed = feedSource();
  const browser = await chromium.launch(process.env.CHROMIUM ? { executablePath: process.env.CHROMIUM } : {});
  const page = await browser.newPage();
  const problems = [];
  page.on("pageerror", (e) => problems.push("page error: " + e.message));
  // A step that provokes an error on purpose names it here while it runs.
  let expectedConsole = null;
  page.on("console", (m) => { if (m.type() === "error" && !/status of 401/.test(m.text()) && !(expectedConsole && expectedConsole.test(m.text()))) problems.push("console: " + m.text()); });
  // A step that expects a confirmation or prompt sets its answer first:
  // true accepts, a string is typed in; any other dialog is a problem.
  let answer = null;
  page.on("dialog", async (d) => {
    if (answer !== null) {
      const a = answer;
      answer = null;
      await (a === true ? d.accept() : d.accept(a));
      return;
    }
    problems.push("dialog: " + d.message());
    await d.dismiss();
  });
  const step = (name) => console.log("·", name);

  try {
    step("login form: help for a forgotten password and an account is always offered");
    await page.goto(base + "/");
    await page.waitForSelector("#forgot-open");
    await page.click("#signup-open"); // requests are off until an administrator allows them
    await page.waitForSelector("#signup-closed");
    await page.click("#login-card button:has-text('Back to sign-in')");

    step("login form, wrong password");
    await page.fill("#login [name=username]", "admin");
    await page.fill("#login [name=password]", "wrong password!");
    await page.click("#login button[type=submit]");
    await page.waitForSelector("#login .error");

    step("log in");
    await page.fill("#login [name=password]", adminPassword);
    await page.click("#login button[type=submit]");
    await page.waitForSelector("text=No rooms yet");

    step("start page: a greeting, quick access and the spaces as tiles");
    await page.waitForSelector("#welcome h1:has-text('admin')");
    await page.waitForSelector("#welcome img.welcome-art");
    await page.waitForSelector("#welcome #home-version:has-text('Version')");
    check(await page.locator(".home-tiles a").count() === 5, "five quick accesses, one's notes among them");

    step("the footer names the running version");
    await page.waitForSelector("footer.footer:has-text('wikiwikibangbang')");

    step("header: the language and the light or dark look switch from every page (ADR-0095)");
    await page.click("#lang-toggle [data-lang=de]");
    await page.waitForFunction(() => document.documentElement.lang === "de");
    check((await page.evaluate(async () => (await (await fetch("/api/me")).json()).language)) === "de", "the language switched in the header is saved with the account");
    await page.click("#lang-toggle [data-lang=en]");
    await page.waitForFunction(() => document.documentElement.lang === "en" && document.querySelector("#lang-toggle [data-lang=en]").getAttribute("aria-pressed") === "true");
    const look = await page.evaluate(() => document.documentElement.dataset.theme);
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme !== was, look);
    await page.waitForFunction((dark) => (getComputedStyle(document.documentElement).getPropertyValue("--bg").trim() === "#0e1014") === dark, look === "light");
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme === was, look);

    step("administration: space, type, user, role");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.fill("#new-space [name=key]", "PLAT");
    await page.fill("#new-space [name=name]", "Platform");
    await page.click("#new-space button[type=submit]");
    await page.waitForSelector("#grant option[value=PLAT]", { state: "attached" });
    await page.fill("#new-type [name=type]", JSON.stringify({
      name: "task", body: true,
      fields: [{ name: "priority", kind: "enum", required: true, values: ["low", "high"] }, { name: "estimate", kind: "number" }],
      workflow: { initial: "open", states: ["open", "done"], transitions: [{ name: "finish", from: ["open"], to: "done" }] },
    }));
    await page.click("#new-type button[type=submit]");
    await page.waitForSelector("text=task v1");
    // A type nothing uses is removed (ADR-0094).
    await page.fill("#new-type [name=type]", JSON.stringify({ name: "scratch", body: true }));
    await page.click("#new-type button[type=submit]");
    await page.waitForSelector("text=scratch v1");
    answer = true;
    await page.click("#types tr:has-text('scratch v1') button:has-text('Remove')");
    await page.waitForSelector("text=scratch v1", { state: "detached" });
    check(!(await page.evaluate(async () => (await (await fetch("/api/types")).json()).types.some((ty) => ty.name === "scratch"))),
      "a removed type is no longer listed");
    await page.fill("#new-user [name=username]", "bert");
    await page.fill("#new-user [name=password]", "bert's password");
    await page.click("#new-user button[type=submit]");
    await page.waitForSelector("#grant option:has-text('bert')", { state: "attached" });
    await page.selectOption("#grant [name=user]", { label: "bert" });
    await page.selectOption("#grant [name=role]", "editor");
    await page.click("#grant button[type=submit]");
    await page.waitForSelector("#grant >> text=Granted.");
    await page.waitForSelector("td:has-text('role.grant')", { state: "attached" }).catch(() => {});

    step("administration: an encrypted backup downloads");
    await page.fill("#backup [name=passphrase]", "a long backup passphrase");
    await page.fill("#backup [name=repeat]", "a long backup passphrase");
    const [download] = await Promise.all([page.waitForEvent("download"), page.click("#backup button[type=submit]")]);
    check(/^wikiwikibangbang-.*\.wwbb$/.test(download.suggestedFilename()), "the backup is named " + download.suggestedFilename());
    const saved = require("fs").readFileSync(await download.path());
    check(saved.subarray(0, 8).toString() === "WWBBBAK1", "the download is an encrypted backup");
    if (process.env.BACKUP_OUT) require("fs").writeFileSync(process.env.BACKUP_OUT, saved);

    if (process.env.AUTOBACKUP_DIR) {
      step("administration: an automatic backup goes to its destination (ADR-0073)");
      await page.waitForSelector("#autobackup-settings");
      await page.fill("#autobackup-destination", process.env.AUTOBACKUP_DIR);
      await page.fill("#autobackup-settings [name=keep]", "2");
      await page.click("#autobackup-settings button[type=submit]");
      await page.waitForSelector("#autobackup-settings >> text=Saved.");
      await page.click("#autobackup-check-button");
      await page.waitForSelector("#autobackup-check >> text=The destination works");
      await page.click("#autobackup-run-full");
      await page.waitForSelector("#autobackup-history td:has-text('succeeded')");
      await page.waitForSelector("#autobackup-versions tbody tr");
      const stored = require("fs").readdirSync(process.env.AUTOBACKUP_DIR);
      check(stored.length === 1 && /-000-full\.wwbb$/.test(stored[0]), "the destination holds one full backup: " + stored.join(", "));
    }

    step("create an item with a hostile title");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.fill("#new-item [name=title]", hostile);
    await page.selectOption("#new-item [name='p:priority']", "high");
    await page.fill("#new-item [name='p:estimate']", "3");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector("#title");
    check(await page.inputValue("#title [name=title]") === hostile, "the title shows as typed");
    check(page.url().endsWith("#/item/PLAT-1?edit"), "the item is addressed by key, and a new one opens for writing");
    await page.waitForSelector("#watch[aria-pressed='true'][aria-label='Stop watching']"); // its creator watches it

    step("edit property, transition");
    await page.fill("[name='p:estimate']", "2.5");
    await page.press("[name='p:estimate']", "Tab"); // a field is saved as it changes
    await page.waitForSelector("tr[data-field=estimate] .saved");
    await page.reload();
    await page.waitForSelector("text=version 2");
    // The status leads the details: the states in a row, the current one
    // marked, the next ones buttons that perform their transition.
    await page.waitForSelector("#item-status .state-step.current:text-is('open')");
    await page.click("#item-status button[data-transition=finish]");
    await page.waitForSelector("text=state: done");
    await page.waitForSelector("#item-status .state-step.current.finished:text-is('done')");

    step("comments: add one with markup, which shows as text, then edit it");
    await page.fill("#add-comment [name=text]", hostile);
    await page.click("#add-comment button[type=submit]");
    await page.waitForSelector("#comments h2:has-text('Comments (1)')");
    check((await page.textContent(".comment-text")) === hostile, "the comment shows as typed");
    await page.click(".comment button:has-text('Edit')");
    await page.fill(".comment [name=text]", "Patched on the staging hosts.");
    await page.click(".comment button:has-text('Save comment')");
    await page.waitForSelector(".comment-text:has-text('Patched on the staging hosts.')");
    check((await page.textContent(".comment .muted")).includes("you") && (await page.textContent(".comment .muted")).includes("edited"), "own, edited comment");

    step("comment overview: one's own comments, across pages, newest first");
    await page.evaluate(async () => fetch("/api/items/PLAT-1/comments", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: "@bert please verify on production" }) }));
    await page.click("#app-switcher");
    await page.click("#drawer-apps a:has-text('Comments')");
    await page.waitForSelector("#comment-feed li:first-child:has-text('@bert please verify')");
    await page.click("#comments-tab-mine");
    await page.waitForSelector("#comment-feed li:has-text('Patched on the staging hosts.') .why-mine");
    await page.click("#comments-tab-mentions");
    await page.waitForSelector("#comment-feed-empty");
    await page.goto(base + "/#/item/PLAT-1");

    step("rich text: a second item with formatting, a list, a mention and an embed");
    await page.click("a:has-text('PLAT')");
    await page.fill("#new-item [name=title]", "Runbook");
    await page.selectOption("#new-item [name='p:priority']", "low");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector(".ProseMirror");
    const editor = page.locator(".ProseMirror");
    await editor.click();
    await page.keyboard.type("Steps");
    await page.selectOption(".pm-toolbar select", "heading:2");
    await page.keyboard.press("End");
    await page.keyboard.press("Enter");
    await page.keyboard.type("- first item");
    await page.keyboard.press("Enter");
    await page.keyboard.type("second item");
    await page.keyboard.press("Enter");
    await page.keyboard.press("Enter"); // an empty item ends the list
    await page.keyboard.press("Control+b");
    await page.keyboard.type("Bold");
    await page.keyboard.press("Control+b");
    await page.keyboard.type(" <script>alert('body')</script> see ");
    // Links, "@" and embedding work as in a word processor (ADR-0096).
    await page.keyboard.type("https://example.com/docs ");
    await page.waitForSelector(".ProseMirror a[href^='https://example.com/docs']");
    await page.keyboard.press("Control+k");
    await page.fill("dialog.pm-dialog [name=text]", "the guide");
    await page.fill("dialog.pm-dialog [name=address]", "www.example.org");
    await page.click("dialog.pm-dialog button[type=submit]");
    await page.waitForSelector(".ProseMirror a[href^='https://www.example.org']:has-text('the guide')");
    await page.keyboard.press("ArrowLeft");
    await page.waitForSelector("#pm-link-bubble:not([hidden]) >> text=https://www.example.org");
    await page.keyboard.press("End");
    await page.keyboard.type(" @PLAT-1");
    await page.waitForSelector("#pm-at:not([hidden]) .pm-at-option:has-text('PLAT-1')");
    await page.keyboard.press("Enter");
    await page.waitForSelector(".ProseMirror .pm-mention");
    await page.selectOption(".pm-toolbar select[aria-label='Insert…']", "embed");
    await page.fill("dialog.pm-dialog [name=q]", "PLAT-1");
    await page.click("dialog.pm-dialog .pm-result[data-key='PLAT-1']");
    await page.waitForSelector(".ProseMirror .pm-embed");
    // A video plays in the text (ADR-0097).
    await page.setInputFiles(".pm-toolbar input[type=file][accept^='video']", { name: "clip.mp4", mimeType: "video/mp4",
      buffer: Buffer.concat([Buffer.from("00000018667479706d70343200000000", "hex"), Buffer.from("mp42isom"), Buffer.alloc(4000, 9)]) });
    await page.waitForSelector(".ProseMirror figure.pm-video video[controls][src*='/attachments/']");
    await page.click("#details h2"); // leaving the editor releases its blocks, which saves them
    await page.waitForSelector(".live[data-pending='0']");

    step("the body survives a reload exactly");
    await page.reload();
    await page.waitForSelector(".ProseMirror h3");
    check((await page.textContent(".ProseMirror h3")) === "Steps", "heading level 2 renders as h3 under the title");
    check(await page.locator(".pm-bullet").count() === 2, "two bulleted items");
    check((await page.textContent(".ProseMirror strong")) === "Bold", "bold text");
    check((await page.textContent(".pm-mention")).startsWith("@") && (await page.getAttribute(".pm-mention", "href")).startsWith("#/item/"), "the mention");
    check(await page.locator(".ProseMirror a[href^='https://example.com/docs']").count() === 1, "the typed address stays a link");
    check((await page.textContent(".pm-embed .pm-embed-title")).includes("PLAT-1"), "the embed");
    await page.waitForSelector(".pm-embed .pm-embed-content[data-embedded='PLAT-1'] .pm-embed-meta");
    check((await page.textContent(".pm-embed .pm-embed-meta")).includes("task"), "the embed shows the item whole, not a link only (ADR-0106)");
    check((await editor.textContent()).includes("<script>alert('body')</script>"), "the script shows as text");
    check(await page.locator(".ProseMirror figure.pm-video video[controls]").count() === 1, "the video stays in the text");

    step("live: two people edit one body at once");
    const other = await browser.newPage();
    other.on("pageerror", (e) => problems.push("bert's page error: " + e.message));
    other.on("dialog", async (d) => { problems.push("bert's dialog: " + d.message()); await d.dismiss(); });
    await other.goto(base + "/");
    await other.fill("#login [name=username]", "bert");
    await other.fill("#login [name=password]", "bert's password");
    await other.click("#login button[type=submit]");
    await other.waitForSelector("#account-button[aria-label='Account of bert']");
    await other.goto(base + "/#/item/PLAT-2?edit");
    await other.waitForSelector(".ProseMirror h3");
    // Who edits shows as faces beside the bell (ADR-0115).
    await page.waitForSelector("#live-editors .live-editor[title='bert is editing'] .avatar");
    // admin types in the heading; bert sees it live, marked as held.
    await page.click(".ProseMirror h3");
    await page.keyboard.press("End");
    await page.keyboard.type(" now");
    await other.waitForSelector(".pm-locked[data-holder='admin'] >> text=Steps now");
    // bert cannot type into the held heading…
    await other.click(".ProseMirror h3");
    await other.keyboard.press("End");
    await other.keyboard.type("XYZ");
    check((await other.textContent(".ProseMirror h3")) === "Steps now", "a held block refuses others' typing");
    // …but can edit another block at the same time.
    await other.click(".ProseMirror .pm-bullet >> nth=0");
    await other.keyboard.press("End");
    await other.keyboard.type(" (bert)");
    await page.waitForSelector(".pm-locked[data-holder='bert'] >> text=first item (bert)");
    await page.click("#details h2");
    await other.click("#details h2");
    await page.waitForSelector(".live[data-pending='0']");
    await other.waitForSelector(".live[data-pending='0']");
    // A reader sees who edits, and what they save, without a reload; the
    // reader is no editor (ADR-0115).
    await other.goto(base + "/#/item/PLAT-2");
    await other.waitForSelector("#body.reading .ProseMirror h3");
    await other.waitForSelector("#live-editors .live-editor[title='admin is editing']");
    check(await page.locator("#live-editors .live-editor").count() === 0, "a reader is not shown as editing");
    await page.click(".ProseMirror .pm-bullet >> nth=1");
    await page.keyboard.press("End");
    await page.keyboard.type(" (live)");
    await page.click("#details h2");
    await page.waitForSelector(".live[data-pending='0']");
    await other.waitForSelector("#body.reading .pm-bullet >> text=second item (live)");
    await other.close();
    await page.reload();
    await page.waitForSelector(".ProseMirror h3");
    check((await page.textContent(".ProseMirror h3")) === "Steps now", "admin's change is saved");
    const bullet = await page.textContent(".pm-bullet >> nth=0");
    check(bullet === "first item (bert)", "bert's change is saved: " + bullet);
    const authors = await page.$$eval("table.history tr", (rows) => rows
      .filter((r) => r.children[2]?.textContent === "BodyCommitted").map((r) => r.children[1].textContent));
    check(new Set(authors).size >= 2, "each person's change is a version in their name");
    check(authors.includes("bert") && authors.includes("you"), "the history names people, not IDs: " + authors.join(", "));

    step("the mention relates the items");
    await page.click(".pm-mention");
    // The runbook itself lists "mentions (from the body)", so wait for the
    // mentioned item's own page first.
    await page.waitForSelector("text=state: done");
    await page.waitForSelector("text=mentions (from the body)");

    step("search: the body is found, its markup shows as text");
    const beforeSearch = await page.evaluate(() => location.hash);
    await page.fill("nav input[name=q]", "alert body");
    await page.press("nav input[name=q]", "Enter");
    // This wait has failed now and then on CI only; say what the page
    // showed instead, to find out why.
    await page.waitForSelector(".hits li").catch(async (err) => {
      console.log("search diagnostics: " + JSON.stringify(await page.evaluate(() => ({ hash: location.hash,
        h1: document.querySelector("main h1")?.textContent, total: document.querySelector("#search-total")?.textContent,
        typed: document.querySelector("nav input[name=q]")?.value, none: document.querySelector("main .empty:not([hidden])")?.textContent }))));
      throw err;
    });
    check((await page.textContent(".hits li a")) === "PLAT-2", "the runbook is the hit");
    check((await page.textContent(".hits .snippet")).includes("<script>alert('body')</script>"), "the snippet shows the script as text");
    await page.fill("nav input[name=q]", "staging hosts");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector(".hits .snippet:has-text('In a comment: Patched on the staging hosts.')");
    await page.fill("nav input[name=q]", "zzzz-nothing");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector("text=Nothing found");

    step("clearing the search field returns to the page before the search; home, back and forward beside the nine dots");
    await page.fill("nav input[name=q]", ""); // deleting the words keeps the results
    await page.waitForTimeout(200);
    check(await page.evaluate(() => location.hash.startsWith("#/search/")), "deleting the words by hand stays on the results");
    await page.fill("nav input[name=q]", "zzzz-nothing");
    await page.press("nav input[name=q]", "Escape"); // as the field's ×
    await page.waitForFunction((h) => location.hash === h, beforeSearch);
    await page.click("#nav-back");
    await page.waitForFunction(() => location.hash.startsWith("#/search/"));
    await page.click("#nav-forward");
    await page.waitForFunction((h) => location.hash === h, beforeSearch);
    await page.click("#history-nav #nav-home");
    await page.waitForFunction(() => location.hash === "#/");
    await page.goBack();
    await page.waitForFunction((h) => location.hash === h, beforeSearch);

    step("a slow view does not replace the one chosen after it");
    await page.route("**/api/items/PLAT-1", async (r) => { await new Promise((ok) => setTimeout(ok, 1500)); await r.continue(); });
    await page.evaluate(() => { location.hash = "#/item/PLAT-1"; });
    await page.fill("nav input[name=q]", "alert body");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector(".hits li");
    await page.waitForTimeout(2500); // the item view has answered by now
    check(await page.locator(".hits li").count() > 0 && (await page.textContent(".hits li a")) === "PLAT-2", "the search results stay");
    await page.unroute("**/api/items/PLAT-1");

    step("app switcher: nine dots open the modules; Tasks lists open work across spaces");
    await page.click("#app-switcher");
    await page.waitForSelector("#drawer:not([hidden])");
    const modules = (await page.locator("#drawer-apps a").allTextContents()).join(",");
    check(modules === "Rooms,Create,Tasks,Comments,Album,AI access,Governance,Account,Administration", "the modules of an administrator: " + modules);
    await page.keyboard.press("Escape");
    await page.waitForSelector("#drawer", { state: "hidden" });
    await page.click("#app-switcher");
    await page.click("#drawer-apps a:has-text('Tasks')");
    await page.waitForSelector("#drawer", { state: "hidden" });
    await page.waitForSelector("#tasks td:has-text('PLAT-2')");
    check(await page.locator("#tasks td:has-text('PLAT-1')").count() === 0, "a finished task is not open work");
    check(await page.locator("#drawer-apps a.active").count() === 1, "one module is marked active");
    await page.click("#app-switcher");
    await page.click("#drawer-apps a:has-text('Governance')");
    await page.waitForSelector("#governance-overview td:has-text('PLAT')");
    // An "i" beside each column says what it checks; Escape closes it.
    await page.click("#governance-overview th:has-text('Sensitive data') .info-btn");
    await page.waitForSelector("#governance-overview .info-pop:visible li:has-text('IBAN')");
    await page.keyboard.press("Escape");
    await page.waitForSelector("#governance-overview .info-pop", { state: "hidden" });

    step("task inbox: an overdue unassigned task is taken over and lands under Assigned to me");
    await page.evaluate(async () => {
      const send = (method, path, body) => fetch(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
      await send("POST", "/api/types", { name: "ticket", body: true,
        fields: [{ name: "assignee", kind: "user" }, { name: "due", kind: "date" }, { name: "priority", kind: "enum", values: ["low", "high"] }],
        workflow: { initial: "open", states: ["open", "closed"], transitions: [{ name: "close", from: ["open"], to: "closed" }] } });
      await send("POST", "/api/items", { space: "PLAT", type: "ticket", title: "Renew the certificate", properties: { due: "2020-01-31", priority: "high" } });
      await send("POST", "/api/items", { space: "PLAT", type: "ticket", title: "Tidy the wiki", properties: { priority: "low" } });
    });
    await page.goto(base + "/#/tasks/unassigned");
    await page.waitForSelector("#tasks tr.overdue:has-text('Renew the certificate') .badge:has-text('overdue since 2020-01-31')");
    // Every column filters; the filters stay in the address and across a reload.
    await page.selectOption("#tasks select[aria-label='Filter Due']", "overdue");
    await page.waitForSelector("#tasks tbody tr:has-text('Tidy the wiki')", { state: "hidden" });
    check(page.url().includes("due=overdue"), "the filter is in the address: " + page.url());
    await page.reload();
    await page.waitForSelector("#tasks tbody tr:has-text('Renew the certificate')");
    check(await page.locator("#tasks tbody tr:visible").count() === 1 && await page.inputValue("#tasks select[aria-label='Filter Due']") === "overdue", "the filter survives a reload");
    await page.selectOption("#tasks select[aria-label='Filter Priority']", "low");
    await page.waitForSelector("#tasks p:has-text('No task matches the filters.')");
    await page.click("#tasks button:has-text('Reset filters')");
    await page.waitForSelector("#tasks tbody tr:has-text('Tidy the wiki')");
    await page.fill("#tasks input[aria-label='Filter Title']", "tidy");
    await page.waitForSelector("#tasks tbody tr:has-text('Renew the certificate')", { state: "hidden" });
    await page.fill("#tasks input[aria-label='Filter Title']", "");
    await page.click("#tasks tr:has-text('Renew the certificate') button:has-text('Take over')");
    await page.goto(base + "/#/tasks/mine");
    await page.waitForSelector("#tasks tr:has-text('Renew the certificate') td:has-text('you')");
    await page.click("#tasks tr:has-text('Renew the certificate') button:has-text('close')");
    await page.waitForSelector("#tasks tr:has-text('Renew the certificate')", { state: "detached" });

    step("new task: the form opens with the task type; the assignee is found by a few letters");
    await page.click("#tasks-new");
    await page.waitForSelector("#new-item [name=title]:focus");
    check(await page.inputValue("#new-item [name=type]") === "task", "New task preselects the task type");
    await page.selectOption("#new-item [name=type]", "ticket");
    await page.fill("#new-item [name=title]", "Order new badges");
    await page.fill("#new-item [name='p:assignee']", "nobody");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector("#new-item .status:has-text('choose a person')");
    await page.fill("#new-item [name='p:assignee']", "ber");
    await page.waitForSelector("#user-options option[value=bert]", { state: "attached" });
    await page.fill("#new-item [name='p:assignee']", "bert");
    await page.waitForFunction(() => document.querySelector("#new-item [name='p:assignee']").dataset.userId !== "");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector("#title");
    check(await page.inputValue("#details [name='p:assignee']") === "bert", "the assignee shows by name");
    await page.click("#edit-done");

    step("board: columns are the workflow's states; dragging a card performs the transition");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.click("a:has-text('Board: task')");
    await page.waitForSelector(".board");
    const column = (state) => `.board-column[data-state='${state}']`;
    check(await page.locator(".board-column").count() === 2, "one column per state");
    check(await page.locator(`${column("done")} .card-item:has-text('PLAT-1')`).count() === 1, "the finished task is done");
    check(await page.locator(`${column("open")} .card-item:has-text('PLAT-2')`).count() === 1, "the runbook is open");
    check((await page.textContent(`${column("done")} .card-item:has-text('PLAT-1')`)).includes(hostile), "a card's title shows as text");
    await page.dragAndDrop(`${column("open")} .card-item:has-text('PLAT-2')`, column("done"));
    await page.waitForSelector(`${column("done")} .card-item:has-text('PLAT-2')`);
    check(await page.locator(`${column("open")} .card-item`).count() === 0, "the open column is empty");

    step("page tree: child pages, reordered by button and by dragging");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.waitForSelector("#home h2:has-text('PLAT-1'), #home h2 a");
    await page.click(".tree a:has-text('Runbook')");
    await page.waitForSelector(".tree a.current:has-text('Runbook')");
    for (const title of ["Install", "Upgrade"]) {
      await page.click("#item-new-child");
      await page.waitForSelector("#new-item [name=under]:checked");
      await page.fill("#new-item [name=title]", title);
      await page.selectOption("#new-item [name='p:priority']", "low");
      await page.click("#new-item button[type=submit]");
      await page.waitForSelector(`.tree a.current:has-text('${title}')`); // the tree opens down to the new page
      await page.click(".tree a:has-text('Runbook')");
      await page.waitForSelector(".tree a.current:has-text('Runbook')");
    }
    const children = () => page.locator(".tree li.open > ul > li > .tree-row a").allTextContents();
    check((await children()).join(",") === "Install,Upgrade", "children in the order made: " + (await children()));
    await page.click(".tree a:has-text('Upgrade')");
    await page.waitForSelector(".tree a.current:has-text('Upgrade')");
    await page.click("button:has-text('Move up')");
    await page.waitForFunction(() => [...document.querySelectorAll(".tree li.open > ul > li > .tree-row a")].map((a) => a.textContent).join(",") === "Upgrade,Install");
    await page.dragAndDrop(".tree .tree-row:has(a:text-is('Install'))", ".tree .tree-row:has(a:text-is('Upgrade'))", { targetPosition: { x: 40, y: 1 } });
    await page.waitForFunction(() => [...document.querySelectorAll(".tree li.open > ul > li > .tree-row a")].map((a) => a.textContent).join(",") === "Install,Upgrade");

    step("creating: New offers a page below the page shown, in its space");
    await page.click(".tree a:has-text('Upgrade')");
    await page.waitForSelector("#item-new-child");
    // The head's buttons stand left of the bell, which is last; the tree
    // makes no pages (ADR-0112).
    check(await page.$eval(".page-head .actions", (p) => p.lastElementChild.id) === "watch" && await page.locator(".tree-box a.tree-new").count() === 0,
      "the bell is the last of the page's buttons");
    await page.click("#nav-new");
    await page.waitForSelector("#new-item [name=under]:checked");
    await page.fill("#new-item [name=title]", "Created from New");
    await page.selectOption("#new-item [name=type]", "task");
    await page.selectOption("#new-item [name='p:priority']", "low");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector(".tree a.current:has-text('Created from New')");
    check(await page.locator(".tree li.open > .tree-row a:has-text('Upgrade')").count() === 1, "the new page lies below the page it was created from");
    await page.fill("#title [name=title]", "Created from New, renamed"); // Done saves the title too
    await page.click("#edit-done"); // a new page opened for writing; Done shows it for reading
    await page.waitForSelector("#body.reading");
    await page.waitForSelector("h1.item-title:text-is('Created from New, renamed')");
    await page.click("#item-new-child"); // the item's own "new child page"
    await page.waitForSelector("#new-item [name=under]:checked");
    await page.goBack();

    step("blocks: a task, a warning panel, a table, a table of contents and lists of tables and figures survive a reload (ADR-0119)");
    await page.click(".tree a:has-text('Install')");
    await page.waitForSelector(".tree a.current:has-text('Install')");
    await page.waitForSelector("#body.reading"); // an existing page opens for reading
    await page.click("#edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    await page.locator("#body .ProseMirror").click();
    await page.keyboard.type("Prepare");
    await page.selectOption("#body .pm-toolbar select", "heading:2");
    await page.keyboard.press("End");
    await page.keyboard.press("Enter");
    await page.keyboard.type("[] Check the backups");
    await page.waitForSelector("#body .pm-task");
    await page.check("#body .pm-task input[type=checkbox]");
    await page.click("#body .pm-task-text"); // back into the text: the checkbox took the focus
    await page.keyboard.press("End");
    await page.keyboard.press("Enter");
    await page.keyboard.press("Enter"); // an empty task ends the list
    await page.keyboard.type("Writers must be stopped.");
    await page.selectOption("#body .pm-toolbar select", "panel:warning");
    await page.selectOption("#body .pm-toolbar select >> nth=1", "table");
    await page.fill("#body .pm-table tr:nth-child(2) td:first-child input", "db1");
    await page.press("#body .pm-table tr:nth-child(2) td:first-child input", "Tab");
    await page.selectOption("#body .pm-toolbar select >> nth=1", "toc");
    check(await page.locator("#body .pm-toolbar select >> nth=1").locator("option[value=toc]").textContent() === "Table of contents", "the insert menu says Table of contents");
    await page.selectOption("#body .pm-toolbar select >> nth=1", "tables");
    await page.selectOption("#body .pm-toolbar select >> nth=1", "figures");
    await page.click("#details h2"); // leave the editor: its blocks are released and saved
    await page.waitForSelector(".live[data-pending='0']");
    await page.reload();
    await page.waitForSelector("#body .pm-task[data-checked='true']:has-text('Check the backups')");
    await page.waitForSelector("#body .pm-panel[data-tone='warning']:has-text('Writers must be stopped.')");
    check(await page.inputValue("#body .pm-table tr:nth-child(2) td:first-child input") === "db1", "the table cell kept its text");
    await page.waitForSelector("#body .pm-toc button:has-text('Prepare')");
    await page.waitForSelector("#body .pm-tables button:has-text('Table 1: ')"); // numbered, with its header row
    await page.waitForSelector("#body .pm-figures:has-text('No pictures yet.')");
    await page.click("#edit-done"); // read: the same blocks, shown, with no stray text
    await page.waitForSelector("#body.reading .pm-table td:has-text('db1')");
    check(!(await page.textContent("#body")).includes("null"), "the page for reading shows no stray text");

    step("versions: the changes of a body show word by word, and an earlier body comes back");
    // Two versions written through the API, so their content does not
    // depend on when the live editor saved.
    await page.click(".tree a:has-text('Upgrade')");
    await page.waitForSelector(".tree a.current:has-text('Upgrade')");
    await page.evaluate(async () => {
      const key = decodeURIComponent(location.hash.split("/").pop());
      const put = (blocks) => fetch(`/api/items/${key}/body`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ blocks }) });
      await put([{ kind: "paragraph", text: "Upgrade the primary first." }]);
      await put([{ kind: "paragraph", text: "Upgrade the replicas first." }, { kind: "task", text: "Check the version" }]);
    });
    await page.reload();
    await page.waitForSelector("#body .pm-task");
    await page.click("table.history button:has-text('Changes') >> nth=0"); // the newest change
    await page.waitForSelector("#version .diff-change ins:has-text('replicas')");
    await page.waitForSelector("#version .diff-add:has-text('Check the version')");
    await page.click("table.history button:has-text('Changes') >> nth=1"); // the one before
    await page.waitForSelector("#version h3");
    await page.click("#version button:has-text('Restore this version')");
    await page.waitForSelector("#body .ProseMirror:has-text('Upgrade the primary first.')");
    check(await page.locator("#body .pm-task").count() === 0, "the restored body is the earlier one");

    step("filters: every list has one (ADR-0098); the history narrows as one types, keeps it in the address, and resets");
    await page.waitForSelector("#history-filterbar:not([hidden])");
    const historyRows = () => page.locator("table.history tr:not([hidden])").count();
    const allHistory = await historyRows();
    await page.selectOption("#history-filterbar select[data-choice=kind]", "BodyCommitted");
    await page.waitForSelector("#history-filterbar .filter-count:has-text('of')");
    const bodies = await historyRows();
    check(bodies > 0 && bodies < allHistory, `the kind narrows the history: ${bodies} of ${allHistory}`);
    check(page.url().includes("history.kind=BodyCommitted"), "the filter is kept in the address");
    await page.fill("#history-filter", "zzzz-nothing");
    await page.waitForSelector("#history-filter-empty:not([hidden])");
    await page.click("#history-filterbar .filter-reset");
    await page.waitForFunction((n) => document.querySelectorAll("table.history tr:not([hidden])").length === n, allHistory);
    check(!page.url().includes("history."), "a reset clears the address");

    step("columns: the page tree and the side column fold away and stay folded (ADR-0100)");
    await page.click("#fold-side");
    await page.waitForSelector("html.side-folded #comments", { state: "hidden" });
    await page.click("#fold-tree");
    await page.reload();
    await page.waitForSelector("html.tree-folded.side-folded #fold-side");
    await page.click("#fold-side");
    await page.click("#fold-tree");
    await page.waitForSelector("#comments");

    step("reading mode: the content alone, by its button or Ctrl+Shift+F, ended by Esc (ADR-0101)");
    await page.click("#reading-toggle");
    await page.waitForSelector("html.reading-mode header.top", { state: "hidden" });
    await page.waitForSelector("#reading-exit:visible");
    check(await page.locator(".item-side").isHidden(), "the side column is away while reading");
    await page.keyboard.press("Escape");
    await page.waitForSelector("header.top", { state: "visible" });
    await page.keyboard.press("Control+Shift+F");
    await page.waitForSelector("html.reading-mode");
    await page.click("#reading-exit");
    await page.waitForSelector("#reading-toggle", { state: "visible" });

    step("labels: a label added to a page lists the page");
    await page.fill("#labels [name=label]", "Disaster Recovery");
    await page.press("#labels [name=label]", "Enter");
    await page.waitForSelector("#labels .label a:text-is('disaster-recovery')");
    await page.click("#labels .label a:text-is('disaster-recovery')");
    await page.waitForSelector("#labelled td:has-text('Upgrade')");
    await page.goBack();
    await page.waitForSelector("#labels .label");
    // Several at once with commas, by the button; one typed and left is
    // added too; a label that breaks the rule is refused beside the labels.
    await page.fill("#labels [name=label]", "alpha, Beta Gamma");
    await page.click("#labels .label-add-button");
    await page.waitForSelector("#labels .label a:text-is('beta-gamma')");
    await page.fill("#labels [name=label]", "left behind");
    await page.click("h1.item-title");
    await page.waitForSelector("#labels .label a:text-is('left-behind')");
    await page.fill("#labels [name=label]", "Sven's");
    await page.press("#labels [name=label]", "Enter");
    await page.waitForSelector("#labels .error:has-text(\"Not possible: Sven's\")");
    check(await page.locator("#labels .label").count() === 4, "the refused label is not added");
    await page.fill("#labels [name=label]", "");
    while (await page.locator("#labels .label-remove").count()) {
      const n = await page.locator("#labels .label").count();
      await page.click("#labels .label-remove >> nth=0");
      await page.waitForFunction((n) => document.querySelectorAll("#labels .label").length === n - 1, n);
    }

    step("inline comments: a comment on selected text highlights it, takes a reply and is resolved");
    await page.click("#body .ProseMirror p");
    await page.evaluate(() => {
      const text = document.querySelector("#body .ProseMirror p").firstChild;
      const at = text.data.indexOf("primary");
      const range = document.createRange();
      range.setStart(text, at);
      range.setEnd(text, at + "primary".length);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    });
    await page.waitForTimeout(100); // the editor reads the selection on selectionchange
    await page.click("#comment-selection");
    await page.fill("#inline-comment [name=text]", "Which host is the primary?");
    await page.click("#inline-comment button[type=submit]");
    await page.waitForSelector("#body .comment-anchor:text-is('primary')");
    await page.waitForSelector(".thread.inline .comment-quote:has-text('primary')");
    await page.click(".thread.inline button:has-text('Reply')");
    await page.fill(".thread.inline [name=text]", "db1, see the table.");
    await page.click(".thread.inline button[type=submit]");
    await page.waitForSelector(".thread.inline .replies .comment-text:has-text('db1, see the table.')");
    await page.click(".thread.inline button:has-text('Resolve')");
    await page.waitForSelector(".resolved-threads summary:has-text('Resolved (1)')");
    check(await page.locator("#body .comment-anchor").count() === 0, "a resolved thread's text is not highlighted");

    step("personal notes: a note on selected text, marked and listed for its maker alone, also in reading mode (ADR-0107)");
    await page.click("#body .ProseMirror p");
    await page.evaluate(() => {
      const text = document.querySelector("#body .ProseMirror p").firstChild;
      const at = text.data.indexOf("primary");
      const range = document.createRange();
      range.setStart(text, at);
      range.setEnd(text, at + "primary".length);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    });
    await page.waitForTimeout(100);
    await page.click("#note-selection");
    await page.waitForSelector("dialog#note-dialog[open] .note-quote:text-is('primary')");
    await page.fill("#note-text", "Check the failover with the ops team");
    await page.click("#note-save");
    await page.waitForSelector("#note-dialog", { state: "detached" });
    await page.waitForSelector("#body .note-anchor:text-is('primary')");
    await page.waitForSelector("#notes-list .note-text:has-text('Check the failover')");
    check((await page.textContent("#notes")).includes("Only you see your notes"), "the list says the notes are private");
    const shared = await page.evaluate(async () => (await (await fetch(location.hash.replace("#/item/", "/api/items/").split("?")[0] + "/history")).text()));
    check(!shared.includes("failover"), "a note is in no shared view of the item, such as its history");
    await page.click("#reading-toggle");
    await page.waitForSelector("html.reading-mode");
    await page.click("#body .note-anchor");
    await page.waitForSelector("dialog#note-dialog[open]");
    check(await page.inputValue("#note-text") === "Check the failover with the ops team", "in reading mode the note opens from its passage");
    await page.fill("#note-text", "Failover tested on Monday");
    await page.click("#note-save");
    await page.waitForSelector("#note-dialog", { state: "detached" });
    await page.click("#reading-exit");
    await page.waitForSelector("#notes-list .note-text:has-text('Failover tested on Monday')");
    // The start page leads to one's notes, wherever they were made, and
    // each back to its page (ADR-0113).
    const notedAt = await page.evaluate(() => location.hash);
    await page.goto(base + "/#/");
    await page.waitForSelector("#home-my-notes:has-text('1 note')");
    await page.click("#home-my-notes");
    await page.waitForSelector("#my-notes li:has-text('Failover tested on Monday') blockquote.my-note-quote:text-is('primary')");
    check((await page.textContent("main")).includes("Only you see your notes"), "the list says the notes are private");
    await page.click("#my-notes li a");
    await page.waitForFunction((h) => location.hash.split("?")[0] === h.split("?")[0], notedAt);
    await page.waitForSelector("#notes-list .note-text:has-text('Failover tested on Monday')");
    await page.click("#notes-list .note-open");
    answer = true;
    await page.click("#note-delete");
    await page.waitForSelector("#body .note-anchor", { state: "detached" });
    await page.waitForSelector("#notes-list li.muted");

    step("relations and access: a page is related by key and unrelated; only I may edit it, then everyone again");
    await page.fill("#add-relation [name=type]", "depends-on");
    await page.fill("#add-relation [name=target]", "PLAT-1");
    await page.click("#add-relation button[type=submit]");
    await page.waitForSelector("#relations li:has-text('depends-on') a:has-text('PLAT-1')");
    await page.click("#relations li:has-text('depends-on') button[aria-label='Remove this relation']");
    await page.waitForSelector("#relations li:has-text('depends-on')", { state: "detached" });
    await page.waitForSelector("#access.access-open .access-bar:has-text('Open')"); // open by default, an open lock
    await page.click("#access button:has-text('Only I may edit')");
    await page.waitForSelector("#access p:has-text('May edit: you')");
    await page.waitForSelector("#access.access-restricted .access-bar:has-text('Restricted')");
    await page.waitForSelector("h1.item-title .lock"); // the title carries the closed lock too
    await page.click("#access button:has-text('Lift the restriction')");
    await page.waitForSelector("#access p:has-text('May edit: everyone with a role in the room')");
    await page.waitForSelector("#access.access-open");

    step("files: an upload is listed and downloads; an image goes into the body and shows after a reload");
    const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");
    await page.setInputFiles("#upload", { name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("<script>alert('file')</script>") });
    await page.waitForSelector("#attachments li:has-text('notes.txt')");
    const notes = await page.getAttribute("#attachments li a:has-text('notes.txt')", "href");
    const served = await page.evaluate(async (href) => { const r = await fetch(href); return [r.headers.get("content-type"), r.headers.get("content-disposition"), await r.text()]; }, notes);
    check(served[0] === "application/octet-stream" && served[1].startsWith("attachment") && served[2].includes("alert('file')"), "a text file only downloads: " + served);
    await page.click("#edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    await page.locator("#body .ProseMirror").click();
    await page.setInputFiles("#body .pm-toolbar input[type=file]", { name: "pixel.png", mimeType: "image/png", buffer: png });
    await page.waitForSelector("#body .pm-image img");
    await page.click("#edit-done"); // Done saves what is not yet saved and shows the page for reading
    await page.waitForSelector("#body.reading");
    await page.waitForFunction(() => { const img = document.querySelector("#body .pm-image img"); return img && img.complete && img.naturalWidth === 1; });
    check(await page.locator("#attachments li").count() === 2, "both files are listed");

    step("drawing: a drawing made in the drawing editor goes into the body, keeps its scene and is edited again");
    const drawingKey = decodeURIComponent(page.url().split("/item/")[1].split("?")[0]);
    await page.click("#edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    await page.locator("#body .ProseMirror").click();
    await page.selectOption("#body .pm-toolbar select:has(option[value=drawing])", "drawing");
    await page.waitForSelector("dialog.drawing[open] iframe");
    let drawFrame = page.frameLocator("dialog.drawing iframe");
    await drawFrame.locator("body[data-state=open] .excalidraw canvas").first().waitFor();
    await drawFrame.locator("[data-testid=toolbar-rectangle]").click({ force: true }); // the tool is a radio button under its icon
    const box = await page.locator("dialog.drawing iframe").boundingBox();
    await page.mouse.move(box.x + box.width / 2 - 120, box.y + box.height / 2 - 60);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 120, box.y + box.height / 2 + 60, { steps: 8 });
    await page.mouse.up();
    await drawFrame.locator("body[data-elements='1']").waitFor();
    await page.click("dialog.drawing button:has-text('Save drawing')");
    await page.waitForSelector("dialog.drawing", { state: "detached" });
    await page.waitForFunction(() => document.querySelectorAll("#body .pm-image").length === 2);
    // A click on the drawing selects it, and Delete removes it like text; so
    // does its Remove button. Undo brings it back.
    await page.waitForTimeout(600); // changes within 500 ms are undone together
    await page.click("#body .pm-image:has(.pm-redraw) img");
    await page.waitForSelector("#body .pm-image.ProseMirror-selectednode:has(.pm-redraw)");
    await page.keyboard.press("Delete");
    await page.waitForFunction(() => document.querySelectorAll("#body .pm-image").length === 1);
    await page.keyboard.press("Control+z");
    await page.waitForFunction(() => document.querySelectorAll("#body .pm-image").length === 2);
    await page.waitForTimeout(600);
    await page.click("#body .pm-image:has(.pm-redraw) .pm-image-remove");
    await page.waitForFunction(() => document.querySelectorAll("#body .pm-image").length === 1);
    await page.keyboard.press("Control+z");
    await page.waitForFunction(() => document.querySelectorAll("#body .pm-image").length === 2);
    await page.click("#body .pm-redraw"); // only the drawing can be edited as a drawing
    await page.waitForSelector("dialog.drawing[open] iframe");
    drawFrame = page.frameLocator("dialog.drawing iframe");
    await drawFrame.locator("body[data-state=open][data-elements='1']").waitFor(); // the scene came back from the picture
    // A palette (ADR-0117) fills the library; a shape of it goes onto the canvas.
    await drawFrame.locator("select.palette:visible").selectOption("bpmn");
    await drawFrame.locator("body[data-palette=bpmn]").waitFor();
    check(await drawFrame.locator(".library-unit").count() === 34, "the BPMN palette is in the library");
    // The ArchiMate palette ends with its relationships, and the Microsoft
    // palette holds Fluent icons as shapes (ADR-0125).
    await drawFrame.locator("select.palette:visible").selectOption("archimate");
    await drawFrame.locator("body[data-palette=archimate]").waitFor();
    check(await drawFrame.locator(".library-unit").count() === 52, "the ArchiMate palette holds its elements and relationships");
    await drawFrame.locator("select.palette:visible").selectOption("microsoft");
    await drawFrame.locator("body[data-palette=microsoft]").waitFor();
    check(await drawFrame.locator(".library-unit").count() === 67, "the Microsoft palette is in the library");
    await drawFrame.locator("select.palette:visible").selectOption("bpmn");
    await drawFrame.locator("body[data-palette=bpmn]").waitFor();
    await drawFrame.locator(".library-unit").nth(4).click(); // the start event
    await drawFrame.locator("body[data-elements='2']").waitFor();
    await page.click("dialog.drawing button:has-text('Cancel')");
    await page.waitForSelector("dialog.drawing", { state: "detached" });
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    await page.waitForFunction(() => { const imgs = [...document.querySelectorAll("#body .pm-image img")]; return imgs.length === 2 && imgs.every((i) => i.complete && i.naturalWidth > 0) && imgs.some((i) => i.naturalWidth > 100); });
    check(await page.locator("#attachments li:has-text('.drawing.png')").count() === 1, "the drawing is a file of the page");
    check(await page.locator("#body.reading .pm-redraw").count() === 0, "a drawing is edited only while editing");

    step("image size: a picture is set to half the column, then dragged smaller, and keeps its size after a reload");
    const ratio = () => page.evaluate(() => {
      const fig = [...document.querySelectorAll("#body .pm-image")].find((f) => f.querySelector("img").naturalWidth > 100);
      return fig.querySelector(".pm-image-frame").getBoundingClientRect().width / fig.getBoundingClientRect().width;
    });
    await page.click("#edit");
    await page.waitForSelector("#body.editing .pm-redraw");
    const drawingFigure = page.locator("#body .pm-image:has(.pm-redraw)");
    await drawingFigure.locator("select.pm-image-size").selectOption("50");
    await page.waitForFunction(() => document.querySelector("#body .pm-image:has(.pm-redraw) select.pm-image-size").value === "50");
    const half = await ratio();
    check(Math.abs(half - 0.5) < 0.03, "the picture takes half the column: " + half);
    await drawingFigure.hover();
    const grip = await drawingFigure.locator(".pm-resize").boundingBox();
    const columnWidth = (await drawingFigure.boundingBox()).width;
    await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
    await page.mouse.down();
    await page.mouse.move(grip.x + grip.width / 2 - columnWidth * 0.2, grip.y + grip.height / 2, { steps: 6 });
    await page.mouse.up();
    await page.waitForFunction(() => document.querySelector("#body .pm-image:has(.pm-redraw) select.pm-image-size").value === "30");
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    await page.reload();
    await page.waitForSelector("#body.reading .pm-image img");
    await page.waitForFunction(() => [...document.querySelectorAll("#body .pm-image img")].every((i) => i.complete));
    const kept = await ratio();
    check(Math.abs(kept - 0.3) < 0.03, "the dragged size is kept: " + kept);

    step("editor: text centred and underlined, a divider, a picture floated right; all kept after a reload");
    await page.click("#edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    const firstText = page.locator("#body .ProseMirror > p").first();
    await firstText.click({ clickCount: 3 });
    await page.click("#body .pm-toolbar button[aria-label^='Underline']");
    await page.click("#body .pm-toolbar button[aria-label^='Centre']");
    await page.waitForSelector("#body .pm-toolbar button[aria-label^='Centre'][aria-pressed=true]");
    await page.selectOption("#body .pm-toolbar select:has(option[value=divider])", "divider");
    await page.locator("#body .pm-image:has(.pm-redraw) select.pm-image-align").selectOption("right");
    await page.waitForSelector("#body .pm-image.pm-image-right");
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    await page.reload();
    await page.waitForSelector("#body.reading p.pm-align-center u");
    await page.waitForSelector("#body.reading hr.pm-divider");
    await page.waitForSelector("#body.reading .pm-image.pm-image-right");

    step("album: every readable picture in one place; one is copied into another page and stays editable as a drawing");
    await page.goto(base + "/#/album");
    await page.waitForSelector("#album .album-card img");
    check(await page.locator("#album .album-card").count() === 2, "the album shows the image and the drawing, not the text file");
    await page.selectOption("#album select[aria-label=Kind]", "drawing");
    await page.waitForFunction(() => document.querySelectorAll("#album .album-card").length === 1);
    await page.goto(base + "/#/item/PLAT-2?edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    await page.locator("#body .ProseMirror").click();
    await page.selectOption("#body .pm-toolbar select:has(option[value=album])", "album");
    await page.waitForSelector("dialog.album-dialog[open] .album-card");
    await page.fill("dialog.album-dialog input[type=search]", "drawing");
    await page.selectOption("dialog.album-dialog select[aria-label=Kind]", "drawing");
    await page.waitForFunction(() => document.querySelectorAll("dialog.album-dialog .album-card").length === 1);
    await page.click("dialog.album-dialog .album-pick");
    await page.waitForSelector("dialog.album-dialog", { state: "detached" });
    await page.waitForSelector("#body .pm-image .pm-redraw"); // the copy is a drawing of this page
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    await page.waitForFunction(() => { const img = document.querySelector("#body .pm-image img"); return img && img.complete && img.naturalWidth > 100; });
    check(await page.locator("#attachments li:has-text('.drawing.png')").count() === 1, "the copy is a file of the page it was inserted into");
    await page.waitForSelector(`#attachments li:has-text('.drawing.png') .att-origin a:has-text('${drawingKey}')`); // the copy names its origin

    step("album folders: a folder is made, a picture uploaded into it, labelled, found by its label and deleted (ADR-0090)");
    await page.goto(base + "/#/album");
    await page.waitForSelector("#album .album-card img");
    await page.selectOption("#album select[aria-label=Room]", "PLAT");
    await page.click("#album-new-folder");
    await page.fill("#album-folder-name", "Logos");
    await page.click("#album .album-new-folder button[type=submit]");
    await page.click("#album .album-folder:has-text('Logos')");
    await page.waitForSelector("#album-upload");
    await page.setInputFiles("#album-files", { name: "brand.png", mimeType: "image/png", buffer: png });
    const brandCard = "#album .album-card:has(.album-name:text-is('brand'))";
    await page.waitForSelector(brandCard + " .album-labels-input");
    check(await page.locator("#album .album-card").count() === 1, "the folder shows its own picture only");
    await page.fill(brandCard + " .album-labels-input", "Logo, print");
    await page.press(brandCard + " .album-labels-input", "Enter");
    await page.waitForSelector(brandCard + " .album-label:text-is('logo')");
    await page.click("#album .album-trail button:has-text('All pictures')");
    await page.waitForSelector("#album-label option[value=logo]", { state: "attached" });
    await page.selectOption("#album-label", "logo");
    await page.waitForFunction(() => { const cards = document.querySelectorAll("#album .album-card"); return cards.length === 1 && cards[0].textContent.includes("brand"); });
    const itemLabels = await page.evaluate(async () => (await (await fetch("/api/labels")).json()).labels.map((l) => l.label));
    check(!itemLabels.includes("logo"), "a picture's label is no label of an item, so no kanban shows it");
    answer = true;
    await page.click(brandCard + " .album-delete");
    await page.waitForSelector(brandCard, { state: "detached" });

    step("provenance: the original is drawn again; its copy learns of it and takes the newer version over");
    await page.goto(base + "/#/item/" + drawingKey + "?edit");
    await page.waitForSelector("#body.editing .pm-redraw");
    await page.click("#body .pm-redraw");
    await page.waitForSelector("dialog.drawing[open] iframe");
    let again = page.frameLocator("dialog.drawing iframe");
    await again.locator("body[data-state=open][data-elements='1']").waitFor();
    await again.locator("[data-testid=toolbar-ellipse]").click({ force: true });
    const box2 = await page.locator("dialog.drawing iframe").boundingBox();
    await page.mouse.move(box2.x + box2.width / 2 + 160, box2.y + box2.height / 2 - 40);
    await page.mouse.down();
    await page.mouse.move(box2.x + box2.width / 2 + 260, box2.y + box2.height / 2 + 40, { steps: 8 });
    await page.mouse.up();
    await again.locator("body[data-elements='2']").waitFor();
    await page.click("dialog.drawing button:has-text('Save drawing')");
    await page.waitForSelector("dialog.drawing", { state: "detached" });
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    await page.goto(base + "/#/item/PLAT-2");
    await page.waitForSelector("#attachments .att-origin .chip:has-text('original changed')");
    await page.goto(base + "/#/item/PLAT-2?edit");
    await page.waitForSelector(`#body.editing .pm-take-over:has-text('${drawingKey}')`);
    await page.click("#body .pm-take-over");
    await page.waitForFunction(() => !document.querySelector("#body .pm-take-over")); // the block now shows the newer version
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    check(await page.locator("#attachments li:has-text('.drawing.png')").count() === 2, "the newer version is a copy of its own");

    step("album versions: the drawing drawn again is one stack, the newest on top; it fans out and folds (ADR-0103)");
    await page.goto(base + "/#/album");
    await page.waitForSelector("#album .album-card img");
    await page.fill("#album input[type=search]", drawingKey);
    await page.selectOption("#album select[aria-label=Kind]", "drawing");
    const stackCard = "#album .album-stack";
    await page.waitForSelector(stackCard + " .album-stack-top[aria-expanded=false]");
    check(await page.locator(stackCard).count() === 1, "the versions of the drawing are one stack");
    const versions = Number(await page.locator(stackCard + " .album-stack-count").textContent());
    check(versions >= 2, "the stack counts the drawing's versions");
    check(await page.locator(stackCard + " .album-version").textContent() === "Version " + versions, "the top is the newest version");
    check(await page.locator(stackCard + " .album-behind").count() === Math.min(versions - 1, 2), "older versions lie half hidden behind it");
    await page.click(stackCard + " .album-stack-top");
    await page.waitForSelector("#album .album-fan .album-card .album-version:text-is('Version 1')");
    check(await page.locator("#album .album-fan .album-card").count() === versions - 1, "fanned out, every older version shows");
    await page.click(stackCard + " .album-stack-top");
    await page.waitForSelector("#album .album-fan", { state: "detached" });

    step("album retention: the default is set in the administration with a preview; a picture's own rule takes precedence and cleans it up (ADR-0104)");
    await page.goto(base + "/#/admin");
    await page.waitForSelector("#admin-album #album-retention-preview p");
    check((await page.textContent("#album-retention-preview")).includes("Nothing would be removed"), "the default keeps every version");
    await page.fill("#album-retention-keep", "1");
    await page.waitForSelector("#album-retention-pictures td:has-text('" + drawingKey + "')");
    await page.click("#album-retention-save");
    await page.waitForSelector("#admin-album .notice:has-text('Saved')");
    await page.goto(base + "/#/album");
    await page.waitForSelector("#album .album-card img");
    await page.fill("#album input[type=search]", drawingKey);
    await page.selectOption("#album select[aria-label=Kind]", "drawing");
    await page.waitForSelector(stackCard + " .album-retention");
    await page.click(stackCard + " .album-retention");
    await page.waitForSelector("dialog#retention-dialog[open] #retention-list li");
    check(await page.inputValue("#retention-keep") === "1", "the picture starts from the album's default");
    check((await page.textContent("#retention-source")).includes("default"), "the dialog says the default applies");
    await page.waitForSelector("#retention-list li[data-state=remove]");
    await page.fill("#retention-keep", String(versions));
    await page.waitForFunction(() => !document.querySelector("#retention-list li[data-state=remove]"));
    check(await page.isDisabled("#retention-clean"), "keeping every version leaves nothing to remove");
    await page.click("#retention-save");
    await page.waitForSelector("#retention-dialog .notice");
    check((await page.textContent("#retention-source")).includes("own"), "the picture has a rule of its own");
    await page.fill("#retention-keep", "1");
    await page.waitForSelector("#retention-list li[data-state=remove]");
    answer = true;
    await page.click("#retention-clean");
    await page.waitForFunction(() => document.querySelectorAll("#retention-list li").length === 1);
    await page.click("#retention-close");
    await page.waitForSelector("#retention-dialog", { state: "detached" });
    await page.waitForFunction(() => !document.querySelector("#album .album-stack"));
    check(await page.locator("#album .album-card").count() === 1, "one version is left, so the stack is a single picture");

    step("album copy: a picture copied in the album is pasted into a page with Ctrl+V as a copy of its original, or inserted as the copied picture (ADR-0108)");
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: base });
    await page.goto(base + "/#/album");
    await page.waitForSelector("#album .album-card img");
    await page.fill("#album input[type=search]", drawingKey);
    await page.selectOption("#album select[aria-label=Kind]", "drawing");
    await page.waitForFunction(() => document.querySelectorAll("#album .album-card").length === 1);
    await page.click("#album .album-card .album-copy");
    await page.waitForSelector("#album .notice:has-text('copied')");
    check(await page.evaluate(() => Boolean(JSON.parse(localStorage.getItem("wwbb.albumClip"))?.attachment?.id)), "this browser keeps the copied picture");
    await page.goto(base + "/#/item/PLAT-2?edit");
    await page.waitForSelector("#body.editing .ProseMirror");
    const copiesBefore = await page.locator("#body .pm-image").count();
    await page.click("#body .ProseMirror");
    await page.keyboard.press("End");
    await page.keyboard.press("Control+V");
    await page.waitForFunction((n) => document.querySelectorAll("#body .pm-image").length === n + 1, copiesBefore);
    await page.selectOption("#body .pm-toolbar select:has(option[value=clip])", "clip");
    await page.waitForFunction((n) => document.querySelectorAll("#body .pm-image").length === n + 2, copiesBefore);
    await page.click("#edit-done");
    await page.waitForSelector("#body.reading");
    check(await page.locator(`#attachments li:has-text('.drawing.png') .att-origin a:has-text('${drawingKey}')`).count() >= 3, "each pasted picture is a copy that names its original");

    step("links: the link check page says when checking is off");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.click("a:has-text('Link check')");
    await page.waitForSelector("#linkcheck >> text=switched off");

    step("governance: the check of a space lists its three kinds of findings");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.click("main a:has-text('Governance')");
    await page.waitForSelector("#governance h2:has-text('Sensitive data')");
    await page.waitForSelector("#governance h2:has-text('Not changed')");
    await page.waitForSelector("#governance h2:has-text('Implausible')");

    step("catalog: a url field opens its address, a unique field refuses a second value, and a review is recorded from the governance check (ADR-0074)");
    // Through the page's own session, outside the page: the refused
    // request is expected and must not count as a console error.
    const api = (path, data) => page.request.post(base + path, { data });
    check((await api("/api/types", { name: "service", body: false,
      fields: [{ name: "source-id", kind: "text", unique: true }, { name: "specification", kind: "url" },
        { name: "contact", kind: "text", recommended: true }, { name: "owner", kind: "user" }, { name: "reviewed", kind: "date" }],
      review: { everyDays: 365, field: "reviewed", owner: "owner" } })).status() === 201, "a catalog type is defined");
    const created = await api("/api/items", { space: "PLAT", type: "service", title: "Weather API", properties: { "source-id": "meteo-1", specification: "https://example.org/openapi.json" } });
    const catalog = { key: (await created.json()).item.key,
      twice: (await api("/api/items", { space: "PLAT", type: "service", title: "Again", properties: { "source-id": "meteo-1" } })).status() };
    check(catalog.twice === 400, "a second item with the same unique value is refused");
    await page.goto(base + "/#/item/" + catalog.key);
    await page.waitForSelector("tr[data-field=specification] a.prop-link[href='https://example.org/openapi.json'][target=_blank]");
    check(await page.getAttribute("tr[data-field=specification] input", "type") === "url", "a url field takes a web address");
    await page.goto(base + "/#/governance/PLAT");
    await page.waitForSelector(`#governance tr[data-key='${catalog.key}'] button.mark-reviewed`);
    await page.waitForSelector("#governance td:has-text('recommended field \"contact\" is empty')");
    await page.click(`#governance tr[data-key='${catalog.key}'] button.mark-reviewed`);
    await page.waitForSelector(`#governance tr[data-key='${catalog.key}']`, { state: "detached" });

    step("governance in the bell: a password saved in a page restricts it, the bell tells, the finding stays listed until resolved");
    const leaked = await page.evaluate(async () => {
      const r = await fetch("/api/items", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ space: "PLAT", type: "ticket", title: "Leaked note", properties: { priority: "low" }, body: [{ kind: "paragraph", text: "Passwort: Sommer2026!" }] }) });
      return (await r.json()).item.key;
    });
    await page.goto(base + "/#/");
    await page.waitForSelector("#nav-notifications.findings.unread");
    check((await page.getAttribute("#nav-notifications", "aria-label")).includes("open governance finding"), "the bell names the open findings");
    await page.click("#nav-notifications");
    await page.waitForSelector(`#notifications li.governance:has-text('${leaked}'):has-text('because of sensitive data')`);
    await page.waitForSelector(`#my-findings li.finding-sensitive:has-text('${leaked}')`);

    step("data protection: a template is adopted; a medical certificate holds a ticket as a draft until a second person releases it");
    await page.goto(base + "/#/admin");
    await page.click("#admin-regulations button:has-text('Adopt template: DSG')");
    await page.waitForSelector("#regulation-dsg");
    // An own regulation explains its format and says what is missing.
    await page.click("#admin-regulations summary");
    await page.click("#regulation-new .info-btn");
    await page.waitForSelector("#regulation-new .info-pop:visible:has-text('One line per category')");
    await page.fill("#regulation-new [name=id]", "own");
    await page.fill("#regulation-new [name=name]", "Own rules");
    await page.click("#regulation-new button[type=submit]");
    await page.waitForSelector("#regulation-new .status:has-text('Enter at least one category')");
    await page.fill("#regulation-new [name=categories]", "Health");
    await page.click("#regulation-new button[type=submit]");
    await page.waitForSelector("#regulation-new .status:has-text('no colon')");
    await page.check("#regulation-dsg [name=active]");
    // The box is checked before saving; wait for the save itself, so the
    // ticket below is assessed against the active regulation.
    await Promise.all([
      page.waitForResponse((r) => r.url().endsWith("/api/regulations/dsg") && r.request().method() === "PUT" && r.ok()),
      page.click("#regulation-dsg button[type=submit]"),
    ]);
    const absence = await page.evaluate(async () => {
      const r = await fetch("/api/items", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ space: "PLAT", type: "ticket", title: "Absence", properties: { priority: "low" }, body: [{ kind: "paragraph", text: "Arztzeugnis bis Freitag" }] }) });
      return (await r.json()).item.key;
    });
    await page.goto(base + "/#/item/" + absence);
    await page.waitForSelector("#hold:has-text('Draft: data protection review open') .chip:has-text('Arztzeugnis')");
    check(await page.locator("#hold form").count() === 0, "who saved the content cannot release it");
    const reviewer = await browser.newPage();
    reviewer.on("pageerror", (e) => problems.push("reviewer's page error: " + e.message));
    await reviewer.goto(base + "/");
    await reviewer.fill("#login [name=username]", "bert");
    await reviewer.fill("#login [name=password]", "bert's password");
    await reviewer.click("#login button[type=submit]");
    await reviewer.waitForSelector("#nav-notifications.findings");
    await reviewer.goto(base + "/#/notifications");
    await reviewer.waitForSelector(`#my-findings li.finding-personal:has-text('${absence}'):has-text('waiting for your release')`);
    await reviewer.goto(base + "/#/item/" + absence);
    await reviewer.waitForSelector("#hold form");
    check(await reviewer.isDisabled("#hold input[value=restricted]"), "an open item cannot be released as restricted");
    await reviewer.check("#hold input[value=false-alarm]");
    await reviewer.fill("#hold [name=reason]", "a template, no real person");
    await reviewer.click("#hold button[type=submit]");
    await reviewer.waitForSelector("#hold", { state: "detached" });
    await reviewer.close();
    await page.reload();
    await page.waitForSelector("h1.item-title");
    check(await page.locator("#hold").count() === 0, "released for everyone");

    step("kanbans: two kanbans on topics; a new card and an existing ticket; one ticket on both; taken off one");
    await page.goto(base + "/#/tasks");
    await page.click("#tab-kanbans");
    await page.fill("#new-kanban [name=name]", "Website relaunch");
    await page.selectOption("#new-kanban [name=type]", "ticket");
    await page.click("#new-kanban button[type=submit]");
    await page.waitForSelector("h1:has-text('Website relaunch')");
    await page.fill("#kanban-add [name=title]", "Hero image");
    await page.click("#kanban-add button[type=submit]");
    await page.waitForSelector("#kanban .board-column[data-state=open] .card-item:has-text('Hero image')");
    await page.selectOption("#kanban-assign [name=item]", { label: (await page.locator("#kanban-assign option:has-text('Tidy the wiki')").textContent()).trim() });
    await page.click("#kanban-assign button[type=submit]");
    await page.waitForSelector("#kanban .card-item:has-text('Tidy the wiki')");
    await page.goto(base + "/#/kanbans");
    await page.fill("#new-kanban [name=name]", "Operations");
    await page.selectOption("#new-kanban [name=type]", "ticket");
    await page.click("#new-kanban button[type=submit]");
    await page.waitForSelector("h1:has-text('Operations')");
    await page.selectOption("#kanban-assign [name=item]", { label: (await page.locator("#kanban-assign option:has-text('Tidy the wiki')").textContent()).trim() });
    await page.click("#kanban-assign button[type=submit]");
    await page.waitForSelector("#kanban .card-item:has-text('Tidy the wiki')");
    await page.goto(base + "/#/kanbans");
    await page.waitForSelector("#kanbans .space-tile:has-text('Website relaunch'):has-text('2 cards')");
    await page.waitForSelector("#kanbans .space-tile:has-text('Operations'):has-text('1 card')");
    await page.click("#kanbans .space-tile:has-text('Website relaunch')");
    await page.click("#kanban .card-item:has-text('Tidy the wiki') .kanban-off");
    await page.waitForSelector("#kanban .card-item:has-text('Tidy the wiki')", { state: "detached" });
    await page.goto(base + "/#/kanbans");
    await page.waitForSelector("#kanbans .space-tile:has-text('Operations'):has-text('1 card')"); // still on the other kanban

    step("kanban board: details beside the board, the fields cards show, an own column, cards moved back and forth");
    await page.click("#kanbans .space-tile:has-text('Website relaunch')");
    await page.click("#kanban .card-item:has-text('Hero image') .card-title");
    await page.waitForSelector("#kanban-detail:not([hidden]) h2:has-text('Hero image')");
    check(/[?&]card=/.test(page.url()), "the open card is in the address");
    await page.click("#kanban-settings summary");
    await page.check("#kanban-card-fields input[value=priority]");
    await page.fill("#kanban-new-column", "Blocked");
    await page.click("#kanban-add-column");
    await page.click("#kanban-settings-save");
    await page.waitForSelector("#kanban .own-column h2:has-text('Blocked')");
    await page.waitForSelector("#kanban-detail:not([hidden]) tr[data-field=priority] select"); // still open after the redraw
    await page.selectOption("#kanban-detail tr[data-field=priority] select", "high");
    await page.waitForSelector("#kanban .card-item:has-text('Hero image') .card-facts dd:text-is('high')");
    await page.dragAndDrop("#kanban .card-item:has-text('Hero image')", "#kanban .own-column");
    await page.waitForSelector("#kanban .own-column .card-item:has-text('Hero image')");
    await page.dragAndDrop("#kanban .card-item:has-text('Hero image')", "#kanban .board-column[data-state=closed]");
    await page.waitForSelector("#kanban .board-column[data-state=closed] .card-item:has-text('Hero image')");
    // Back to open: the workflow has no such transition; an administrator adds it.
    await page.dragAndDrop("#kanban .card-item:has-text('Hero image')", "#kanban .board-column[data-state=open]");
    await page.click("#kanban-add-transition");
    await page.waitForSelector("#kanban .board-column[data-state=open] .card-item:has-text('Hero image')");

    step("kanban picture and boards by label: a kanban shows its logo; every label is a board without making one (ADR-0087)");
    if (!(await page.$("#kanban-settings[open]"))) await page.click("#kanban-settings summary");
    await page.setInputFiles("#kanban-icon-file", { name: "logo.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector(".kanban-title img.kanban-icon[src*='/icon?v=']");
    await page.goto(base + "/#/kanbans");
    await page.waitForSelector("#kanbans .space-tile:has-text('Website relaunch') img.kanban-icon");
    // Boards by label on the left, the kanbans in the middle, a new kanban
    // on the right (ADR-0110).
    const leftOf = async (sel) => (await page.locator(sel).boundingBox()).x;
    check(await leftOf("#label-boards") < await leftOf(".kanbans-main") && await leftOf(".kanbans-main") < await leftOf(".kanbans-new #new-kanban"),
      "boards by label left, kanbans in the middle, a new kanban right");
    // The labels are filtered as one types, suggested, and Enter opens the
    // one left.
    check(await page.getAttribute("#labelboards-filter", "list") === "labelboards-suggest" &&
      await page.locator("#labelboards-suggest option[value*=relaunch]").count() === 1, "the filter suggests the labels");
    await page.fill("#labelboards-filter", "relaun");
    await page.waitForFunction(() => [...document.querySelectorAll("#label-boards li")].filter((li) => !li.hidden).length === 1);
    await page.press("#labelboards-filter", "Enter");
    await page.waitForFunction(() => location.hash.startsWith("#/labelboard/") && location.hash.includes("relaunch"));
    await page.goto(base + "/#/kanbans");
    await page.click("#label-boards a:has-text('relaunch')");
    await page.waitForSelector("#labelboard .board-column[data-state=open] .card-item:has-text('Hero image')");
    await page.waitForSelector("a.kanban-chip:has-text('Website relaunch')"); // the kept kanban of the label
    await page.dragAndDrop("#labelboard .card-item:has-text('Hero image')", "#labelboard .board-column[data-state=closed]");
    await page.waitForSelector("#labelboard .board-column[data-state=closed] .card-item:has-text('Hero image')");
    await page.dragAndDrop("#labelboard .card-item:has-text('Hero image')", "#labelboard .board-column[data-state=open]");
    await page.waitForSelector("#labelboard .board-column[data-state=open] .card-item:has-text('Hero image')");
    await page.goto(base + "/#/labelboard/no-such-label");
    await page.waitForSelector("#labelboard-empty");
    // Labelling suggests the labels in use while typing.
    const tidy = (await (await page.request.get(base + "/api/items?label=operations")).json()).items[0].key;
    await page.goto(base + "/#/item/" + tidy);
    await page.locator("#labels [name=label]").pressSequentially("relau");
    await page.waitForSelector("#label-options option[value='website-relaunch']", { state: "attached" });
    await page.fill("#labels [name=label]", "");
    // The search in the header finds items by their labels, and offers the
    // labels themselves (ADR-0092).
    await page.fill("#nav .search input", "relaunch");
    await page.press("#nav .search input", "Enter");
    await page.waitForSelector("#search-labels a:has-text('website-relaunch')");
    await page.waitForSelector("ol.hits li:has-text('Hero image')");
    // While typing, the search at the top suggests labels; the arrow keys
    // and Enter open the chosen one.
    await page.locator("#nav .search input").fill("");
    await page.locator("#nav .search input").pressSequentially("relau");
    await page.waitForSelector("#search-suggest:not([hidden]) [role=option][data-label='website-relaunch']");
    await page.press("#nav .search input", "ArrowDown");
    await page.press("#nav .search input", "Enter");
    await page.waitForFunction(() => location.hash === "#/label/website-relaunch");
    await page.waitForSelector("#labelled tr:has-text('Hero image')");
    // The suggestions offer items too; choosing one opens it.
    await page.locator("#nav .search input").pressSequentially("Hero ima");
    await page.click("#search-suggest:not([hidden]) [role=option][data-item]:has-text('Hero image')");
    await page.waitForFunction(() => location.hash.startsWith("#/item/"));
    await page.waitForSelector("h1:has-text('Hero image'), .page-head :has-text('Hero image')");

    step("task list: tasks on a kanban are listed too, with their label; the labels at the right filter the list");
    await page.goto(base + "/#/tasks/all");
    await page.waitForSelector("#tasks tr:has-text('Hero image') .task-labels a.kanban-chip");
    await page.waitForSelector("#tasks tr:has-text('Tidy the wiki') .task-labels a.kanban-chip");
    await page.click("#tasks-labels label:has-text('relaunch') input");
    await page.waitForSelector("#tasks tbody tr:has-text('Tidy the wiki')", { state: "hidden" });
    await page.waitForSelector("#tasks tbody tr:has-text('Hero image')");
    check(/labels=/.test(page.url()), "the label filter is in the address");
    await page.click("#tasks-labels label:has-text('operations') input");
    await page.waitForSelector("#tasks tbody tr:has-text('Tidy the wiki')");
    await page.selectOption("#tasks-label-mode", "all");
    await page.waitForSelector("#tasks p:has-text('No task matches the filters.')");
    await page.click("#tasks button:has-text('Reset filters')");
    await page.waitForSelector("#tasks tbody tr:has-text('Tidy the wiki')");

    step("access: an item is protected for a group from its page, and opened again");
    const meID = (await (await page.request.get(base + "/api/me")).json()).user.id;
    check((await page.request.put(base + "/api/groups/redaktion/members/" + meID)).status() === 204, "a group is made");
    await page.click("#tasks tr:has-text('Hero image') td:first-child a");
    await page.waitForSelector("#access-edit");
    await page.click("#access-edit summary");
    await page.fill("#access-who", "@redaktion");
    await page.waitForSelector("#access-options option[value='@redaktion']", { state: "attached" });
    await page.click("#access-add");
    await page.waitForSelector("#access-entries li[data-principal='@redaktion']");
    await page.click("#access-save button[type=submit]");
    await page.waitForSelector("#access.access-restricted p:has-text('May read: group redaktion')");
    await page.click("#access button:has-text('Lift the restriction')");
    await page.waitForSelector("#access.access-open");
    check((await page.request.delete(base + "/api/groups/redaktion/members/" + meID)).status() === 204, "the group is taken away again");

    step("account requests: someone asks at the sign-in page, an administrator approves, the new account sets its message channels");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.waitForSelector("#setup-reset.on"); // the server under test sends mail
    await page.waitForSelector("#setup-sso.off");
    // What is not set up says where it is set: the server's environment.
    await page.click("#setup-sso .info-button");
    await page.waitForSelector("#login-env-help[open] >> text=WWBB_SMTP_HOST");
    await Promise.all([page.waitForResponse((r) => r.url().endsWith("/api/settings/access") && r.request().method() === "PUT"), page.check("#requests-open")]);
    const guestCtx = await browser.newContext();
    const guest = await guestCtx.newPage();
    guest.on("pageerror", (e) => problems.push("guest page error: " + e.message));
    guest.on("dialog", async (d) => { problems.push("guest dialog: " + d.message()); await d.dismiss(); });
    await guest.goto(base + "/");
    await guest.click("#signup-open");
    await guest.fill("#signup [name=username]", "dana");
    await guest.fill("#signup [name=email]", "dana@example.org");
    await guest.fill("#signup [name=password]", "dana's password");
    await guest.fill("#signup [name=reason]", hostile);
    await guest.click("#signup button[type=submit]");
    await guest.waitForSelector("#signup-sent");
    await page.reload();
    await page.waitForSelector("#account-button.pending");
    await page.waitForSelector("#account-requests tr[data-username=dana]");
    check(await page.locator("#account-requests tr[data-username=dana] td").nth(2).textContent() === hostile, "the note of a request shows as text");
    // The request waits on the start page and is decided under the tasks.
    await page.goto(base + "/#/");
    await page.waitForSelector("#home-requests:has-text('1 account request')");
    await page.waitForSelector("#home-tasks .chip");
    await page.click("#home-requests");
    await page.waitForSelector("#tasks-requests tr[data-username=dana]");
    check(await page.locator("#tasks-requests tr[data-username=dana] td").nth(2).textContent() === hostile, "the note of a request shows as text among the tasks");
    await page.click("#tasks-requests tr[data-username=dana] button.approve");
    await page.waitForSelector("#tasks-requests", { state: "detached" });
    await page.waitForFunction(() => !document.querySelector("#account-button.pending"));
    await page.goto(base + "/#/admin");
    await page.waitForSelector("#no-requests");
    await page.waitForSelector("#users tr:has-text('dana'):has-text('dana@example.org')");
    await guest.click("#signup-sent ~ p button");
    await guest.fill("#login [name=username]", "dana");
    await guest.fill("#login [name=password]", "dana's password");
    await guest.click("#login button[type=submit]");
    await guest.waitForSelector("#account-button");
    await guest.goto(base + "/#/account");
    check(await guest.inputValue("#contacts [name=email]") === "dana@example.org", "the address of the request is on the account");
    await guest.fill("#contacts [name=slack]", "https://hooks.slack.com/services/T0/B0/x");
    await guest.click("#contacts button[type=submit]");
    await guest.waitForSelector("#channels:has-text('Mail address, Slack')");
    await guestCtx.close();
    if (process.env.SMTP_SINK_PORT) {
      check((await mailTo(sink, "dana@example.org")).includes("Your account dana was approved"), "dana is told of the approval");
      step("password reset: a link by mail sets a new password");
      const resetCtx = await browser.newContext();
      const reset = await resetCtx.newPage();
      reset.on("pageerror", (e) => problems.push("reset page error: " + e.message));
      sink.mails.length = 0;
      await reset.goto(base + "/");
      await reset.click("#forgot-open");
      await reset.fill("#forgot [name=username]", "dana");
      await reset.click("#forgot button[type=submit]");
      await reset.waitForSelector("#forgot-sent");
      const link = (await mailTo(sink, "dana@example.org")).match(/http\S*#\/reset\/[0-9a-f]+/)[0];
      await reset.goto(link);
      await reset.fill("#reset [name=password]", "dana's second password");
      await reset.fill("#reset [name=repeat]", "dana's second password");
      await reset.click("#reset button[type=submit]");
      await reset.waitForSelector("#reset-done");
      await reset.click("#reset-login");
      await reset.fill("#login [name=username]", "dana");
      await reset.fill("#login [name=password]", "dana's second password");
      await reset.click("#login button[type=submit]");
      await reset.waitForSelector("#account-button");
      await resetCtx.close();
    }

    step("audit trail: the hash chain is checked over the whole journal and holds (ADR-0123)");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.click("#audit-verify");
    await page.waitForSelector("#audit-chain .notice.success");
    check(/^[0-9a-f]{64}$/.test(await page.textContent("#audit-chain code")), "the newest entry's hash is shown");

    step("administration: groups, a group's role in a space, system roles");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.fill("#add-member [name=group]", "ops");
    await page.selectOption("#add-member [name=user]", { label: "bert" });
    await page.click("#add-member button[type=submit]");
    await page.waitForSelector("#groups th:text-is('@ops')");
    await page.click("#users tr:has-text('bert') label:has-text('erasure') input");
    await page.waitForSelector("#users tr:has-text('bert') label:has-text('erasure') input:checked");
    await page.click("#users tr:has-text('bert') label:has-text('erasure') input");
    await page.waitForSelector("#users tr:has-text('bert') label:has-text('erasure') input:not(:checked)");
    await page.click("#admin-spaces a:has-text('Settings')");
    await page.fill("#add-role [name=group]", "ops");
    await page.selectOption("#add-role [name=role]", "editor");
    await page.click("#add-role button[type=submit]");
    await page.waitForSelector("#members tr:has-text('@ops')");
    check(await page.inputValue("#members tr:has-text('@ops') select") === "editor", "the group holds the role granted");
    await page.setInputFiles("#space-icon-file", { name: "icon.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector("#space-icon img.space-icon[src*='/api/spaces/PLAT/icon']");
    // The settings rename the space; its key stays (ADR-0086).
    const spaceName = await page.inputValue("#space-name [name=name]");
    await page.fill("#space-name [name=name]", "Platform renamed");
    await page.click("#space-name button[type=submit]");
    await page.waitForSelector("h1:has-text('Settings of Platform renamed')");
    await page.fill("#space-name [name=name]", spaceName);
    await page.click("#space-name button[type=submit]");
    await page.waitForSelector(`h1:has-text('Settings of ${spaceName}')`);

    step("start page: the spaces as tiles with their icon, or as a list, as each reader chooses");
    await page.click("a.brand");
    await page.waitForSelector("#spaces-overview .space-tile:has-text('Platform') img.space-icon");
    await page.click("#spaces-list");
    await page.waitForSelector("#spaces-overview table.space-list a:has-text('Platform') img.space-icon");
    await page.reload();
    await page.waitForSelector("#spaces-list[aria-pressed=true]"); // the choice is kept
    await page.click("#spaces-tiles");
    await page.waitForSelector("#spaces-overview .space-tile");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.click("#groups button[aria-label^='Take bert out']");
    await page.waitForSelector("#admin-groups p:has-text('No groups yet.')");

    step("appearance: a logo, an accent colour and a name give the installation its corporate design");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.setInputFiles("#design-logo", { name: "logo.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector("header .brand img.brand-logo[src^='/branding/logo']");
    check(await page.locator("head link[rel=icon][href^='/branding/logo']").count() === 0, "the design's logo is not the browser's icon");
    check(await page.locator("head link[rel=icon][href^='/favicon.ico']").count() === 1, "the browser's icon stays the product's");
    // A logo for the dark look comes in by the scheme, and follows the
    // look chosen with the bulb (ADR-0109).
    await page.setInputFiles("#design-logo-dark", { name: "logo-dark.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector("header .brand picture source[srcset^='/branding/logo-dark']", { state: "attached" });
    await page.waitForSelector(".design-asset[data-asset=logo-dark] img.design-preview-dark");
    const darkLogoMedia = () => page.$eval("header .brand picture source", (s) => s.media);
    const lookWas = await page.evaluate(() => document.documentElement.dataset.theme);
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme !== was, lookWas);
    const darkNow = (await page.evaluate(() => document.documentElement.dataset.theme)) === "dark";
    check((await darkLogoMedia()) === (darkNow ? "all" : "not all"), "the logo for the dark look follows the chosen look");
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme === was, lookWas);
    await page.click(".design-asset[data-asset=logo-dark] button.danger");
    await page.waitForSelector("header .brand > img.brand-logo[src^='/branding/logo']");
    // An own picture on the start page (ADR-0095).
    await page.setInputFiles("#design-welcome", { name: "welcome.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector(".design-asset img[src^='/branding/welcome']");
    // A removal that does not reach the server, e.g. a proxy refusing
    // DELETE, says so beside the picture; one answered but not done too.
    expectedConsole = /status of 403/;
    await page.route("**/api/branding/welcome", (r) => r.request().method() === "DELETE" ? r.fulfill({ status: 403, contentType: "application/json", body: '{"error":"blocked by a proxy"}' }) : r.continue());
    await page.click(".design-asset[data-asset=welcome] button.danger");
    await page.waitForSelector(".design-asset[data-asset=welcome] .design-asset-status:has-text('blocked by a proxy')");
    await page.unroute("**/api/branding/welcome");
    await page.route("**/api/branding/welcome", (r) => r.request().method() === "DELETE" ? r.fulfill({ status: 204 }) : r.continue());
    await page.click(".design-asset[data-asset=welcome] button.danger");
    await page.waitForSelector(".design-asset[data-asset=welcome] .design-asset-status:has-text('still set')");
    await page.unroute("**/api/branding/welcome");
    expectedConsole = null;
    // And one for the dark look, shown when the look is dark (ADR-0109).
    await page.setInputFiles("#design-welcome-dark", { name: "welcome-dark.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector(".design-asset[data-asset=welcome-dark] img.design-preview-dark");
    await page.goto(base + "/#/");
    await page.waitForSelector("#welcome-art.welcome-own[src^='/branding/welcome']");
    const startLook = await page.evaluate(() => document.documentElement.dataset.theme);
    const startArt = () => page.$eval("#welcome-art", (i) => new URL(i.src).pathname);
    check((await startArt()) === (startLook === "dark" ? "/branding/welcome-dark" : "/branding/welcome"), "the start picture fits the look");
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme !== was, startLook);
    check((await startArt()) === (startLook === "dark" ? "/branding/welcome" : "/branding/welcome-dark"), "the start picture follows the chosen look");
    await page.click("#theme-toggle");
    await page.waitForFunction((was) => document.documentElement.dataset.theme === was, startLook);
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/admin']");
    await page.click(".design-asset[data-asset=welcome-dark] button.danger");
    await page.waitForSelector(".design-asset[data-asset=welcome-dark]:not(:has(img))");
    await page.fill("#design [name=name]", "ACME");
    await page.check("#design [name=accent-on]");
    await page.fill("#design [name=accent]", "#d40000");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design .notice");
    await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() === "#d40000");
    // The title follows once the design is fetched again, which may be after the stylesheet.
    await page.waitForFunction(() => document.title === "ACME"); // the instance's name, nothing else (ADR-0097)
    check(await page.locator("#design-contrast li").count() === 0, "a readable design brings no contrast warning");
    // In the dark scheme the corporate red is lightened until it reads on the
    // dark cards, and its buttons carry dark text (ADR-0052).
    await page.emulateMedia({ colorScheme: "dark" });
    const darkAccent = await page.evaluate(() => { const s = getComputedStyle(document.documentElement); return [s.getPropertyValue("--accent").trim(), s.getPropertyValue("--accent-ink").trim()]; });
    check(darkAccent[0] === "#e35959" && darkAccent[1] === "#0b0d12", "the dark scheme's accent and its text: " + darkAccent);
    await page.emulateMedia({ colorScheme: "light" });
    // An own stylesheet without a dark look turns the dark scheme off for everyone (ADR-0070).
    await page.fill("#design [name=css]", ".brand { letter-spacing: .5px; }");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design-dark:has-text('dark scheme is off')");
    await page.waitForSelector("#theme-toggle", { state: "hidden" });
    await page.emulateMedia({ colorScheme: "dark" });
    await page.waitForFunction(() => getComputedStyle(document.documentElement).getPropertyValue("--bg").trim() === "#f5f6f8");
    await page.emulateMedia({ colorScheme: "light" });
    await page.fill("#design [name=css]", "");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design-dark:has-text('stays on')");
    // Yellow is unreadable as link text on white: saved, with a warning.
    await page.fill("#design [name=accent]", "#ffcc00");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design-contrast li:has-text('link text on white cards')");
    await page.fill("#design [name=accent]", "#d40000");
    await page.click("#design button[type=submit]");
    await page.waitForFunction(() => document.querySelectorAll("#design-contrast li").length === 0);
    await page.click("#admin-design button:has-text('Delete')"); // the product's logo comes back
    await page.waitForSelector("header .brand picture");
    await page.waitForSelector("head link[rel=icon][href^='/favicon.ico']", { state: "attached" });
    // A ready-made theme applies under the design; the comic one has no
    // dark scheme (ADR-0085).
    // The Swiss theme says whom it is for (ADR-0120).
    check((await page.textContent("#design-themes label.theme-swiss .theme-notice")).includes("Swiss Confederation"), "the Swiss theme names whom it is for");
    await page.click("#design-themes label.theme-comic");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design-dark:has-text('The dark scheme is off')");
    check((await (await page.request.get(base + "/branding.css")).text()).includes("/* The theme comic"), "the theme is in the design's stylesheet");
    check((await (await page.request.get(base + "/branding.json")).json()).theme === "comic", "the theme is kept");
    await page.click("#design-themes label.theme-none");
    await page.click("#design button[type=submit]");
    await page.waitForSelector("#design-dark:has-text('stays on')");

    step("AI access: an administrator registers Claude and gets the three values for its connector");
    await page.click("#app-switcher");
    await page.click("#drawer-apps a:has-text('AI access')");
    await page.waitForSelector(".tools li:has-text('create_item')");
    check(await page.inputValue("#aiaccess-register [name=redirect]") === "https://claude.ai/api/mcp/auth_callback", "Claude's redirect address is filled in");
    await page.click("#aiaccess-register button[type=submit]");
    await page.waitForSelector("#aiaccess-client-secret");
    const clientId = (await page.textContent("#aiaccess-client-id")).trim();
    const clientSecret = (await page.textContent("#aiaccess-client-secret")).trim();
    check(/^wwbbc_/.test(clientSecret) && (await page.textContent("#aiaccess-url")).trim().endsWith("/mcp"), "the secret and the MCP address are shown");
    await page.waitForSelector(`#aiaccess-apps td:has-text('${clientId}')`);

    step("AI access: the person approves Claude on the consent page, and Claude's token works on /mcp only");
    const verifier = "e2e-verifier-0123456789-abcdefghijklmnopqrstuvwxyz";
    const challenge = await page.evaluate(async (v) => {
      const d = new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(v)));
      return btoa(String.fromCharCode(...d)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    }, verifier);
    // The callback is Claude's; here it only records where it was sent.
    let callback = null;
    await page.route("https://claude.ai/**", (r) => { callback = r.request().url(); r.fulfill({ status: 200, contentType: "text/html", body: "<p>back at the assistant</p>" }); });
    const authorize = new URLSearchParams({ response_type: "code", client_id: clientId, redirect_uri: "https://claude.ai/api/mcp/auth_callback",
      state: "e2e-state", code_challenge: challenge, code_challenge_method: "S256", resource: base + "/mcp" });
    await page.goto(base + "/oauth/authorize?" + authorize);
    await page.waitForSelector("#oauth-consent h1:has-text('Allow access?')");
    check((await page.textContent("#oauth-consent")).includes("Claude wants to act as you"), "the consent page names the application");
    check(!(await page.$("#oauth-self")), "an application added by an administrator is not marked self-registered");
    check(await page.isChecked("#oauth-access input[value=read]"), "reading only is preselected");
    await page.check("#oauth-access input[value=full]");
    await page.click("#oauth-allow");
    await page.waitForURL("https://claude.ai/**");
    const back = new URL(callback);
    check(back.searchParams.get("state") === "e2e-state" && back.searchParams.get("code"), "the code comes back with the state: " + callback);
    await page.unroute("https://claude.ai/**");
    await page.goto(base + "/#/");
    const oauth = await page.evaluate(async ({ code, id, secret, verifier, base }) => {
      const tok = await (await fetch("/oauth/token", { method: "POST", credentials: "omit", headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: "https://claude.ai/api/mcp/auth_callback",
          code_verifier: verifier, client_id: id, client_secret: secret, resource: base + "/mcp" }) })).json();
      const mcp = await fetch("/mcp", { method: "POST", credentials: "omit", headers: { "Content-Type": "application/json", Authorization: "Bearer " + tok.access_token },
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "list_spaces", arguments: {} } }) });
      const listed = (await mcp.json()).result;
      const api = await fetch("/api/me", { credentials: "omit", headers: { Authorization: "Bearer " + tok.access_token } });
      const anon = await fetch("/mcp", { method: "POST", credentials: "omit", headers: { "Content-Type": "application/json" }, body: "{}" });
      return { prefix: (tok.access_token || "").slice(0, 6), mcp: mcp.status, spaces: listed && !listed.isError && listed.content[0].text.includes("PLAT"), api: api.status, challenge: anon.headers.get("WWW-Authenticate") };
    }, { code: back.searchParams.get("code"), id: clientId, secret: clientSecret, verifier, base });
    check(oauth.prefix === "wwbba_" && oauth.mcp === 200 && oauth.spaces && oauth.api === 401, "the OAuth token reaches /mcp and nothing else: " + JSON.stringify(oauth));
    check((oauth.challenge || "").includes("resource_metadata="), "an anonymous MCP call points to the OAuth metadata: " + oauth.challenge);

    step("AI access: the person withdraws the approval; local clients still use an API token");
    await page.goto(base + "/#/mcp");
    await page.waitForSelector("#aiaccess-grants td:has-text('read and write')");
    await page.click("#aiaccess-grants button:has-text('Withdraw')");
    await page.waitForSelector("#aiaccess-grants p:has-text('not approved any application')");
    await page.click("#aiaccess-token summary");
    await page.click("#mcp-token button[type=submit]");
    await page.waitForSelector("#mcp-setup pre:has-text('Bearer wwbb_')");
    const mcpToken = (await page.textContent("#mcp-setup pre")).match(/Bearer (wwbb_\S+?)"/)[1];
    const made = await page.evaluate(async (tok) => {
      const call = async (body) => (await fetch("/mcp", { method: "POST", credentials: "omit",
        headers: { "Content-Type": "application/json", Authorization: "Bearer " + tok }, body: JSON.stringify(body) })).json();
      await call({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "e2e", version: "1" } } });
      const res = await call({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_item", arguments: {
        space: "PLAT", type: "task", title: "Written by an assistant", properties: { priority: "low" },
        body: [{ kind: "heading", level: 1, text: "Summary" }, { kind: "paragraph", text: "Made through MCP." }] } } });
      return JSON.parse(res.result.content[0].text).item.key;
    }, mcpToken);
    await page.goto(base + "/#/item/" + made);
    await page.waitForSelector("#body .ProseMirror:has-text('Made through MCP.')");

    step("account: token");
    await page.click("#account-button");
    await page.click("#account-menu a[href='#/account']");
    await page.fill("#new-token [name=name]", "ci");
    await page.click("#new-token button[type=submit]");
    await page.waitForSelector("text=Copy this token now");
    check(/wwbb_[0-9a-f]{64}/.test(await page.textContent("code")), "the token secret is shown once");

    step("account: a profile picture replaces the initials in the header, and goes again");
    check((await page.getAttribute("#account-button .avatar", "data-initials")) === "A", "without a picture the header shows the initials");
    await page.setInputFiles("#avatar-file", { name: "me.png", mimeType: "image/png",
      buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64") });
    await page.waitForSelector("#account-button .avatar img");
    await page.goto(base + "/#/item/PLAT-1"); // the picture also shows beside one's comments and changes
    await page.waitForSelector(".comment .comment-head .avatar img");
    await page.waitForSelector("table.history .who .avatar img");
    await page.goto(base + "/#/account");
    await page.click("#avatar button:has-text('Remove the picture')");
    await page.waitForSelector("#account-button .avatar:not(:has(img))");

    step("language: German on request, kept across sign-ins");
    await page.selectOption("#language [name=language]", "de");
    await page.waitForSelector("#nav-notifications[aria-label^='Benachrichtigungen']");
    check(await page.getAttribute("html", "lang") === "de", "the document is marked German");
    check((await page.textContent("#password h2")) === "Passwort ändern", "the account page is German");
    // A German browser does not override bert's own earlier choice (English,
    // taken from his browser at his first sign-in).
    const german = await browser.newContext({ locale: "de-CH" });
    const bertDe = await german.newPage();
    bertDe.on("pageerror", (e) => problems.push("bert's German page: " + e.message));
    await bertDe.goto(base + "/");
    await bertDe.waitForSelector("#login >> text=Anmelden"); // signed out: the browser's language
    await bertDe.fill("#login [name=username]", "bert");
    await bertDe.fill("#login [name=password]", "bert's password");
    await bertDe.click("#login button[type=submit]");
    await bertDe.waitForSelector("#nav-notifications[aria-label^='Notifications']");
    await german.close();
    await page.selectOption("#language [name=language]", "en");
    await page.waitForSelector("#nav-notifications[aria-label^='Notifications']");

    step("copy and move: a page is copied with its child pages, then moved below another page");
    await page.goto(base + "/#/space/PLAT");
    await page.waitForSelector("#space-new-page");
    check(await page.$eval(".page-head .actions", (p) => p.firstElementChild.id === "space-new-page" && p.lastElementChild.id === "watch"),
      "a space offers a new page first and the bell last");
    await page.click(".tree a:has-text('Runbook')");
    await page.waitForSelector(".tree a.current:has-text('Runbook')");
    await page.click("#item-copy");
    await page.waitForSelector("#relocate-form #relocate-children:checked");
    await page.selectOption("#relocate-parent", "");
    await page.fill("#relocate-title", "Runbook template");
    await page.click("#relocate-form button[type=submit]");
    await page.waitForSelector("h1.item-title:text-is('Runbook template')");
    await page.waitForSelector(".tree a.current:has-text('Runbook template')");
    await page.waitForSelector(".tree li:has(> .tree-row a.current) > ul a:text-is('Install')"); // its child pages came along
    await page.click("#item-move");
    await page.selectOption("#relocate-parent", { label: (await page.locator("#relocate-parent option:has-text('Tidy the wiki')").textContent()).trim() });
    await page.click("#relocate-form button[type=submit]");
    await page.waitForSelector("p:has(a[href='#/']) >> text=PLAT"); // the page shown again after moving
    await page.waitForFunction(() => document.querySelector(".tree a.current")?.closest("ul")?.closest("li")?.querySelector(".tree-row a")?.textContent === "Tidy the wiki");

    step("export: a page as Word and its table as CSV; printing hides the surroundings; lists as CSV");
    const exported = await page.evaluate(async () => {
      const res = await fetch("/api/items", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({
        space: "PLAT", type: "task", title: "Price list", properties: { priority: "low" },
        body: [{ kind: "heading", level: 1, text: "Prices" }, { kind: "table", text: "Item\tPrice\nTea\t3" }] }) });
      return (await res.json()).item.key;
    });
    await page.goto(base + "/#/item/" + exported);
    await page.waitForSelector("h1.item-title:text-is('Price list')"); // not the menu of the page shown before
    await page.click("#item-export summary");
    await page.waitForSelector("#item-export a.export-table:has-text('Item, Price')");
    const [docx] = await Promise.all([page.waitForEvent("download"), page.click("#export-docx")]);
    check(docx.suggestedFilename() === exported + " Price list.docx", "the Word file is named after the page: " + docx.suggestedFilename());
    await page.click("#item-export summary"); // a download closes the menu
    const [table] = await Promise.all([page.waitForEvent("download"), page.click("#item-export a.export-table")]);
    const tableText = require("fs").readFileSync(await table.path(), "utf8");
    check(tableText === "\ufeffItem,Price\r\nTea,3\r\n", "the table as CSV: " + JSON.stringify(tableText));
    await page.emulateMedia({ media: "print" });
    check(!(await page.isVisible("header.top")) && !(await page.isVisible("#item-export")) && await page.isVisible("h1.item-title"), "printing shows the page without the surroundings");
    await page.emulateMedia({ media: "screen" });
    // A page with subpages exports as a handbook, with them and what they embed.
    await page.goto(base + "/#/space/PLAT");
    await page.click(".tree a:text-is('Tidy the wiki')");
    await page.waitForSelector("h1.item-title:text-is('Tidy the wiki')");
    await page.click("#item-export summary");
    await page.waitForSelector("#export-docx-handbook");
    const [book] = await Promise.all([page.waitForEvent("download"), page.click("#export-docx-handbook")]);
    check(book.suggestedFilename().endsWith("Tidy the wiki.docx"), "the handbook is named after its root page: " + book.suggestedFilename());
    await page.click("#item-export summary");
    // Each print the frame asks for is noted instead of opening the dialog.
    await page.evaluate(() => {
      window.printed = [];
      const own = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, "contentWindow");
      Object.defineProperty(HTMLIFrameElement.prototype, "contentWindow", { configurable: true, get() {
        const w = own.get.call(this);
        if (w) w.print = () => window.printed.push(w.location.href);
        return w;
      } });
    });
    await page.click("#export-pdf-handbook");
    const frame = await (await page.waitForSelector("#export-frame")).contentFrame();
    await frame.waitForSelector("h2.page-title:text-is('Runbook template')");
    check(await frame.isVisible("nav.contents"), "the printable handbook lists its pages");
    await page.waitForFunction(() => window.printed.length > 0);
    await page.waitForTimeout(300);
    const printed = await page.evaluate(() => window.printed);
    check(printed.length === 1 && printed[0].includes("/export/html?children=true"), "the handbook is printed once, with its content: " + JSON.stringify(printed));
    // The search without a term asks for one instead of failing; the start
    // page has no search button of its own, the top bar searches.
    await page.goto(base + "/#/");
    await page.waitForSelector("#welcome");
    check(await page.locator("#welcome a[href='#/search/']").count() === 0, "the start page has no search button");
    // A ready-made type defined before it gained a field gets it from the
    // start page: a decision defined without an assignee.
    check((await page.request.post(base + "/api/types", { data: { name: "decision", body: true, fields: [{ name: "decided", kind: "date" }],
      workflow: { initial: "proposed", states: ["proposed", "accepted"], transitions: [{ name: "accept", from: ["proposed"], to: "accepted" }] } } })).status() === 201, "an old decision type");
    await page.reload();
    await page.waitForSelector("#setup-types :text('decision (assignee)')");
    await page.click("#setup-fields");
    await page.waitForSelector("#setup-types :text('decision (assignee)')", { state: "detached" });
    const decision = (await (await page.request.get(base + "/api/types")).json()).types.find((ty) => ty.name === "decision");
    check(decision.fields.map((f) => f.name).join(",") === "decided,assignee" && decision.workflow.states.length === 2, "the decision gained its assignee and kept the rest");
    // The start page counts what one may read: pages, tasks, pictures.
    await page.waitForSelector("#home-stats #home-pages strong");
    await page.waitForSelector("#home-stats a#home-task-count[href='#/tasks/all'] strong");
    await page.waitForSelector("#home-stats a#home-album-count[href='#/album'] strong");
    check(Number(await page.textContent("#home-task-count strong")) > 0, "tasks are counted");
    await page.goto(base + "/#/search/");
    await page.waitForSelector("#search-page input:focus");
    await page.waitForSelector("#search-hint");
    check(await page.locator(".error").count() === 0, "an empty search is no error");
    await page.fill("#search-page input", "Price");
    await page.click("#search-page button[type=submit]");
    await page.waitForSelector(".hits li:has-text('Price list')");
    await page.goto(base + "/#/search/%2A%2A");
    await page.waitForSelector("#search-hint:has-text('at least one letter')");
    await page.goto(base + "/#/search/Price");
    await page.waitForSelector(".hits li:has-text('Price list') .stars[aria-label='5 of 5 stars']"); // every word whole in the title (ADR-0083)
    // A hit holds any of the words; it names those it holds and those it lacks.
    await page.goto(base + "/#/search/" + encodeURIComponent("Price zebraquark"));
    await page.waitForSelector(".hits li:has-text('Price list') .hit-words .hit-matched:has-text('Price')");
    await page.waitForSelector(".hits li:has-text('Price list') .hit-words s.hit-missing:has-text('zebraquark')");
    const [found] = await Promise.all([page.waitForEvent("download"), page.click("#search-csv")]);
    check(require("fs").readFileSync(await found.path(), "utf8").includes(exported + ",Price list,PLAT,task"), "the search as CSV");
    await page.goto(base + "/#/tasks/all");
    const [tasks] = await Promise.all([page.waitForEvent("download"), page.click("#tasks-csv")]);
    const tasksText = require("fs").readFileSync(await tasks.path(), "utf8");
    check(tasksText.includes(exported + ",Price list"), "the task list as CSV: " + JSON.stringify(tasksText.slice(0, 300)));

    step("connectors: opendata.swiss as a connector; a dry run reports, a run mirrors the source's API into a catalog space, linked to its data set (ADR-0075, ADR-0077)");
    check((await page.request.post(base + "/api/spaces", { data: { key: "APIS", name: "Interfaces" } })).status() === 201, "the catalog's space is created");
    await page.goto(base + "/#/admin");
    await page.waitForSelector("#connectors >> text=No connector yet.");
    await page.click("#connector-new");
    await page.waitForSelector("#connector-settings");
    check(await page.inputValue("#connector-settings [name=id]") === "opendata.swiss", "a new connector starts from the template of opendata.swiss");
    await page.selectOption("#connector-settings [name=space]", "APIS");
    await page.click("#connector-settings details summary");
    await page.fill("#connector-settings [name=baseURL]", `http://127.0.0.1:${ckan.address().port}/api/3/action`);
    // The source's formats are listed to choose from; its own spelling,
    // here "api", is matched.
    await page.click("#connector-source-formats");
    await page.waitForSelector("#connector-formats label:has-text('api') input:checked");
    await page.click("#connector-formats label:has-text('CSV') input");
    check((await page.inputValue("#connector-settings [name=formats]")).split(", ").includes("CSV"), "ticking a format adds it to the list");
    await page.click("#connector-formats label:has-text('CSV') input");
    await page.click("#connector-settings button[type=submit]");
    await page.waitForSelector("#connector-list tr[data-connector='opendata.swiss']");
    await page.waitForSelector("#connector-credential >> text=WWBB_CONNECTOR_OPENDATA_SWISS_TOKEN");
    check(await page.getAttribute("#connector-settings [name=id]", "readonly") !== null, "a saved connector keeps its ID");
    await page.click("#connector-dry-run");
    await page.waitForSelector("#connector-history .catalog-run:has-text('Dry run'):has-text('1 new')");
    await page.click("#connector-run-now");
    await page.waitForSelector("#connector-history .catalog-run:has-text('Sync'):has-text('1 new') a");
    await page.waitForSelector("#connector-list tr[data-connector='opendata.swiss'] >> text=1 page");
    await page.click("#connector-history .catalog-run a");
    await page.waitForSelector("#item-origin a[href='https://opendata.swiss/de/dataset/luftqualitaet']");
    // One ordinary page per data set, written from what the source says
    // (ADR-0080, ADR-0093): publisher and contact are in its text.
    await page.waitForSelector(".item-main :text('Bundesamt für Umwelt')");
    await page.waitForSelector(".item-main :text('luft@example.org')");
    check(await page.$("tr[data-field=publisher]") === null, "the page has no fields of its own");
    await page.waitForSelector(".item-main :text('Schnittstellen')");
    await page.waitForSelector(".item-main a[href='https://air.example.org/v1']");
    await page.waitForSelector(".item-main a[href='https://air.example.org/data.csv']");
    // A catalog page is no task.
    await page.goto(base + "/#/tasks/all");
    await page.waitForSelector("#tasks");
    check(await page.locator("#tasks tr:has-text('Luftqualität')").count() === 0, "a catalog entry is not in the task list");

    step("connectors: a JSON source in the feed format fills items of a type of one's own, its values mapped to the type's fields (ADR-0127)");
    check((await page.request.post(base + "/api/types", { data: { name: "service", body: true,
      fields: [{ name: "owner", kind: "text", required: true }, { name: "since", kind: "date" }] } })).status() === 201, "a type of one's own");
    await page.goto(base + "/#/admin");
    await page.click("#connector-new-json");
    await page.waitForSelector("#connector-settings [name=url]");
    await page.fill("#connector-settings [name=id]", "registry");
    await page.fill("#connector-settings [name=name]", "Registry");
    await page.selectOption("#connector-settings [name=space]", "APIS");
    await page.fill("#connector-settings [name=url]", `http://127.0.0.1:${feed.address().port}/entries`);
    check(await page.inputValue("#connector-settings [name=items]") === "entries", "the paths read the feed format by default");
    await page.fill("#connector-settings [name=values]", "owner = owner\nsince = since");
    await page.dispatchEvent("#connector-settings [name=values]", "change");
    await page.selectOption("#connector-type", "service");
    await page.waitForSelector("#connector-fields select[data-field=owner] option[value=owner]", { state: "attached" });
    await page.selectOption("#connector-fields select[data-field=owner]", "owner");
    await page.selectOption("#connector-fields select[data-field=since]", "since");
    await page.click("#connector-settings button[type=submit]");
    await page.waitForSelector("#connector-list tr[data-connector='registry']");
    check(await page.inputValue("#connector-type") === "service", "the type is kept");
    check(await page.inputValue("#connector-fields select[data-field=owner]") === "owner", "the mapping is kept");
    await page.click("#connector-run-now");
    await page.waitForSelector("#connector-history .catalog-run:has-text('Sync'):has-text('1 new') a");
    await page.click("#connector-history .catalog-run a");
    await page.waitForSelector("#item-origin a[href='https://registry.example.org/svc-1']");
    await page.waitForSelector("tr[data-field=owner] input");
    check(await page.inputValue("tr[data-field=owner] input") === "Amt für Geoinformation", "the text field is filled from the source");
    check(await page.inputValue("tr[data-field=since] input") === "2024-03-01", "the date field is filled from the source");
    await page.waitForSelector(".item-main :text('Liefert die Adressen des Kantons.')");

    step("spaces: an administrator deletes a space into the trash, restores it, and deletes it for good");
    await page.goto(base + "/#/admin");
    await page.fill("#new-space [name=key]", "TMP");
    await page.fill("#new-space [name=name]", "Temporary");
    await page.click("#new-space button[type=submit]");
    await page.waitForSelector("button[data-trash-space=TMP]");
    answer = true;
    await page.click("button[data-trash-space=TMP]");
    await page.waitForSelector("#space-trash td:text-is('TMP')");
    check(!(await page.$("button[data-trash-space=TMP]")), "a space in the trash leaves the list of spaces");
    await page.click("#space-trash button:has-text('Restore')");
    await page.waitForSelector("button[data-trash-space=TMP]");
    answer = true;
    await page.click("button[data-trash-space=TMP]");
    await page.waitForSelector("#space-trash td:text-is('TMP')");
    answer = "TMP";
    await page.click("#space-trash button:has-text('Delete for good')");
    await page.waitForFunction(() => !document.querySelector("#space-trash") && !document.querySelector("button[data-trash-space=TMP]"));

    step("filters: a paged list is filtered by the server (ADR-0098)");
    await page.goto(base + "/#/notifications?notifications.kind=mentions");
    await page.waitForSelector("#notifications-filterbar select[data-choice=kind]");
    check(await page.inputValue("#notifications-filterbar select[data-choice=kind]") === "mentions", "the filter comes back from the address");
    const mentionsShown = await page.locator("#notifications li").count();
    await page.selectOption("#notifications-filterbar select[data-choice=kind]", "");
    await page.waitForFunction(() => !location.hash.includes("notifications.kind"));
    await page.waitForFunction((n) => document.querySelectorAll("#notifications li").length >= n, mentionsShown);

    step("tasks in a text: a new task below the item, as a link and shown whole; a list of the tasks of a label, sorted and filtered (ADR-0114)");
    const plan = await page.evaluate(async () => (await (await fetch("/api/items", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ space: "PLAT", type: "task", title: "Release plan", properties: { priority: "low" } }) })).json()).item);
    await page.goto(base + "/#/item/" + plan.key + "?edit");
    await page.waitForSelector(".ProseMirror[contenteditable=true]");
    await page.click(".ProseMirror");
    const newTask = async (title, display) => {
      await page.selectOption(".pm-toolbar select[aria-label='Insert…']", "newtask");
      await page.fill("#newtask-dialog [name=title]", title);
      if (await page.locator("#newtask-dialog [name=type]").count()) await page.selectOption("#newtask-dialog [name=type]", "task");
      await page.selectOption("#newtask-dialog [name='p:priority']", "high");
      await page.check(`#newtask-dialog [name=display][value=${display}]`);
      await page.click("#newtask-dialog-ok");
      await page.waitForSelector("#newtask-dialog", { state: "detached" });
    };
    await newTask("Write the release notes", "full");
    await page.waitForSelector(".ProseMirror .pm-embed .pm-embed-title:has-text('Write the release notes')");
    await newTask("Tag the build", "link");
    await page.waitForSelector(".ProseMirror .pm-mention:has-text('Tag the build')");
    const below = await page.evaluate(async (k) => (await (await fetch(`/api/spaces/PLAT/tree?parent=${k}`)).json()).items.map((n) => n.key), plan.key);
    check(below.length === 2, "both tasks are made below the item: " + below.join(", "));
    for (const key of below) {
      await page.evaluate(async (k) => fetch(`/api/items/${k}/labels`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ labels: ["release-2"] }) }), key);
    }
    await page.selectOption(".pm-toolbar select[aria-label='Insert…']", "tasklist");
    await page.fill("#tasklist-dialog [name=label]", "Release 2");
    await page.click("#tasklist-dialog-ok");
    await page.waitForSelector(".ProseMirror section.pm-tasklist[data-label='release-2'] tbody tr >> nth=1");
    await page.click("#details h2");
    await page.waitForSelector(".live[data-pending='0']");
    await page.goto(base + "/#/item/" + plan.key);
    const rows = "#body section.pm-tasklist tbody tr";
    await page.waitForSelector(rows + " >> nth=1");
    // Each click redraws the list; the check waits for the order it asks for.
    await page.click("#body section.pm-tasklist th button[data-sort=title]");
    await page.waitForSelector("#body section.pm-tasklist th[aria-sort=ascending]:has(button[data-sort=title])");
    check((await page.locator(rows + " td:nth-child(2)").allTextContents()).join("|") === "Tag the build|Write the release notes", "sorted by title");
    await page.click("#body section.pm-tasklist th button[data-sort=title]");
    await page.waitForSelector("#body section.pm-tasklist th[aria-sort=descending]:has(button[data-sort=title])");
    check((await page.locator(rows + " td:nth-child(2)").first().textContent()) === "Write the release notes", "and the other way round");
    await page.fill("#body section.pm-tasklist .filter-text", "notes");
    await page.waitForFunction((sel) => [...document.querySelectorAll(sel)].filter((r) => !r.hidden).length === 1, rows);

    step("phones: no view is wider than the screen; the drawing editor fills it and closes again (ADR-0116)");
    const phoneCtx = await browser.newContext({ viewport: { width: 375, height: 700 }, isMobile: true, hasTouch: true });
    const phone = await phoneCtx.newPage();
    phone.on("pageerror", (e) => problems.push("phone page error: " + e.message));
    await phone.goto(base + "/");
    await phone.fill("#login [name=username]", "admin");
    await phone.fill("#login [name=password]", adminPassword);
    await phone.press("#login [name=password]", "Enter");
    await phone.waitForSelector("#account-button");
    for (const view of ["#/", "#/space/PLAT", "#/item/PLAT-2", "#/item/" + plan.key, "#/item/" + plan.key + "?edit", "#/tasks/all", "#/kanbans", "#/governance", "#/governance/PLAT", "#/mcp", "#/admin", "#/search/runbook"]) {
      await phone.goto(base + "/" + view);
      await phone.waitForSelector("main h1, main .welcome, #body");
      await phone.waitForTimeout(400);
      const wide = await phone.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      check(wide <= 0, `${view} is ${wide}px wider than a phone's screen`);
    }
    await phone.goto(base + "/#/item/" + plan.key + "?edit");
    await phone.waitForSelector(".ProseMirror[contenteditable=true]");
    await phone.click(".ProseMirror");
    await phone.selectOption(".pm-toolbar select[aria-label='Insert…']", "drawing");
    await phone.waitForSelector("dialog.drawing[open]");
    const cancel = await phone.locator("dialog.drawing button:has-text('Cancel')").boundingBox();
    check(cancel && cancel.y >= 0 && cancel.y + cancel.height <= 700, "the drawing editor's Cancel is in sight on a phone");
    await phone.click("dialog.drawing button:has-text('Cancel')");
    await phone.waitForSelector("dialog.drawing[open]", { state: "detached" });
    await phoneCtx.close();

    step("log out, log in as bert, read the item");
    await page.click("#account-button");
    await page.click("#logout");
    await page.waitForSelector("#login");
    await page.fill("#login [name=username]", "bert");
    await page.fill("#login [name=password]", "bert's password");
    await page.click("#login button[type=submit]");
    await page.click("a:has-text('Platform')");
    await page.click("a:has-text('PLAT-1')");
    await page.waitForSelector("text=state: done");
    check(await page.$("text=Administration") === null, "bert sees no administration");
    check((await page.textContent(".comment-text")) === "Patched on the staging hosts.", "bert reads the comment");
    check((await page.textContent(".comment .muted")).startsWith("admin · "), "the comment names its author");
    check(await page.$(".comment button:has-text('Edit')") === null, "bert cannot edit admin's comment");
    await page.goto(base + "/#/comments/mentions");
    await page.waitForSelector("#comment-feed li:has-text('@bert please verify') .why-mentions");
    await page.goto(base + "/#/item/PLAT-1");
    check(!(await page.textContent(".comment")).includes("null"), "another's comment shows no stray text");

    step("watching: bert watches the item, admin renames it and mentions bert in a comment, bert is told (ADR-0084)");
    await page.click("#watch[aria-pressed='false'][aria-label='Watch']");
    await page.waitForSelector("#watch[aria-pressed='true'][aria-label='Stop watching']");
    const adminCtx = await browser.newContext();
    const admin = await adminCtx.newPage();
    await admin.goto(base + "/");
    await admin.fill("#login [name=username]", "admin");
    await admin.fill("#login [name=password]", adminPassword);
    await admin.click("#login button[type=submit]");
    await admin.waitForSelector("#nav a");
    const renamed = await admin.evaluate(async () => (await fetch("/api/items/PLAT-1/title", {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title: "Renamed by admin" }),
    })).status);
    check(renamed === 200, "admin renames the item: " + renamed);
    // A comment names bert: typing "@be" suggests him, and he is told.
    await admin.goto(base + "/#/item/PLAT-1");
    await admin.locator("#add-comment textarea").pressSequentially("Over to @be");
    await admin.click("#add-comment .mention-list button:text-is('@bert')");
    check(await admin.inputValue("#add-comment textarea") === "Over to @bert ", "the suggestion completes the name");
    await admin.locator("#add-comment textarea").pressSequentially("for a check");
    await admin.click("#add-comment button[type=submit]");
    await admin.waitForSelector(".comment-text:has-text('Over to @bert for a check')");
    await adminCtx.close();
    await page.reload();
    await page.waitForSelector("#nav-notifications[aria-label='Notifications (2)'] .count:text-is('2')");
    await page.click("#nav-notifications");
    await page.waitForSelector("#notifications li.unread:has-text('admin mentioned you in a comment on PLAT-1')");
    await page.waitForSelector("#notifications li.unread:has-text('admin renamed PLAT-1 Renamed by admin')");
    await page.waitForSelector("#nav-notifications[aria-label='Notifications'] .count[hidden]", { state: "attached" }); // seen now
    await page.waitForSelector("#watches a:has-text('PLAT-1')");

    step("protected passages: admin protects a name; bert reads it veiled, may not change the text, asks; admin grants; bert reads it (ADR-0088)");
    const bossCtx = await browser.newContext();
    const boss = await bossCtx.newPage();
    boss.on("pageerror", (e) => problems.push("admin's page error: " + e.message));
    await boss.goto(base + "/");
    await boss.fill("#login [name=username]", "admin");
    await boss.fill("#login [name=password]", adminPassword);
    await boss.click("#login button[type=submit]");
    await boss.waitForSelector("#nav a");
    const salaries = await boss.evaluate(async () => (await (await fetch("/api/items", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ space: "PLAT", type: "ticket", title: "Salaries", properties: { priority: "low" }, body: [{ kind: "paragraph", text: "The salary of Paul Muster is 9000." }] }) })).json()).item.key);
    await boss.goto(base + "/#/item/" + salaries);
    await boss.waitForSelector("#body .ProseMirror p");
    await boss.evaluate(() => {
      const text = document.querySelector("#body .ProseMirror p").firstChild;
      const at = text.textContent.indexOf("Paul Muster");
      const range = document.createRange();
      range.setStart(text, at);
      range.setEnd(text, at + "Paul Muster".length);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    });
    await boss.click("#protect-selection");
    await boss.waitForSelector(".protect-passage blockquote:text-is('Paul Muster')");
    await boss.click("#protect-save");
    await boss.waitForSelector("#body .pm-secret.secret-readable:text-is('Paul Muster')");
    // Editing the text keeps the passage.
    await boss.goto(base + "/#/item/" + salaries + "?edit");
    await boss.waitForSelector("#body .ProseMirror[contenteditable=true] .pm-secret");
    await boss.click("#body .ProseMirror p");
    await boss.keyboard.press("End");
    await boss.keyboard.type(" Due Friday.");
    await boss.click("#edit-done");
    await boss.waitForSelector("#body.reading .ProseMirror p:has-text('Due Friday.')");
    await boss.waitForSelector("#body .pm-secret.secret-readable:text-is('Paul Muster')");
    // Bert reads it veiled, cannot change the text, and asks.
    await page.goto(base + "/#/item/" + salaries);
    await page.waitForSelector("#secrets .secret-request");
    const veiledText = await page.textContent("#body .ProseMirror");
    check(!veiledText.includes("Paul") && veiledText.includes("▒") && veiledText.includes("Due Friday."), "bert reads the passage veiled: " + veiledText);
    check(await page.$("#edit") === null && await page.$("#protect-selection") === null, "bert cannot change the text");
    await page.click("#body .pm-secret.secret-veiled");
    await page.click("#secrets .secret-request");
    await page.waitForSelector("#secrets .notice");
    // Admin is told, and grants it.
    await boss.goto(base + "/#/notifications");
    await boss.waitForSelector("#notifications li:has-text('bert asked to read a protected passage of " + salaries + "')");
    await boss.goto(base + "/#/item/" + salaries);
    await boss.click("#secrets .secret-ask[data-principal] .secret-grant");
    await boss.waitForSelector("#secrets .secret-grant", { state: "detached" });
    await page.reload();
    await page.waitForSelector("#body .pm-secret.secret-readable:text-is('Paul Muster')");

    step("protected passages: admin erases the passage for good; it is gone from the text (ADR-0089)");
    await boss.click("#secrets .secret-edit summary");
    boss.once("dialog", (d) => d.accept());
    await boss.click("#secrets .secret-erase");
    await boss.waitForSelector("#secrets", { state: "detached" });
    await bossCtx.close();
    await page.reload();
    await page.waitForSelector("#body .ProseMirror p:has-text('Due Friday.')");
    const erasedText = await page.textContent("#body .ProseMirror");
    check(!erasedText.includes("Paul") && !erasedText.includes("▒") && await page.$("#body .pm-secret") === null, "the erased passage is gone: " + erasedText);

    check(problems.length === 0, problems.join("; "));
    console.log("ok");
  } catch (err) {
    await page.screenshot({ path: "failure.png", fullPage: true }).catch(() => {});
    console.error(err.message);
    if (problems.length) console.error(problems.join("\n"));
    process.exitCode = 1;
  } finally {
    await browser.close();
    sink.close();
    ckan.close();
    feed.close();
  }
})();
