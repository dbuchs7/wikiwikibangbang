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
    await page.click("#nav >> text=Spaces");
    await page.click("a:has-text('Platform')");
    await page.fill("#new-item [name=title]", hostile);
    await page.selectOption("#new-item [name='p:priority']", "high");
    await page.fill("#new-item [name='p:estimate']", "3");
    await page.click("#new-item button[type=submit]");
    await page.waitForSelector("#title");
    check(await page.inputValue("#title [name=title]") === hostile, "the title shows as typed");
    check(page.url().endsWith("#/item/PLAT-1"), "the item is addressed by key");

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
    check((await page.locator("#drawer-apps a").allTextContents()).join(",") === "Spaces,Tasks,Governance", "the three modules");
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

    step("board: columns are the workflow's states; dragging a card performs the transition");
    await page.click("#nav >> text=Spaces");
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
    await page.click("#nav >> text=Spaces");
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
    await page.click("#version ~ table button:has-text('Changes') >> nth=0"); // the newest change first
    await page.waitForSelector("#version .diff");
    check(await page.locator("#version .diff-add, #version .diff-change").count() > 0, "the newest change adds something");
    await page.click("#version ~ table button:has-text('Changes') >> nth=-1"); // the first change of the body
    await page.waitForSelector("#version h3");
    await page.waitForTimeout(1600); // the editor has released its blocks by now
    await page.click("#version button:has-text('Restore this version')");
    await page.waitForFunction(() => !document.querySelector("#body .pm-table"));
    check(await page.locator("#body .pm-task").count() === 0, "the restored body is the first one");

    step("links: the link check page says when checking is off");
    await page.click("#nav >> text=Spaces");
    await page.click("a:has-text('Platform')");
    await page.click("a:has-text('Link check')");
    await page.waitForSelector("#linkcheck >> text=switched off");

    step("governance: the check of a space lists its three kinds of findings");
    await page.click("#nav >> text=Spaces");
    await page.click("a:has-text('Platform')");
    await page.click("main a:has-text('Governance')");
    await page.waitForSelector("#governance h2:has-text('Sensitive data')");
    await page.waitForSelector("#governance h2:has-text('Not changed')");
    await page.waitForSelector("#governance h2:has-text('Implausible')");

    step("account: token");
    await page.click("nav a[href='#/account']");
    await page.fill("#new-token [name=name]", "ci");
    await page.click("#new-token button[type=submit]");
    await page.waitForSelector("text=Copy this token now");
    check(/wwbb_[0-9a-f]{64}/.test(await page.textContent("code")), "the token secret is shown once");

    step("language: German on request, kept across sign-ins");
    await page.selectOption("#language [name=language]", "de");
    await page.waitForSelector("nav >> text=Bereiche");
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
    await bertDe.waitForSelector("nav >> text=Spaces");
    await german.close();
    await page.selectOption("#language [name=language]", "en");
    await page.waitForSelector("nav >> text=Spaces");

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
