# ADR-0160: Protected blocks and pictures of a page

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-02
- **Deciders:** Project initiators. The maintainer reported: "In Orbithek only text can be protected; I also want to protect headings, pictures etc. — generally any content of a page." They decided (2026-11-02):
  1. every block kind can be protected as a whole — headings, paragraphs, list items, quotes, panels, tables, code, images and drawings, embeds, BPMN, process, form, decision and architecture blocks, tasks, dividers — with the readers, requests, answers, notifications and deciders of protected passages (ADR-0088), reusing the `Secret` of a passage; passages stay as they are;
  2. a contiguous range of several blocks, such as a heading with its section, is protected in one step, as one `Secret`; the editor offers to protect a block, the blocks a selection touches, and the section of a heading;
  3. a protected picture is protected as a file, not only as a block: whoever may not read the protection gets the file nowhere — not by its address, in the album, on the starmap or in an impact view, in exports, copies, versions, MCP, the change feed or search.

## Context and problem statement

ADR-0088 protects words or paragraphs of a body with a mark over their text. Marks exist only in blocks of text: a heading's or a paragraph's text can hold a passage, but a picture, a table, a code block, an embed or a diagram cannot, and a section of several blocks needs several passages, one per block, each with its own readers and requests. A picture's file is an attachment of the item (ADR-0045) that whoever reads the item reads, wherever it is listed: the item's files, the album (ADR-0054, ADR-0090, ADR-0103), the starmap and impact view (ADR-0157, ADR-0159), exports (ADR-0071, ADR-0131) and copies (ADR-0055, ADR-0067, ADR-0108).

How are whole blocks, and the files they show, protected for some of those who read the item, with nothing of them reaching the others?

## Decision drivers

- **Nothing leaks,** as for passages (ADR-0088): no answer, export, search hit, version, live session, comment quote, governance excerpt, relation, starmap or impact node, album entry, copy or file download gives a protected block or its file to one who may not read it.
- **One model:** the protection of a block is the protection of a passage — the same readers, requests, deciders, notifications and card — so nothing about who reads it is new.
- **A file is content too:** veiling a picture's block while its file stays readable protects nothing.
- **Erasure:** a protection can be erased for good (ADR-0089), its blocks and their files with it.
- **Placeholder:** others see that something is protected there and whom to ask, not what it is or how large.
- **D4, D10:** relations derived from protected blocks must not tell an unreadable reader what the blocks point at.

## Considered options

1. **A per-block reference to the item's `Secret` (`Block.Secret`), one `Secret` per range; the files the blocks show are protected with it, gated in every read path and deleted with its erasure.**
2. **Veil only the block, leave the file readable.**
3. **Protect through the item: move the content into a child page restricted for people and groups (ADR-0078) and embed it.**
4. **Per-block encryption with separate events per block.**
5. **Passages only (the status quo).**

## Decision outcome

Chosen option: **1**.

### Representation

- A block carries `Secret`, the ID of a `Secret` of its item. Every block of a protected range names the same `Secret`; the range is one `Secret`, with one `By`, one list of readers and one list of requests, exactly as a passage's (ADR-0088). `SecretView`, and the item's `secrets` in the API, say `blocks: true` for such a protection.
- A `Secret` protects passages or blocks, never both. A protected block holds no protected passage, and a block with a passage is not protected whole.
- Protecting makes a range contiguous. Editing by one who reads every protection may afterwards move, split or delete the blocks of a range; a range that is no longer contiguous stays one protection (the editor keeps the `secret` of a block whose kind changes).
- No new event kind: protecting is `SecretSet` with the body carrying the blocks' `Secret`, in one batch; taking the protection off is `SecretRemoved` with the body without it; requests, answers and erasure are the events of ADR-0088 and ADR-0089.

### Files

