# ADR-0151: Credentials of engines and connectors change without a restart

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-29
- **Deciders:** Project initiators (the maintainer asked on 2026-10-29 that a new token no longer requires restarting the server, and chose both a file the operator keeps and a value an administrator stores on the administration pages, for process engines and connectors alike); proposed for the maintainer's acceptance
- **Open question:** Should a stored credential be bound to the address it was stored for, so that an administrator who changes an engine's or a connector's address has to store it again, and should stored credentials be part of backups after all? (see "Open questions")
- **Question checked:** 2026-10

## Context and problem statement

A connector's credential comes from the environment variable `WWBB_CONNECTOR_<ID>_TOKEN` (ADR-0077, accepted, and `WWBB_CONNECTOR_<ID>_EMAIL` for an Atlassian import, ADR-0128), a process engine's from `WWBB_ENGINE_<ID>_TOKEN` (ADR-0129). ADR-0077 states the rule: "Credentials are secrets: they come from the server's environment, never from the settings, the page or the data, as for backups (ADR-0073)."

A process cannot see a change to its environment after it has started. Every new credential therefore means a restart, which ends every live editing session (ADR-0010) and every request in flight, for everybody. Credentials change for ordinary reasons:

- they expire, and atlas warns 30 days ahead;
- they are rotated;
- they are replaced because what they may reach changes. On 2026-10-28 the architecture sketches of ADR-0146 needed an atlas token with a reach, which only a new token has. Restarting the wiki to put it in place was the only way.

How does a credential change take effect while the server runs, without giving up what the environment rule protects?

## Decision drivers

- A new credential takes effect at its next use, without a restart.
- The operator keeps the authority ADR-0077 gives them: what the environment sets is not overridden from the browser.
- A credential never reaches a browser, a log line, the audit trail, the settings journal (`site.wal`) or an answer of the API.
- A credential never reaches disk in plaintext, as content does not (ADR-0026). A replaced or removed credential leaves no readable copy behind.
- A backup does not carry the credentials of other systems, as it does not carry its own passphrase (ADR-0073). A stolen backup should cost the wiki's content, not access to every system the wiki reads.
- One binary, no new dependency (ADR-0009); every new bound is a budget or classified (ADR-0015).

## Considered options

1. **The environment only**, as today: a restart for every new credential.
2. **A file the operator keeps**, named by `<NAME>_FILE`, read at each use.
3. **A value stored in the data directory**, set by an administrator on the administration pages, sealed.
4. **Options 2 and 3 together**, with the environment, a variable or a file, taking precedence over a stored value.
5. **A reload on a signal**, re-reading an environment file the server is pointed at.
6. **An external secret store**, such as HashiCorp Vault or a cloud key management service.

## Decision outcome

Chosen option: **4**, because it is what the maintainer asked for. The file serves operators who keep secrets in an orchestrator or a secret manager. The stored value serves installations where the administrator is the operator. The environment's precedence keeps the operator's authority wherever they choose to exercise it.

This supersedes the credential rule of ADR-0077 and amends the credential paragraph of ADR-0129. ADR-0073's secrets, the backup passphrase and a destination's credentials, stay in the environment.

### One name, three places

A credential keeps the name it has: its variable, `WWBB_ENGINE_<ID>_TOKEN`, `WWBB_CONNECTOR_<ID>_TOKEN` or `WWBB_CONNECTOR_<ID>_EMAIL`. It is read, at each use, from the first of these that is set:

1. **The variable** `<NAME>`, as today.
2. **The file** that the variable `<NAME>_FILE` names, as Docker and Kubernetes provide secrets:
   - the file is read again at each use, so replacing it takes effect at once;
   - its content without surrounding white space is the credential, a single line of at most 8 KiB;
   - a file that cannot be read, is empty, is longer or holds a control character makes the credential fail with a message naming the variable `<NAME>_FILE`, never the content.
3. **The stored value**, set on the administration page of the engine or the connector.

