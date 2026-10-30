# ADR-0152: A process engine is connected with one approval, and its people follow without a step of their own

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-30
- **Deciders:** Project initiators (requested 2026-10-30: connecting an atlas engine as ADR-0129 and ADR-0151 built it takes five credentials kept in three places — the engine's console, the server's environment and the operator's vault — and the requester asked that an administrator who manages both systems connect them with a few clicks. The requester decided: the procedure is the engine's general one for connected systems, of which the wiki is the first user; each person keeps a grant of their own and the engine keeps deciding who works a task, while the engine's administrator approves once for all of them; the administrator who connects decides what the wiki may read.)
- **Open question:** two points (see "Open questions"): whether a linked item whose task the wiki may no longer follow is detached — kept open and handed back to the wiki — as this record proposes, or closed; and whether connecting should also leave the wiki's address with the engine, for processes that link to its pages.
- **Question checked:** 2026-10

## Context and problem statement

To link an atlas engine's user tasks (ADR-0151) an operator today:

1. mints three API tokens at the engine — for processes, forms and sketches (ADR-0129), for the list of open tasks, and for the feed of task facts;
2. registers the wiki at the engine as an OAuth client, copying the redirect address the engine's page shows;
3. puts the three tokens, the client ID and the secret into the server's environment as `WWBB_ENGINE_<ID>_TOKEN`, `…_TASKLIST_TOKEN`, `…_EVENTS_TOKEN`, `…_CLIENT_ID` and `…_CLIENT_SECRET`, and restarts the server;
4. and then every person connects their own account under *Account*.

Every value is copied by a person, and a name typed wrong shows only as "missing" on the engine's page. Credentials came from the environment by an earlier decision (ADR-0077, ADR-0129): the operator, not an administrator of the wiki, decides what the server can reach, and no secret is kept in the data that backups carry.

The engine's side of a shorter path is recorded in atlas's decision record on connected systems (pblumer/atlas, `docs/adr`, numbered when it lands): a system names itself by the address of a client metadata document it publishes, an administrator of the engine approves a **system grant** for it — choosing which capabilities it gets and how far they reach —, and may **pre-consent** the grants its people ask for, which are then approved without a consent screen. This record decides the wiki's side.

Building on it surfaced a gap in ADR-0151: an open linked item may not be finished, removed or moved by anyone but the synchronisation (`item.Session.Transition`, `openTaskBelow`, `keepsLinkedTasks`). Removing its engine — or anything else that stops the synchronisation following the task — leaves such an item open for good.

## Decision drivers

- **One approval, nothing copied** (the requester): the administrator types the engine's address, starts the connection and approves it at the engine.
- **The engine decides who works a task** (ADR-0151, unchanged): every person's work runs on their own grant.
- **The operator keeps the last word:** a credential set in the environment still wins, and an installation that uses them keeps working unchanged.
- **No secret leaves the server,** and none is kept in plaintext or in a backup (ADR-0026, ADR-0034, ADR-0073).
- **A task the wiki no longer follows is not reported as done.** Narrowing what the wiki may read, or disconnecting, must not close items as completed.
- **The product stays one binary** (ADR-0009); nothing is needed beside `WWBB_PUBLIC_URL`.

## Considered options

1. **The first user of the engine's connected systems:** the wiki publishes a client metadata document with a key of its own, an administrator connects the engine from its page, and the system grant replaces the three tokens and the OAuth client.
2. **A ready-to-paste block:** the engine's page shows the five variables with the right names, as environment lines and as configuration-management variables, for the operator to fill in.
3. **Credentials entered on the engine's page,** kept sealed in the data directory instead of the environment.
4. **A pairing code** minted at the engine and pasted into the wiki once.

## Decision outcome

Chosen option: **1**, because it removes every copied value while the engine keeps deciding what the wiki reads and who works a task; option 2 is kept in a smaller form: when the environment sets a credential, the page names it.

### The wiki names itself

- **A client metadata document** at `GET /api/engines/client-metadata` (public): `client_id` (the document's own address under `WWBB_PUBLIC_URL`), `client_name` (the installation's name from the corporate design, ADR-0052, or its host), `redirect_uris` (the callback of every configured engine, `<WWBB_PUBLIC_URL>/api/engines/{id}/connect/callback`, which ADR-0151 already serves), `jwks` with the wiki's public key, and `token_endpoint_auth_method: private_key_jwt`.
- **The key** is an Ed25519 signing key (EdDSA) made on first use, its private half sealed with the server's keyring and never shown, exported or backed up; a restored server makes a new one, and the document says so to every engine at its next read. An administrator may replace it on the engine's page.
- Without `WWBB_PUBLIC_URL` there is no stable address to name the wiki by; the engine's page says so and offers no connection.

### Connecting an engine

- On the engine's page under **Administration → Process engines**, after its address is saved, **Connect to the engine** begins the authorization for the engine's resource `…/system` with PKCE and the capabilities the wiki uses: processes, forms and sketches (`diagrams`), the open tasks (`tasklist`) and the task facts (`events`). The engine's redirect returns to the callback above; the pending flow, held by the server as for a person's connection, says it is the system's.
- **What the engine approved** — who approved it, the capabilities with their reach, whether pre-consent stands — is read from the engine's `auth/me` with the new grant and shown on the page. A capability the approver left out is shown as not granted; its blocks and rounds say so as they say a missing variable today.
- **The grant is kept** as ADR-0151 keeps a person's: in `connections.wal`, under a subject of its own that no account can have, its refresh token sealed with a key of its own, not backed up, renewed serially. After a restore an administrator connects again, with one approval.
- **Disconnect** gives the grant back (RFC 7009) and forgets it. Removing the engine or changing its address does the same, as it ends people's connections today.
- **The environment wins.** For each capability, a credential set in the environment (`…_TOKEN`, `…_TASKLIST_TOKEN`, `…_EVENTS_TOKEN`) is used instead of the system grant, and the page names the variable that is used. With `…_CLIENT_ID` and `…_CLIENT_SECRET` set, people connect through that client as before; without them, through the metadata document.
- Connecting, changing and disconnecting are audited (`engine.system.connect`, `engine.system.disconnect`, ADR-0029).

### People follow

- **A person connects as in ADR-0151**, with the metadata document's `client_id` and a signed assertion instead of a secret.
- **When the engine reports pre-consent**, a person who opens a page that needs their connection — a linked task, a board or table with linked tasks, the task dialog — and has none is sent once through the engine with `prompt=none`: back within a moment when they have a session there, with the connection made and no screen in between. Without a session the engine answers `login_required`, and the page shows the *Connect* button of today. A person who disconnected is not sent again on their own pages; the engine also remembers that they declined.
- What each person sees is still their own task list at the engine, read with their own grant (ADR-0151, "Who sees a linked task").

### A task the wiki no longer follows is detached, not closed

- **With a system grant, a task that leaves the list is ended only on the engine's word:** a `completed` or `cancelled` fact on the feed, or the engine's answer that it ended when the task is read by its key. A task the engine answers as not found — out of the reach the approver chose, or no longer readable at all — is **detached**.
- **Detached** means: the item keeps its origin and its history, is no longer synchronised, and is handed back to the wiki — the engine no longer owns its title and fields, and people may finish, restrict, move and remove it as any item (lifting the refusals of ADR-0151 for it). It keeps the readers it had; its page says it is no longer followed and why. A task that comes back into reach while still open is linked again to the same item.
- **Disconnecting or removing an engine detaches all its open linked items** in one batch, which closes the gap above. An administrator may close detached items in bulk from the engine's page.
- Without a system grant — credentials from the environment, no feed — a comparison keeps closing a vanished task as completed, as ADR-0151 decided; the operator who chose that setup chose that behaviour.

### Routes

- `GET /api/engines/client-metadata` (public).
- `POST /api/engines/{id}/system/connect` and `DELETE /api/engines/{id}/system` (administrators): begin a connection and end it. The callback is ADR-0151's.
- `POST /api/engines/{id}/system/key` (administrators): replace the wiki's key.
- The engine's answer to administrators gains `system`: connected, approver, capabilities with their reach, pre-consent, since, and which capabilities come from the environment.

### As built

Nothing yet. It waits for the engine's side; suggested order, each step usable on its own:

1. Detaching: the item's detached state (a new event, so a new codec version), lifting ADR-0151's refusals for it, detaching an engine's open items when it is removed or its address changes, closing detached items in bulk. This step needs nothing from the engine and closes the gap now.
2. The metadata document, the key and `private_key_jwt` for people's connections.
3. The system grant: connecting, keeping, renewing, disconnecting; using it per capability where the environment sets nothing; ending a task only on the engine's word.
4. Pre-consent: the silent connection with `prompt=none`.

### Consequences

- **Positive:** an engine is connected from its page with one approval at the engine; no token, client ID or secret is copied by anyone; an operator who prefers the environment changes nothing; people with a session at the engine are connected without noticing; narrowing or ending a connection no longer reports work as done, and a removed engine no longer leaves items nobody may finish.
- **Negative / trade-offs accepted:** an administrator of the wiki, not only the operator, can now give the server a credential — but only one an administrator of the engine approved, for the reach they chose; the system grant is not in backups, so a restored server is disconnected until an administrator connects it again; the grant carries the engine administrator who approved it and ends with their role, which disconnects the wiki until someone approves again; a detached item has to be finished by hand.
- **Follow-ups / risks to watch:** the engine's side rests on an IETF draft for the metadata document, which may change; the silent connection is a top-level redirect, so it is made on a page load, never in the background of an open page.

## Pros and cons of the options

### Option 2: a ready-to-paste block

Strongest form: nothing changes in the trust model — the operator still decides everything — and the wiki already knows every name and the redirect address, so it can print exactly what to set, for the environment and for configuration management alike; a day's work.

- Good: no wrong names; nothing new to secure.
- Bad: five values are still made at the engine and copied, a restart is still needed, and the people still connect one by one; the requester asked for fewer steps, not better instructions.

### Option 3: credentials entered on the engine's page

Strongest form: one place instead of three, no restart, kept sealed as people's refresh tokens already are.

- Good: no environment, no vault.
- Bad: the five values are still minted and copied by hand; static tokens without an approver stand until someone remembers them; nothing narrows them but the person who minted them.

### Option 4: a pairing code

Strongest form: one value copied once; simple on both sides.

- Good: little to build.
- Bad: still a secret carried by a person, the one that unlocks the rest; the engine's record rejects it in favour of a standard procedure every system can implement.

## Open questions

- **Detached or closed.** This record detaches a task the wiki may no longer follow: open, handed back to the wiki, marked. Closing it would keep boards tidy but say the work is done when nobody knows; hiding it would hide its comments. To be confirmed by the requester.
- **The wiki's address at the engine.** Connecting could also leave the wiki's public address at the engine as an installation constant, so processes link to its pages without anyone typing it again. Not decided here; it would write into the engine's configuration and needs the engine's consent for that.

## Links

- extends ADR-0151 (linked tasks: connecting a person, who sees a task, learning what is open) and ADR-0129 (process engines)
- revisits, for engines that support it, ADR-0077's and ADR-0129's choice of credentials from the environment, which stays the operator's override
- relates to ADR-0026 (sealing), ADR-0029 (audit), ADR-0034 and ADR-0073 (backups), ADR-0068 (the wiki as an OAuth server for assistants)
- the engine's side: atlas's decision record on connected systems (github.com/pblumer/atlas, `docs/adr`), building on its ADR-0200 and ADR-0510
