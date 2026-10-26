# ADR-0136: Markdown from Git repositories: folders as page trees, files in a text

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-26
- **Deciders:** Project initiators (a contributor asked to show Markdown files of Git repositories in the wiki, formatted, always at the current version or at a chosen one, and chose: public repositories for now, whole folders as page trees, the common hosts including Bitbucket, an update every hour and a button to update now, 2026-10-26)
- **Open question:** How are private repositories read: with a list of repositories the operator allows, or with each person's own sign-in at the host?
- **Question checked:** 2026-10

## Context and problem statement

Teams keep documentation as Markdown files next to their code: a `README.md`, a `docs/` folder with a tree of pages. The wiki shows none of it. Pasting the address of such a file into a text gives a link (ADR-0096); readers leave the wiki to read it, and search, the governance check and exports do not see it. Copying the text by hand goes stale with the next commit.

The body is a block document, not Markdown (ADR-0001), and user content is rendered by one closed renderer (ADR-0014). How does the wiki show the Markdown of a repository — a whole folder, or one file in a text — formatted, current or at a chosen version, without a second renderer and without the server reaching what it should not?

## Decision drivers

- What a reader sees is stored in the wiki: search (ADR-0033), the governance check (ADR-0040), exports (ADR-0071), backups (ADR-0034) and the history (ADR-0043) see it. Content is items (D1); a page's text is a function of its stored version (D4).
- One renderer (ADR-0014). Markdown is a foreign format that is converted on the server, as import and paste convert theirs; no fragment of it reaches the browser as markup.
- A folder of a repository becomes a tree of pages; a file becomes one page that a text shows whole (ADR-0106), as a task made in a text (ADR-0114).
- The version is chosen by the address, as the hosts write it: a branch follows, a commit stays.
- The common hosts, cloud and self-hosted, without a library per host.
- What ADR-0077 and ADR-0127 decided for connectors holds: a dry run, an audited run, upsert by origin (ADR-0023), withdrawal instead of deletion, public addresses only unless the operator allows others.
- The server calls nobody unless someone responsible wants it to (ADR-0039).
- Public repositories only for now; nothing that needs a credential of a person.

## Considered options

1. **Convert on the server and store:** a connector kind `git` mirrors a folder as a page tree; "Insert → Markdown file…" makes a page below the edited item from one file and embeds it. Both convert the Markdown into blocks with a Markdown parser whose syntax tree our own code maps, and keep the pages current by reading the source again every hour.
2. **Show the file live:** a block kind that names the file's address; the server fetches and converts it each time the block is shown.
3. **Fetch in the reader's browser:** the page loads the file from the host and renders it.
4. **Import once:** convert the file into the edited text, with no later updates.

## Decision outcome

Chosen option: **1**, because the content is the wiki's own — searchable, checked, exported, backed up, versioned — while the address keeps the choice of a current or a fixed version, and the price is an hour's delay.

### The conversion