- A file a protected block shows (image, drawing, video, BPMN model) is protected with the block's `Secret`, and so is every version of its stack (ADR-0103) — an older version of a drawing shows nearly the same picture, and a new version made later joins the stack and the protection.
- **The protection sticks to the file.** The item remembers each file a protected block showed (`Item.secretFiles`, derived from the log, the same live and on replay) until its `Secret` is taken off or erased. A reader who deletes the picture's block from the text does not thereby open its file.
- **One place only:** a protected file, or a version of its stack, is shown by no unprotected block and no block of another protection; such a body is refused, and protecting a block is refused while another, unprotected block shows the same file. This is the safer of the two rules considered. The alternative — "protected if any protected block shows it" — would leave a picture visibly open in one place of the text and veiled in another, and a reader of the open place would rightly expect to have it.
- **Where it is gated:** reading a file (`GET …/attachments/{id}`), listing an item's files and counting them, the album and its labels, the starmap's picture nodes and copy edges, an impact walk (a protected picture is no node and no start), the origin of a copy and its "newer original" (ADR-0055), the plan and clean-up of versions, labelling and deleting a file, and a file uploaded as a copy of it (a source one may not read is not found). Exports read files through the same session, so Word, print and Markdown archives hold no protected picture for such a reader.
- **Copies:** a protected file is copied into another item only with its whole item (ADR-0067), which takes its protections along (`CopySecrets`), so the copy is protected by a `Secret` of the same ID; it is protected from the moment it is recorded, before the copy's body shows it. Copying it alone — the album's copy and paste (ADR-0108), *Insert → From the album* — is refused even to a reader: such a copy would leave its protection.

### Veiling

