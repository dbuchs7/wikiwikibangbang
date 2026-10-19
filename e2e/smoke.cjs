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

(async () => {
  const browser = await chromium.launch(process.env.CHROMIUM ? { executablePath: process.env.CHROMIUM } : {});
  const page = await browser.newPage();
  const problems = [];
  page.on("pageerror", (e) => problems.push("page error: " + e.message));
  page.on("console", (m) => { if (m.type() === "error" && !/status of 401/.test(m.text())) problems.push("console: " + m.text()); });
  page.on("dialog", async (d) => { problems.push("dialog: " + d.message()); await d.dismiss(); });
  const step = (name) => console.log("·", name);

  try {
    step("login form, wrong password");
    await page.goto(base + "/");
    await page.fill("#login [name=username]", "admin");
    await page.fill("#login [name=password]", "wrong password!");
    await page.click("#login button[type=submit]");
    await page.waitForSelector("#login .error");

    step("log in");
    await page.fill("#login [name=password]", adminPassword);
    await page.click("#login button[type=submit]");
    await page.waitForSelector("text=No spaces yet");

    step("the footer names the running version");
    await page.waitForSelector("footer.footer:has-text('wikiwikibangbang')");

    step("administration: space, type, user, role");
    await page.click("text=Administration");
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

    step("create an item with a hostile title");
    await page.click("a.brand");
    await page.click("a:has-text('Platform')");
    await page.fill("#new-item [name=title]", hostile);
    await page.selectOption("#new-item [name='p:priority']", "high");
    await page.fill("#new-item [name='p:estimate']", "3");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector("#title");
    check(await page.inputValue("#title [name=title]") === hostile, "the title shows as typed");
    check(page.url().endsWith("#/item/PLAT-1"), "the item is addressed by key");
    await page.waitForSelector("#watch:has-text('Stop watching')"); // its creator watches it

    step("edit property, transition");
    await page.fill("[name='p:estimate']", "2.5");
    await page.click("tr:has-text('estimate') button");
    await page.waitForSelector("text=version 2");
    await page.click("button:has-text('finish')");
    await page.waitForSelector("text=state: done");

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
    await page.fill(".pm-target", "PLAT-1");
    await page.click(".pm-toolbar button:has-text('Mention')");
    await page.waitForSelector(".ProseMirror .pm-mention"); // resolving the key is asynchronous
    await page.fill(".pm-target", "PLAT-1");
    await page.click(".pm-toolbar button:has-text('Embed')");
    await page.waitForSelector(".ProseMirror .pm-embed");
    await page.click("#body h2"); // leaving the editor releases its blocks, which saves them
    await page.waitForSelector(".live[data-pending='0']");

    step("the body survives a reload exactly");
    await page.reload();
    await page.waitForSelector(".ProseMirror h3");
    check((await page.textContent(".ProseMirror h3")) === "Steps", "heading level 2 renders as h3 under the title");
    check(await page.locator(".pm-bullet").count() === 2, "two bulleted items");
    check((await page.textContent(".ProseMirror strong")) === "Bold", "bold text");
    check((await page.textContent(".pm-mention")) === "@PLAT-1", "the mention");
    check((await page.textContent(".pm-embed")).includes("PLAT-1"), "the embed");
    check((await editor.textContent()).includes("<script>alert('body')</script>"), "the script shows as text");

    step("live: two people edit one body at once");
    const other = await browser.newPage();
    other.on("pageerror", (e) => problems.push("bert's page error: " + e.message));
    other.on("dialog", async (d) => { problems.push("bert's dialog: " + d.message()); await d.dismiss(); });
    await other.goto(base + "/");
    await other.fill("#login [name=username]", "bert");
    await other.fill("#login [name=password]", "bert's password");
    await other.click("#login button[type=submit]");
    await other.waitForSelector("nav >> text=bert");
    await other.goto(base + "/#/item/PLAT-2");
    await other.waitForSelector(".ProseMirror h3");
    await page.waitForSelector(".live-presence:has-text('bert')");
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
    await page.click("#body h2");
    await other.click("#body h2");
    await page.waitForSelector(".live[data-pending='0']");
    await other.waitForSelector(".live[data-pending='0']");
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
    await page.fill("nav input[name=q]", "alert body");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector(".hits li");
    check((await page.textContent(".hits li a")) === "PLAT-2", "the runbook is the hit");
    check((await page.textContent(".hits .snippet")).includes("<script>alert('body')</script>"), "the snippet shows the script as text");
    await page.fill("nav input[name=q]", "staging hosts");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector(".hits .snippet:has-text('In a comment: Patched on the staging hosts.')");
    await page.fill("nav input[name=q]", "zzzz-nothing");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector("text=Nothing found");

    step("a slow view does not replace the one chosen after it");
    await page.route("**/api/items/PLAT-1", async (r) => { await new Promise((ok) => setTimeout(ok, 1500)); await r.continue(); });
    await page.evaluate(() => { location.hash = "#/item/PLAT-1"; });
    await page.fill("nav input[name=q]", "alert body");
    await page.press("nav input[name=q]", "Enter");
    await page.waitForSelector(".hits li");
    await page.waitForTimeout(2500); // the item view has answered by now
    check(await page.locator(".hits li").count() === 1, "the search results stay");
    await page.unroute("**/api/items/PLAT-1");

    step("app switcher: nine dots open the modules; Tasks lists open work across spaces");
    await page.click("#app-switcher");
    await page.waitForSelector("#drawer:not([hidden])");
    const modules = (await page.locator("#drawer-apps a").allTextContents()).join(",");
    check(modules === "Spaces,Create,Tasks,AI assistants (MCP),Notifications,Governance,Account,Administration", "the modules of an administrator: " + modules);
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

    step("task inbox: an overdue unassigned task is taken over and lands under Assigned to me");
    await page.evaluate(async () => {
      const send = (method, path, body) => fetch(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
      await send("POST", "/api/types", { name: "ticket", body: true,
        fields: [{ name: "assignee", kind: "user" }, { name: "due", kind: "date" }, { name: "priority", kind: "enum", values: ["low", "high"] }],
        workflow: { initial: "open", states: ["open", "closed"], transitions: [{ name: "close", from: ["open"], to: "closed" }] } });
      await send("POST", "/api/items", { space: "PLAT", type: "ticket", title: "Renew the certificate", properties: { due: "2020-01-31", priority: "high" } });
    });
    await page.goto(base + "/#/tasks/unassigned");
    await page.waitForSelector("#tasks tr.overdue:has-text('Renew the certificate') .badge:has-text('overdue since 2020-01-31')");
    await page.click("#tasks tr:has-text('Renew the certificate') button:has-text('Take over')");
    await page.goto(base + "/#/tasks/mine");
    await page.waitForSelector("#tasks tr:has-text('Renew the certificate') td:has-text('you')");
    await page.click("#tasks tr:has-text('Renew the certificate') button:has-text('close')");
    await page.waitForSelector("#tasks tr:has-text('Renew the certificate')", { state: "detached" });

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
      await page.fill("#new-child [name=title]", title);
      await page.selectOption("#new-child [name='p:priority']", "low");
      await page.click("#new-child button[type=submit]");
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
    await page.click("#nav-new");
    await page.waitForSelector("#new-item [name=under]:checked");
    await page.fill("#new-item [name=title]", "Created from New");
    await page.selectOption("#new-item [name=type]", "task");
    await page.selectOption("#new-item [name='p:priority']", "low");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector(".tree a.current:has-text('Created from New')");
    check(await page.locator(".tree li.open > .tree-row a:has-text('Upgrade')").count() === 1, "the new page lies below the page it was created from");
    await page.click("#item-new-child"); // the item's own "new child page"
    await page.waitForSelector("#new-item [name=under]:checked");
    await page.goBack();

    step("blocks: a task, a warning panel, a table and a table of contents survive a reload");
    await page.click(".tree a:has-text('Install')");
    await page.waitForSelector(".tree a.current:has-text('Install')");
    await page.waitForSelector("#body .ProseMirror");
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
    await page.click("#body h2"); // leave the editor: its blocks are released and saved
    await page.waitForSelector(".live[data-pending='0']");
    await page.reload();
    await page.waitForSelector("#body .pm-task[data-checked='true']:has-text('Check the backups')");
    await page.waitForSelector("#body .pm-panel[data-tone='warning']:has-text('Writers must be stopped.')");
    check(await page.inputValue("#body .pm-table tr:nth-child(2) td:first-child input") === "db1", "the table cell kept its text");
    await page.waitForSelector("#body .pm-toc button:has-text('Prepare')");

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
    await page.click("#version ~ table button:has-text('Changes') >> nth=0"); // the newest change
    await page.waitForSelector("#version .diff-change ins:has-text('replicas')");
    await page.waitForSelector("#version .diff-add:has-text('Check the version')");
    await page.click("#version ~ table button:has-text('Changes') >> nth=1"); // the one before
    await page.waitForSelector("#version h3");
    await page.click("#version button:has-text('Restore this version')");
    await page.waitForSelector("#body .ProseMirror:has-text('Upgrade the primary first.')");
    check(await page.locator("#body .pm-task").count() === 0, "the restored body is the earlier one");

    step("labels: a label added to a page lists the page");
    await page.fill("#labels [name=label]", "Disaster Recovery");
    await page.press("#labels [name=label]", "Enter");
    await page.waitForSelector("#labels .label a:text-is('disaster-recovery')");
    await page.click("#labels .label a:text-is('disaster-recovery')");
    await page.waitForSelector("#labelled td:has-text('Upgrade')");
    await page.goBack();
    await page.waitForSelector("#labels .label");
    await page.click("#labels .label-remove");
    await page.waitForFunction(() => document.querySelectorAll("#labels .label").length === 0);

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

    step("relations and access: a page is related by key and unrelated; only I may edit it, then everyone again");
    await page.fill("#add-relation [name=type]", "depends-on");
    await page.fill("#add-relation [name=target]", "PLAT-1");
    await page.click("#add-relation button[type=submit]");
    await page.waitForSelector("#relations li:has-text('depends-on') a:has-text('PLAT-1')");
    await page.click("#relations li:has-text('depends-on') button[aria-label='Remove this relation']");
    await page.waitForSelector("#relations li:has-text('depends-on')", { state: "detached" });
    await page.click("#access button:has-text('Only I may edit')");
    await page.waitForSelector("#access p:has-text('May edit: you')");
    await page.click("#access button:has-text('Lift the restriction')");
    await page.waitForSelector("#access p:has-text('May edit: everyone with a role in the space')");

    step("files: an upload is listed and downloads; an image goes into the body and shows after a reload");
    const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");
    await page.setInputFiles("#upload", { name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("<script>alert('file')</script>") });
    await page.waitForSelector("#attachments li:has-text('notes.txt')");
    const notes = await page.getAttribute("#attachments li a:has-text('notes.txt')", "href");
    const served = await page.evaluate(async (href) => { const r = await fetch(href); return [r.headers.get("content-type"), r.headers.get("content-disposition"), await r.text()]; }, notes);
    check(served[0] === "application/octet-stream" && served[1].startsWith("attachment") && served[2].includes("alert('file')"), "a text file only downloads: " + served);
    await page.waitForSelector("#body .ProseMirror");
    await page.locator("#body .ProseMirror").click();
    await page.setInputFiles("#body .pm-toolbar input[type=file]", { name: "pixel.png", mimeType: "image/png", buffer: png });
    await page.waitForSelector("#body .pm-image img");
    await page.click("#body h2"); // leave the editor: the new block is released and saved
    await page.waitForSelector(".live[data-pending='0']");
    await page.reload();
    await page.waitForFunction(() => { const img = document.querySelector("#body .pm-image img"); return img && img.complete && img.naturalWidth === 1; });
    check(await page.locator("#attachments li").count() === 2, "both files are listed");

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

    step("administration: groups, a group's role in a space, system roles");
    await page.click("nav a:has-text('Administration')");
    await page.fill("#add-member [name=group]", "ops");
    await page.selectOption("#add-member [name=user]", { label: "bert" });
    await page.click("#add-member button[type=submit]");
    await page.waitForSelector("#groups th:text-is('@ops')");
    await page.click("#users tr:has-text('bert') label:has-text('erasure') input");
    await page.waitForSelector("#users tr:has-text('bert') label:has-text('erasure') input:checked");
    await page.click("#users tr:has-text('bert') label:has-text('erasure') input");
    await page.waitForSelector("#users tr:has-text('bert') label:has-text('erasure') input:not(:checked)");
    await page.click("#admin-spaces a:has-text('Members and roles')");
    await page.fill("#add-role [name=group]", "ops");
    await page.selectOption("#add-role [name=role]", "editor");
    await page.click("#add-role button[type=submit]");
    await page.waitForSelector("#members tr:has-text('@ops')");
    check(await page.inputValue("#members tr:has-text('@ops') select") === "editor", "the group holds the role granted");
    await page.click("nav a:has-text('Administration')");
    await page.click("#groups button[aria-label^='Take bert out']");
    await page.waitForSelector("#admin-groups p:has-text('No groups yet.')");

    step("AI assistants: a token made on the MCP page lets an assistant write a page with its content");
    await page.click("#app-switcher");
    await page.click("#drawer-apps a:has-text('AI assistants (MCP)')");
    await page.waitForSelector(".tools li:has-text('create_item')");
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
    await page.click("nav a[href='#/account']");
    await page.fill("#new-token [name=name]", "ci");
    await page.click("#new-token button[type=submit]");
    await page.waitForSelector("text=Copy this token now");
    check(/wwbb_[0-9a-f]{64}/.test(await page.textContent("code")), "the token secret is shown once");

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

    step("log out, log in as bert, read the item");
    await page.click("text=Log out");
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

    step("watching: bert watches the item, admin renames it, bert is told");
    await page.click("#watch:has-text('Watch')");
    await page.waitForSelector("#watch:has-text('Stop watching')");
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
    await adminCtx.close();
    await page.reload();
    await page.waitForSelector("#nav-notifications[aria-label='Notifications (1)'] .count:text-is('1')");
    await page.click("#nav-notifications");
    await page.waitForSelector("#notifications li.unread:has-text('admin renamed PLAT-1 Renamed by admin')");
    await page.waitForSelector("#nav-notifications[aria-label='Notifications'] .count[hidden]", { state: "attached" }); // seen now
    await page.waitForSelector("#watches a:has-text('PLAT-1')");

    check(problems.length === 0, problems.join("; "));
    console.log("ok");
  } catch (err) {
    await page.screenshot({ path: "failure.png", fullPage: true }).catch(() => {});
    console.error(err.message);
    if (problems.length) console.error(problems.join("\n"));
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
})();