- **Parser.** [goldmark](https://github.com/yuin/goldmark) (MIT, pure Go, no further dependencies) parses CommonMark with the GitHub extensions for tables, strike-through, task lists and bare addresses. Only its parser and its syntax tree are used; its HTML renderer is never called. Our own code (`internal/markdown`) walks the tree and creates every block and mark itself, so ADR-0014's closed renderer stays the only renderer. ADR-0014 rejected a Markdown library as the renderer of stored content; here Markdown is an import format, which ADR-0014 has converted on the server.
- **Blocks.** Paragraphs, headings (levels 4 to 6 become 3, the model's deepest), bullet and numbered items, task items, quotes, code blocks, tables, dividers and pictures map to their block kinds. A quote that opens with `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]` or `[!CAUTION]` becomes a panel of the tone info, tip, note, warning or error.
- **Marks.** Emphasis, strong emphasis, strike-through, code and links become the marks italic, bold, strike, code and link. Only `http`, `https` and `mailto` targets stay links (ADR-0014).
- **What the model cannot hold:**
  - nested lists become flat items; inside a quote or a panel, an item keeps its marker as text;
  - a table's cells are plain text, and a link in one keeps its address in parentheses;
  - a code block loses its language;
  - footnotes and formulas stay as their text.
- **HTML in the Markdown** never becomes markup. Comments, scripts and styles are dropped, `<br>` is a line break, `<img>` a picture. Of other tags only the text between them stays.
- **Title.** The `title` of a front matter, else a leading heading of level 1, which then leaves the text, else the file's name without its extension. The front matter itself is not shown.
- **Links.** A relative link is resolved against the file's place in the repository, at the version read. Within a mirrored folder, it becomes a mention of the page written from the target file (D2); otherwise a link to the file at the host.
- **Pictures.** A picture is read and kept as an attachment of the page (ADR-0045), within the attachment budget. Only PNG, JPEG, GIF and WebP are kept, as these are what a page shows inline. An SVG can carry script, so it becomes a link with its alternative text, as does a picture that cannot be read. A picture already attached with the same content is not stored again.
- **One mapping, two converters.** ADR-0133 converts pasted Markdown in the browser (`assets/markdown.js`). A mirrored file is read and converted by the server, every hour and without a browser, and the server has no JavaScript runtime (ADR-0009), so this record needs a converter in Go. Both map Markdown onto the blocks in the same way; a difference between them is a bug. Where they differ on purpose, the source differs: a file of a repository has its pictures and its links in the repository, which the server reads, so pictures are attached and links between mirrored files become mentions; and raw HTML, common in READMEs, keeps its text and pictures instead of showing as text.
- **Size.** A Markdown file of more than 2 MB is left out and reported. A page keeps at most 50 pictures, read up to `WWBB_BUDGET_MARKDOWN_PICTURE_BYTES` together (50 MB by default, ADR-0015); the pictures beyond stay links.

### The hosts

- **Supported:** GitHub (github.com and Enterprise Server), GitLab (gitlab.com and self-managed), Bitbucket Cloud, Bitbucket Data Center, and Gitea and Forgejo (e.g. Codeberg).
- **Recognised from the address** of a file or folder as the host shows it in the browser:
  - known names: github.com, gitlab.com, bitbucket.org, codeberg.org;
  - for other names, the path's shape: `/-/blob/` and `/-/tree/` for GitLab, `/projects/…/repos/…/browse/` for Bitbucket Data Center, `/src/branch/`, `/src/tag/` and `/src/commit/` for Gitea and Forgejo, `/blob/` and `/tree/` for GitHub Enterprise Server.
  - A self-managed GitLab is taken to be installed at the root of its host, as its address does not tell where an installation below a path ends and the project's path begins.
- **What is read:** each host's public API, for three things: the commit a branch or tag points to, the files below a folder, and a file at a commit. A branch or tag name holding a `/` is found by trying the longer names in turn.
- **Credentials.** A connector sends its credential `WWBB_CONNECTOR_<ID>_TOKEN` if the operator sets one, as every connector does (ADR-0127). It raises the host's rate limit; it also reads what the token may read, which is the operator's choice. A file in a text never sends a credential, so it reads public repositories only.
- **Addresses.** Every request keeps to public addresses, redirects included, unless `WWBB_CONNECTOR_ALLOW_PRIVATE=on` (ADR-0127). That is needed for a self-hosted instance in the organisation's own network.

### A folder as a page tree: the connector kind `git`

- **Settings:** the address of a folder (or a file) at a branch, tag or commit, and the language of the page's own words. The type and the field mapping are as for every connector (ADR-0127). The source gives the values `path`, `url` and `commit`. A new connector runs every hour.
- **Entries:**
  - The folder and every folder below it that holds Markdown files becomes a page. Each file ending in `.md` or `.markdown` becomes a page below its folder's page.
  - A folder's `README.md`, `index.md` or `_index.md` is its page's text, not a page of its own.
  - Other files are not mirrored; pictures come in only as a page shows them.
  - Pages of folders come before pages of files, each in the order of their names.
  - At most 2000 Markdown files are read per run; more are reported and left out.
- **Identity:** an entry is its path in the repository (`entry/<path>`). Git keeps no identity for a file, so a renamed or moved file is withdrawn under its old path and new under its new one.
- **Writing:**
  - The Markdown read is kept in the item's origin (`Origin.Raw`, sealed content, ADR-0026). The commit it was read at is kept as the origin's version.
  - A page is written only when its Markdown changed. Text edited by hand stays until the source changes; the next change replaces it, and the history keeps it. ADR-0080 replaces such text at every run; this kind compares the source instead.
  - Where the host's listing names each file's content (its Git blob), a file whose content is unchanged is not read at all.
  - A run whose source points to the same commit as the last run, with the same settings, ends after one request. A run started by hand always reads everything, and so does the next run after one that could not read or write an entry for a reason that may pass.
- **Place:** a new page goes below its folder's page. People may move pages; a run does not move them back, as for every connector.
- **Withdrawal** is that of every connector: a page whose file is gone is labelled and keeps what was added here.

### A file in a text

- **Inserting:**
  - "Insert… → Markdown file…" asks for the address of a file and for the version: **always the current one**, which follows the branch or tag of the address, or **this one**, pinned to the commit the address points to now. An address that names a commit is pinned.
  - A pasted address of a Markdown file becomes a link, as before (ADR-0096); the link's bubble offers to show the file's content instead.
- **What is made:**
  - A page below the edited item, in its space, through the caller's session (D10). The caller must be allowed to add a page there.
  - The text gets an embed of that page, which shows it whole (ADR-0106).
  - Inserting the same file in the same item again updates the page made before, wherever it was moved since.
  - The page holds the file's text alone. Where it comes from and which version it shows, the page and the embed say in each reader's language, from the origin.
- **Origin:**
  - the system `git:file`, which no connector's ID can be, the repository's address as its instance, and the parent and the path as its ID;
  - the branch or tag to follow as its key, and the file's address at that branch or tag, or at the pinned commit;
  - the commit read as its version, and the Markdown as its raw content.
- **Updating:**
  - Every hour the server reads each page that follows a branch or tag, at that branch or tag. When the Markdown changed, it asks the host for the commit and writes the page as `sync:git`.
  - Whoever may edit the page can update it now, pin the version it shows, or follow the branch or tag again.
  - A pinned page is not read again.
- **Off unless the operator wants it:** `WWBB_GIT_FILES=on` lets editors have the server read files. Any other value leaves the function off, and the server says so when asked. This follows ADR-0039: here editors, not administrators, make the server call out.

### Consequences

- **Positive:** a repository's documentation reads as wiki pages: found by search, checked for sensitive data, exported, backed up and versioned, with links between its pages that work in the wiki.
- **Positive:** the address decides the version, as the hosts teach: a branch link follows, a permalink stays.
- **Positive:** one walker over one syntax tree serves both uses, and a new host is one adapter of three requests.
- **Negative / trade-offs accepted:**
  - What the wiki shows can be an hour behind the repository. "Update now" closes the gap on request.
  - Formatting the block model cannot hold is lost (see *The conversion*). Raw HTML, common in READMEs, keeps only its text and pictures.
  - A dependency: goldmark, about 14 000 lines of Go we depend on and do not own, at a pinned version.
  - The Markdown is kept twice, as the page and in its origin, so a later conversion needs no source.
  - A better converter reaches a page only when its source changes.
  - Without a credential, github.com answers 60 API requests an hour per server address. A connector needs one request per run while its branch stands still, two when it moved. A file in a text needs none while it is unchanged, as files are read without the API there, and one when it changed.
  - Bitbucket's listings do not name a file's content, so a run after a new commit reads every Markdown file of the folder.
- **Follow-ups / risks to watch:**
  - private repositories (the open question);
  - Azure DevOps Repos and further hosts;
  - updates pushed by the host (webhooks) instead of read every hour;
  - nested lists, once the block model holds them.

## Pros and cons of the options

### Option 2: show the file live

Strongest form: always current to the second; nothing is stored twice, no job runs, and the block is a plain window onto the repository.

- Good: no delay and no copy.
- Bad: search, the governance check, exports and backups do not see the content, and the history shows nothing of its changes.
- Bad: each view calls the host. A slow or failing host, or its rate limit, empties the page for every reader.
- Bad: what a reader sees is no longer a function of the stored version (D4).

### Option 3: fetch in the reader's browser

Strongest form: the server makes no outbound call at all, and each reader fetches at the host as themselves.

- Bad: every reader's visit is reported to the host, which ADR-0097 refused for players of other sites.
- Bad: the content security policy would have to admit the hosts.
- Bad: every reader's browser would convert the file at every view, and the hourly update, which runs without a browser, could not.
- Bad: the drawbacks of option 2 apply as well.

### Option 4: import once

Strongest form: the simplest; the text is the wiki's from then on and is edited like any other.

- Good: no job, no outbound call after inserting.
- Bad: stale with the next commit, which is the problem the request names. It is what "this version" in option 1 does, with the source kept.

### A parser of our own instead of goldmark

Strongest form: no dependency, as the project prefers the standard library. The block model is lossy anyway, so a subset of Markdown would do, and ADR-0133 shows that a closed converter of about 900 lines is enough for pasted text; porting it to Go would make one design in two languages.

- Good: no code we do not own; the two converters could be kept in step line by line.
- Bad: a paste is checked by the person who pastes it, and ADR-0133 errs towards plain text. A mirrored folder is read in bulk, unattended, from documentation that uses the whole of CommonMark: nested containers, reference links across a file, blocks of HTML. CommonMark's rules for emphasis, nested containers and lazy continuation are intricate; a parser of our own would differ from what the hosts show in exactly the cases people notice, and getting it right means maintaining a test suite of the size of the specification.

## Links

- relates to [ADR-0133](0133-markdown-is-pasted-into-the-editor-and-converted-in-the-browser.md) (pasted Markdown, converted in the browser the same way), [ADR-0131](0131-pages-export-as-markdown-and-agents-read-them-so.md) (pages export as Markdown; this record reads Markdown in), [ADR-0014](0014-closed-renderer-for-user-content.md) (Markdown is converted, not rendered), [ADR-0127](0127-connectors-read-any-json-source-into-a-type-of-ones-own.md) and [ADR-0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md) (a further kind of connector), [ADR-0080](0080-one-page-per-data-set-written-from-the-source.md) (this kind writes a page only when its source changed), [ADR-0106](0106-an-embedded-item-is-shown-whole.md) and [ADR-0114](0114-tasks-in-a-text.md) (a page made below the text and shown whole), [ADR-0039](0039-link-check-on-demand-public-addresses-only.md) (outbound calls only when wanted), [ADR-0023](0023-import-and-sync-from-jira-and-confluence.md) (origin), [ADR-0045](0045-attachments-encrypted-files-beside-the-log.md) (pictures), ADR-0096 (pasting an address)
