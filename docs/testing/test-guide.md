# Test guide

wikiwikibangbang is an early prototype. This guide is for people who try a release and report what they find. It says what to look at first, because a few open questions in the ADRs can only be answered by people using the product, and how to report a finding so it can be acted on.

**Do not put real or confidential data into a test installation.** The prototype makes no promise that a later version reads today's data directory.

## Start a server

From a release archive (Linux shown; macOS and Windows work the same way):

```bash
tar xzf wikiwikibangbang_<version>_linux_amd64.tar.gz
cd wikiwikibangbang_<version>_linux_amd64
WWBB_ADMIN_PASSWORD='a long password of your own' ./wikiwikibangbang serve -addr :8080
```

Or as a container:

```bash
docker run -p 8080:8080 -v wwbb-data:/data \
  -e WWBB_ADMIN_PASSWORD='a long password of your own' ghcr.io/dbuchs7/wikiwikibangbang:<version>
```

Open <http://localhost:8080> and sign in as `admin`. `./wikiwikibangbang version` prints the version to quote in a report.

## Set up a playground

As `admin`, under **Administration**:

1. Create a space, e.g. key `PLAT`, name `Platform`.
2. Define a type. A small one to start with:

   ```json
   {"name": "task", "body": true,
    "fields": [{"name": "priority", "kind": "enum", "required": true, "values": ["low", "high"]},
               {"name": "estimate", "kind": "number"}],
    "workflow": {"initial": "open", "states": ["open", "done"],
                 "transitions": [{"name": "finish", "from": ["open"], "to": "done"}]}}
   ```

3. Create two or three users and give them roles in the space: `editor` (may change items) and `viewer` (may only read).

Use a second browser, or a private window, to be signed in as two people at once.

## What to look at first

Each area names the question it helps answer. A report that says "this felt wrong, because …" is as useful as a bug.

### 1. Editing together (ADR-0010)

Two people open the same item and type into its body at the same time. A paragraph one person is editing is locked for the other until they leave it.

- Is it clear who is in the document and which paragraph is locked?
- When you want to type into a paragraph someone else holds, is it obvious what to do?
- **The open question:** is "one person per paragraph" acceptable for how you work, or do you need several people typing in the same paragraph?

### 2. Showing versus containing (ADR-0020)

An item's body can **embed** another item (toolbar: *Embed*, with the other item's key). An item embedded in a page is not necessarily *in* that page: where an item lives (its place in the tree) and where it is shown are independent.

- Embed an item that lives elsewhere, and create one from inside a page. Can you tell the two apart?
- Remove an embed: did you expect the item to be gone? (It is not; it stays where it lives.)
- **The open question:** do you understand the difference between "lives here" and "is shown here", or does the page need to show it more clearly?

### 3. Permissions (ADR-0022)

- Sign in as a viewer: you can read but not change. Is that clear before you try?
- Sign in as someone without a role: items of the space must not appear anywhere — not in search, not as embeds, not in relations. An embed you may not read shows as "restricted" without its title. **Anything that reveals an item you should not see is a security finding; report it privately (see below).**

### 4. Backup and restore (ADR-0034)

1. **Administration → Backup**: choose a passphrase and download the backup.
2. Stop the server and restore into a new directory:

   ```bash
   WWBB_BACKUP_PASSPHRASE='the passphrase' ./wikiwikibangbang restore -from backup.wwbb -data-dir restored
   ./wikiwikibangbang check -data-dir restored
   ./wikiwikibangbang serve -data-dir restored
   ```

3. Is everything there — items, bodies, history, users?

### 5. An AI agent (ADR-0012)

Create an API token under your account (scope `read` for an agent that may only read) and connect an MCP client, e.g. Claude Code:

```bash
claude mcp add wikiwikibangbang --env WWBB_TOKEN=<token> -- ./wikiwikibangbang mcp -url http://localhost:8080
```

Ask it to summarise a space, create tasks from notes, or finish a task. It acts with exactly your rights; moving, trashing and deleting items is deliberately not offered to agents.

- Did the agent do what you asked, and only that?
- With a read token, does it explain that it may not write?

### 6. Comments (ADR-0035)

Every item has a comment section below its relations. Editors write comments; viewers read them.

- Write, edit and delete a comment. Does a deleted comment disappear for everyone, and does the history keep only that something changed?
- **The open question:** should viewers be allowed to comment, or is commenting rightly tied to the right to edit the item?

### 7. Boards (ADR-0036)

On a space's page, *Board: task* shows the space's tasks as columns, one per workflow state. Move a card with its buttons or by dragging it to another column.

- Is it clear why a card cannot be dropped on some columns?
- **The open question:** is creation order enough, or do you need to order cards by hand, filter them, or see several states in one column?

### 8. Link check (ADR-0039)

Only if the server runs with `WWBB_LINKCHECK=on`. Put links into a body, some working, some not, and one to an address in your home network. As a space administrator, open *Link check* on the space's page and start a check.

- Does the list name the right items and the person who edited them last?
- Is a page reported as broken that works in your browser? Say which one; some sites turn away automated checks.

### 9. Governance (ADR-0040)

Paste something that looks like a password (`password: Test1234!`), a card number or an IBAN into a page as one user, then open the page as another user of the space. Open *Governance* on the space's page as a space administrator.

- Is the page hidden from the other user, and does it tell its author why?
- Does the governance view show the finding masked, and can an administrator lift the restriction?
- **Report** every harmless text that was flagged, and every secret that was not.

### 10. Pasting Markdown (ADR-0133)

Copy Markdown from an assistant, a README or a code editor and paste it into a page with Ctrl+V; paste other Markdown through *Insert… → Paste Markdown…*. The [Markdown guide](../guides/markdown.md) lists what is converted.

- Does Markdown become the formatting you expected — headings, lists, tasks, tables, code, links?
- Did ordinary text you pasted (a mail, a list, a piece of code) ever turn into formatting you did not want? That is a finding: say what you pasted.
- Is it clear what happened to pictures and to what the page cannot show?

### 11. Markdown from Git repositories (ADR-0137)

Under *Administration → Connectors*, start *New connector (Git)*, give it a space and the address of a folder of a public repository as its host shows it, e.g. `https://github.com/owner/repository/tree/main/docs`, and run it. With `WWBB_GIT_FILES=on`, edit a page, paste the address of a Markdown file and choose *Show the file's content* in the link's bubble, or use *Insert… → Markdown file from a repository…*.

- Do the pages read as the files do at the host? Report what looks different and how it looks there.
- Do links between the files lead to their pages, and are the pictures there?
- Does a page follow its branch after a change at the host (within an hour, or at once with *Update now*), and does a pinned one stay?

### 12. Everything else

Search, item history, relations, mentions (toolbar: *Mention*), formatting, the API at `/api/openapi.json`. Anything that surprises you is worth a report.

## Report a finding

Open an issue with the **Test finding** template: <https://github.com/dbuchs7/wikiwikibangbang/issues/new/choose>. One finding per issue. The template asks for:

- the version (`wikiwikibangbang version`) and the platform or container;
- the steps, starting from a fresh server where you can;
- what you expected, and what happened instead;
- the server's log lines around the moment, if any.

**Security findings** — something you can see or do that your role should not allow, a script that runs from content, a password or token that shows up somewhere — are **not** reported as public issues. Use GitHub's private vulnerability reporting on the repository's *Security* tab instead, as [SECURITY.md](../../SECURITY.md) describes.

Write reports in English if you can; a report in another language is better than none.