Setting both `<NAME>` and `<NAME>_FILE` is an error that the administration page shows: neither is guessed at. When the environment sets a credential, a stored value is not used. The page says that the environment sets it and offers no field for it. A stored value from before stays until it is removed, shown as overridden.

### The stored value

- **Who sets it:** administrators only, through `PUT /api/credentials/{name}` with `{"value": …}`, and they remove it through `DELETE /api/credentials/{name}`.
- **Which names are accepted:** only the names of engines and connectors that exist: a token for each, and an e-mail address for a connector of the kind atlassian. Any other name answers 404.
- **When it is refused:**
  - while the environment sets the name: 409, saying so;
  - a value that is empty, longer than 8 KiB or holds a control character: 400.
- **What is never shown:** the value is never answered, logged or audited. An engine's and a connector's settings say where the credential comes from (`variable`, `file`, `stored` or none), when it was stored and by whom, and what is wrong with it.
- **Audit:** storing and removing are recorded as `credential.set` and `credential.remove`, naming the credential and never its value.
- **Removal with its owner:** removing an engine or a connector removes its stored credentials, unless another engine or connector has the same name for one. Two IDs that differ only in characters the name replaces by `_` already share a variable today.
- **Not an MCP tool**, as nothing about credentials is (ADR-0012).

### Sealed, and gone when replaced

- **The file:** stored values are kept in `credentials.wal` in the data directory, as one record that every change rewrites. The record holds the values sealed with AES-256-GCM under a key of their own in `keys.wal` (ADR-0026). Neither the values nor their names are in plaintext on disk; a test reads the files back to show it.
- **A new key for every change:** each change takes a new key, rewrites `credentials.wal` under it and then destroys the old key, which rewrites `keys.wal` without it. A copy of an older `credentials.wal`, from a snapshot of the file system say, can then no longer be opened.
- **After a crash:** a crash between the steps leaves a key that no `credentials.wal` uses, and the next start destroys it, as it destroys the keys of purged items (ADR-0089).
- **In memory:** the values are held in memory from the start on, as the environment's are.
- **Not in backups:** `credentials.wal` is listed among the files a backup leaves out, with its reason. After a restore the stored credentials are stored again; the page shows them as missing.

### Showing it

On the administration page, the engine's editor and the connector's editor say where each credential comes from:

| Where it comes from | What the editor shows and offers |
|---|---|
| The variable or the file | That the environment sets it, and nothing to type into |
| Stored | When and by whom it was stored, a field to replace it and a button to remove it |
| Nothing | A field to store it, beside the names of the variable and the file that would set it instead |

An error, such as both variable and file set or a file that cannot be read, is shown in the place of that line. The field is a password field that is emptied once the value is stored.

### Consequences

- **Positive:**
  - A new token takes effect at its next use: at once from a file or a stored value, with no restart and no interrupted session.
  - An operator who keeps secrets in an orchestrator replaces a file. An administrator who is also the operator types the token on the page.
  - Setting the variable or the file still locks the credential against the browser, so the operator keeps the authority ADR-0077 gave them.
  - The stored values are sealed; a replaced value is unreadable once its key is destroyed; a backup carries none of them.
- **Negative / trade-offs accepted:**
  - The data directory now holds credentials of other systems, sealed. Whoever holds both `keys.wal` and `credentials.wal`, which is whoever holds the data directory, can read them, as they can read its content.
  - A restore from a backup brings no stored credential back; they are stored again.
  - An administrator can store a credential for any engine or connector, and can change the address it is sent to, which was true of a variable already (open question 1).
  - A variable that is set still needs a restart to change; moving it to a file or to the page needs one last restart, to take the variable away.
  - A file is read at each use: three small reads per architecture sketch shown (ADR-0146), from the page cache.