For one who may not read a protection, each run of consecutive blocks of it becomes **one** placeholder block: a paragraph whose text is `▒▒▒▒▒▒▒▒` and whose `secret` names the protection. It reveals that something protected is there and who decides (through the item's `secrets`), and nothing else: not the kind of the blocks, their number, text, caption, table size, code language, file, target or engine. The kind was considered — "a protected picture" would read better — but a kind says what a page holds (a diagram of a process, a table of salaries), and the placeholder of a passage does not reveal even its length. The veiling applies wherever ADR-0088 veils a body: reading, listings, search, query selection, exports, MCP, the answers of commands, embeds, comment quotes, governance excerpts, earlier versions (shown only to those who read every protection), copies (only by those who read them all) and live sessions (only for those who read them all).

Relations derived from a protected block (mentions, embeds; D4) remain a function of the body, and each remembers the protections it came from when no unprotected block implies it too. Whoever reads none of them does not learn of the relation: not in the relations of either end, the embeds, the starmap, an impact walk, or the referrers named when trashing (D8 still holds: the reference still blocks the deletion; it is counted among those not shown). A mention of `@user` in a protected block notifies no one.

### Storage and erasure

- A sealed body keeps its protected blocks apart: they go whole into `secretBlocks`, sealed with the protection's own key (`{item}~{secret}`, ADR-0088); in the body sealed with the item's key each of them is a stand-in that names only the protection. Destroying that key leaves nothing of the blocks in any version — not even their kind.
- **Erasing** a protection of blocks (ADR-0089) removes its blocks from the body, destroys its key, veils what inline comments quoted of them, and **deletes the files** it protected, every version of their stacks, in the same batch: their content leaves the data directory once the batch is durable, as any deleted file's does (ADR-0045).
- Files are not re-encrypted under the protection's key: they stay encrypted with the item's key and are gated by access. Deletion on erasure is real erasure of their content. What stays, and is stated here honestly: the description of each file (name, size, hash, media type) remains in the log sealed with the item's key, as for every deleted file; backups taken before the erasure hold the files and the key until they age out; versions of the body from before the protection are withheld, not erased (ADR-0089).
- The format is codec version 38: blocks may carry `Secret`, and an event may carry `secretBlocks`. Versions 1 to 37 are still read.

### Who

As ADR-0088: those who may edit the item and read every protection of it protect; the one who protected and the readers read; the one who protected and the space's administrators decide, take off and erase. Only those who read every passage and block of an item change its text; others keep changing its title, fields, state, labels and comments.

### Interface

- **API:** `POST /api/items/{ref}/secrets` takes `fromBlock` and `toBlock` (from 0, both included) instead of `block`, `start` and `end`. Blocks carry `secret` in every body the API answers and accepts.
- **MCP:** the secrets routes stay unoffered (they change permissions, ADR-0012); `get_item` describes the placeholder block.
- **Page:** *Protect* offers, by what is selected: the selected words; the block clicked (a picture too) or the blocks the selection touches; and for a heading, its section — up to the next heading of the same or a higher level (`assets/protect.js`). A reader sees protected blocks marked at the side; one who may not read them sees a dark bar card that says the content is protected, who decides, and *Ask to read it*. The *Protected passages* card lists protections of blocks as "Protected blocks n".

### Consequences

- **Positive:** a heading with its section, a picture, a table or a diagram is protected in one step, with the readers and requests people know from passages, and a protected picture does not turn up anywhere else.
- **Negative / trade-offs accepted:** a protected file cannot be shown in two places of the text, one protected and one open; it is shown in protected blocks only.
- **Negative / trade-offs accepted:** a protected picture cannot be copied into another page on its own, even by its readers; they take the protection off first, or copy the whole page.
- **Negative / trade-offs accepted:** the files of a protection are encrypted with the item's key, not the protection's; their content is erased by deletion, and their descriptions stay in the log under the item's key.
- **Negative / trade-offs accepted:** a non-reader cannot tell one protected picture from a protected section; the placeholder says whom to ask, not what.
- **Negative / trade-offs accepted:** a restore from a backup into a directory in use (ADR-0124) protects the restored files that the restored body shows in protected blocks; a protected file that no block shows any more comes back open.
- **Follow-ups:** protecting cells of a table; protecting a file that no block shows; showing a protected picture's place in the album to its readers with a mark.

## Pros and cons of the options

### Option 2: veil the block, leave the file readable

Strongest form: no change to attachments, the album, the starmap or exports; the block is veiled where the text is read, and the file's random ID is hard to guess.

- Bad: the album, the starmap, the item's file list, exports and the impact view all list readable files by name and picture; a protected salary plan would appear in the album of everyone in the space. A secret that depends on an ID not being listed is not protected.

### Option 3: a child page restricted for people and groups

Strongest form: no new model; ADR-0078 and ADR-0022 already protect a page, its files and everything below it, and an embed shows it in place.

- Bad: a heading and its section become a separate page, the text a hole with an embed; the embed is shown "restricted" without saying whom to ask, and there are no requests to read it. It is ADR-0088's rejected option 3 for whole sections.

### Option 4: per-block encryption with separate events per block

Strongest form: each block is its own unit of protection and erasure, so a section's blocks can be granted or erased one by one.

- Bad: a section of ten blocks becomes ten protections with ten lists of readers and ten requests, which is what decision 2 rules out; new event kinds would duplicate `SecretSet`, `SecretRemoved` and the rest for no new behaviour.

### Option 5: passages only

Strongest form: no format change; the text of headings and paragraphs can already be protected word by word.

- Bad: pictures, tables, code, embeds and diagrams cannot be protected at all, and a section needs a passage per block.

## Links

- amends [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages: protections of whole blocks and their files)
- amends [ADR-0089](0089-a-protected-passage-is-erased-by-its-key.md) (erasure deletes the protected files)
- relates to ADR-0022 and ADR-0078 (permissions), ADR-0025 and ADR-0026 (format, keys), ADR-0045 (attachments), ADR-0054, ADR-0055, ADR-0090, ADR-0103, ADR-0108 (album, provenance, folders, versions, copy and paste), ADR-0067 (copying), ADR-0071 and ADR-0131 (exports), ADR-0121 (change feed), ADR-0157 and ADR-0159 (starmap, impact), ADR-0124 (restore)
