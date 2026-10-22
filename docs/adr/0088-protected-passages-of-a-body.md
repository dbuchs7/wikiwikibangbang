# ADR-0088: Protected passages of a body

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-22
- **Deciders:** Project initiators. The maintainer asked to black out or encrypt words or paragraphs of pages and tasks. Unauthorised readers see a cryptic placeholder that names who can release it, with a button to ask. They chose:
  - words and whole paragraphs;
  - people and groups as readers;
  - the one who protects and the space's administrators as those who decide;
  - server-side encryption with a key per passage.

  (2026-10-22)

## Context and problem statement

Access is decided per item: whoever may read an item reads all of it (ADR-0022, ADR-0078). A page often holds one sensitive detail among text that everyone in the space should read: a salary, a name, an account. Today such a detail needs a separate item with its own restriction, which breaks the text apart.

How can parts of a body be readable only to some of those who read the item, without anything of them reaching the others?

## Decision drivers

- **Nothing leaks:** no answer, export, search hit, snippet, version, live session, comment quote, governance finding or copy may give a protected passage to someone who may not read it.
- **Placeholder:** others see that something is there and who decides, but not what it is. The placeholder does not reveal the passage's length.
- **Nothing lost:** a passage cannot be lost or overwritten by someone who cannot see it.
- **Narrowing only:** protection only narrows who reads an item (D9). A passage never grants access to the item.
- **Erasure:** a passage can be erased on its own, as an item can (ADR-0026).

## Considered options

1. **A mark over the passage, with the protection stored on the item. The server veils the passage for each reader, and seals its text in the log with a key of its own.**
2. **The same, with end-to-end encryption: only the readers' browsers decrypt.**
3. **No passages: put the sensitive part into an item of its own and embed it.**

## Decision outcome

Chosen option: **1**.

### Model

- A protected passage is a mark of type `secret` over its text. The mark's ref names a `Secret` of the item.
- A `Secret` holds who protected it (`By`), who else reads it (`Readers`, user IDs and `@groups`), and the pending requests.
- Passages do not overlap, and hold no link and no mention, since those would show where they point. A body can only refer to passages its item has.
- The protection is access data, like a restriction. Events:
  - `SecretSet` protects a passage or changes who reads it;
  - `SecretRemoved` takes the protection off;
  - `SecretRequested`, `SecretGranted` and `SecretDeclined` carry a request and its answer.
- Protecting (`SecretSet` together with the body that carries the new mark) and unprotecting (`SecretRemoved` together with the body without it) are each one batch.

### Who

- **Protect:** those who may edit the item and read all its passages, from text they read. A passage cannot be protected while the body is in a live session.
- **Read:** the one who protected it and the readers, if they may read the item at all.
- **Decide:** the one who protected it and the space's administrators. They change the readers, answer requests and take the protection off. An administrator who cannot read a passage may grant it to themself; that is recorded in the history.

### Veiling

For everyone else, the text of a passage is replaced by `▒▒▒▒▒▒▒▒`. The mark over it stays, so the page can show the passage as veiled and name who decides. Veiling applies to every way a body leaves the server:

- reading one item, listings, search (matching, snippet and the hit), exports (Word, print) and the MCP tools, which read through the API;
- the answers of every command;
- embedded items, read by their own permissions.

Anything a passage could leak through otherwise:

- **Quotes:** a comment's quote, and the text before and after it, are veiled where they share text with a passage the reader may not read.
- **Governance:** the excerpt of a sensitive-data finding is veiled the same way.
- **Earlier versions:** an earlier version may hold in clear what is protected now, so the versions of an item with passages are shown only to those who read them all.
- **Copies:** copying an item takes its passages along, and only those who read them all may copy it. A copy never holds a placeholder in place of the text.

### Changing the text

A live session shares one text with everyone in it (ADR-0010), and saving the body replaces it whole. So only those who read every passage of an item may change its text: they join its live session or commit its body. Everyone else keeps changing:

- the title, fields and state;
- labels;
- comments.

Their page says so.

### Storage

- A sealed body keeps the text of each passage apart from the rest. In the body, sealed with the item's key (ADR-0026), the passage's characters are filler, so the marks' offsets stay valid.
- The passage's own text goes into `secrets`, sealed with the passage's own key (`{item}~{secret}`).
- Destroying that key erases the passage from every version and leaves the rest of the text. The purge of the item destroys its passages' keys with its own.
- The format is codec version 21; versions 1 to 20 are still read.

### Interface

- **API:**
  - `POST /api/items/{ref}/secrets` protects a passage, given a block and character offsets;
  - `PUT` and `DELETE /api/items/{ref}/secrets/{id}` change the readers or take the protection off;
  - `POST …/request` asks to read it;
  - `POST …/requests/{principal}` with `grant` answers a request.
- An item read alone carries `secrets`, as the caller sees them, and `readsAllSecrets`.
- **Page:**
  - Selected text is protected with **Protect**.
  - A readable passage is highlighted; a veiled one is a dark bar.
  - The *Protected passages* card names who reads each passage. For a veiled passage it names who decides and offers to ask. To those who decide it shows the requests, with grant and decline.
- **Notifications:** those who decide are told of a request; the one who asked is told of the answer.

### Consequences

- **Positive:** a detail can be protected inside the text it belongs to. What others see says that it is there and whom to ask.
- **Negative / trade-offs accepted:** whoever cannot read every passage of an item cannot change its text, even far from the passage.
- **Negative / trade-offs accepted:** versions from before a passage was protected hold it in clear. They are shown only to those who read every current passage, and stay in the log until the item is purged.
- **Negative / trade-offs accepted:** the server can read every passage. Encryption at rest protects the log and its backups, not against the server or anyone holding its keys file. A backup holds the keys file, so whoever has a backup's passphrase can read every passage.
- **Negative / trade-offs accepted:** tables, code blocks and image captions cannot hold a passage, as they have no marks. Neither can titles, fields or comments.
- **Negative / trade-offs accepted:** veiling quotes and findings errs towards hiding, so it may veil a quote that only shares a common word with a passage.
- **Follow-ups:** erasing a single passage, by destroying its key, from the page. Passages in tables. A reader who leaves a group keeps nothing they read before.

## Pros and cons of the options

### Option 2: end-to-end encryption

Strongest form: the server never holds a passage in clear, so neither a server administrator nor a stolen keys file can read it.

- Bad:
  - every reader needs a key pair, kept and recovered in the browser;
  - granting a passage needs someone who holds it to re-encrypt it for the new reader;
  - search, export, the governance scan and the MCP tools cannot see it;
  - a lost key loses the passage.

  For a wiki whose server is trusted to enforce every other permission, that cost buys little.

### Option 3: an item of its own

Strongest form: no new model; the existing restriction does it.

- Bad: a salary in a sentence becomes a separate page, and the sentence a hole. The reader also learns nothing about whom to ask.

## Links

- relates to ADR-0022 and ADR-0078 (permissions, protecting items), ADR-0026 (keys per item, erasure), ADR-0010 (live sessions), ADR-0043 (versions), ADR-0047 (inline comments), ADR-0040 (governance), ADR-0067 (copying), ADR-0046 (notifications)