- **Follow-ups / risks to watch:**
  - Other secrets in the environment, such as the mail server's password or an identity provider's client secret, are not covered; they are read once at start. The same rule, a variable or a file, could reach them.
  - A warning before a stored token expires, if engines say when their tokens do.

## Pros and cons of the options

### Option 1: the environment only

Strongest form: the secret never touches the wiki's storage. The operator's tooling, such as systemd, Docker or Kubernetes, owns it end to end. The rule is one sentence, and ADR-0077 accepted it.

- Good: nothing new to secure; the operator's authority is total.
- Bad: every rotation, expiry or change of reach is a restart for everybody. That is what the maintainer asked to end. The rule's protection, the secret kept out of the settings and the data, is kept by options 2 and 4 without the restart.

### Option 2: a file only

Strongest form: the secret stays outside the data directory and its backups, as option 1 keeps it, and Kubernetes and Docker update mounted secrets in place. It takes one line of code per use, with no new store, no key, no page and no new attack surface in the browser.

- Good: everything option 1 protects, without the restart.
- Bad: changing the secret still needs access to the host or the orchestrator. Where the administrator is the operator, and the server runs as a single container, that is the restart's burden in another form. The maintainer asked for the page as well.

### Option 3: a stored value only

Strongest form: the administrator changes a token where they configure the engine. Nothing outside the wiki is touched, and the change is audited.

- Good: the fastest path for the common small installation.
- Bad: the operator loses the authority option 1 gave them, and with it a way to keep credentials out of the data entirely. Option 4 keeps both.

### Option 5: a reload on a signal

Strongest form: the server re-reads an environment file on `SIGHUP`, as many daemons re-read their configuration. Nothing new is stored, and the file can hold every secret.

- Good: one mechanism for every secret, including those read only at start.
- Bad:
  - It is option 2 with an extra step that needs a shell on the host.
  - A signal reloads everything at once: a typo in one secret breaks them all.
  - Nothing on the page tells an administrator whether the reload took. A file read at each use reports per credential.

### Option 6: an external secret store

Strongest form: rotation, leases, auditing and access policy belong to a system built for them, and the wiki fetches each secret when it needs it.

- Good: the strongest handling of secrets where an organisation already runs such a store.
- Bad:
  - It needs a client per store and a network call per use, against ADR-0009's single dependency-free binary.
  - The wiki would need a credential for the store itself, which is the same problem one level up.
  - A store's agent can already write a file, which option 2 reads.

## Open questions

1. **Binding a stored credential to its address.** An administrator who changes an engine's or a connector's address sends its credential to the new address, whether the credential is stored or set in the environment. A stored value could be bound to the address it was stored for: changing the address would then remove it, and the administrator would store it again. That would protect stored values against an administrator who should not see them, but not values from the environment. It would also need the address of every kind of connector, which differ. Whether that protection is worth the rule is the maintainer's question.
2. **Backups.** Stored credentials are left out of backups, so a stolen backup does not reach other systems, at the cost of storing them again after a restore. An organisation that restores often may prefer them in, sealed under the backup's passphrase as everything else is.

## Links

- supersedes the credential rule of [ADR-0077](0077-connectors-keep-catalogs-in-step-with-their-sources.md); amends [ADR-0129](0129-bpmn-diagrams-and-processes-from-a-process-engine.md) and [ADR-0128](0128-the-atlassian-import-as-a-connector.md) on their credentials
- relates to [ADR-0009](0009-single-binary-with-embedded-web-ui.md) (one binary), [ADR-0012](0012-mcp-adapter-over-http-api.md) (MCP), [ADR-0015](0015-resource-budgets-and-paged-listings.md) (budgets), [ADR-0026](0026-crypto-shredding-of-item-content.md) (keys and erasure), [ADR-0029](0029-persisted-security-audit-trail.md) (audit), [ADR-0073](0073-automatic-full-and-incremental-backups.md) (secrets and backups), [ADR-0146](0146-architecture-sketches-of-releases-from-an-engine.md) (the token that started this)
