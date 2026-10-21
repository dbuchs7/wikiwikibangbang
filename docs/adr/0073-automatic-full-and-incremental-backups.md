# ADR-0073: Automatic full and incremental backups to a destination

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked for automatic backups with a system default of every 14 days, up to ten kept versions, full and incremental backups and a destination such as FTP, a file server, a NAS or the cloud; and chose daily incremental backups, set under Administration, with secrets from the environment, to a local path, WebDAV, S3-compatible storage, FTPS and SFTP, 2026-10-21)

## Context and problem statement

A backup is taken today by an administrator who downloads it (ADR-0034) and keeps it somewhere. Nothing happens unless somebody remembers, and every backup holds everything again: the whole event log, every attached file. How does the server take backups on its own, store them away from its data, keep a bounded number, and avoid copying everything every day?

## Decision drivers

- A restore must still be possible with nothing but the backup files, the passphrase and the binary (ADR-0034): no index, no database of backups.
- Secrets — the passphrase and a destination's credentials — must not be in the data or in its backups: a backup that holds the key to the other backups protects nothing.
- The logs are append-only (ADR-0008), and an attachment's file never changes once written (ADR-0045); most of what changes between two backups is appended bytes and new files.
- One binary, standard library first (ADR-0009).
- A backup that is half written, or a chain with a gap, must never look restorable.

## Considered options

1. Full backups only, on a schedule.
2. Full backups with incremental ones that each build on the backup before (a chain).
3. Full backups with differential ones that each build on the last full backup.

## Decision outcome

Chosen option: **2**, because the logs only grow: the bytes appended since the last backup are exactly what an incremental backup must hold, and a day's increment is small even when the full backup is large.

**Format.** The archive format (ADR-0034) gets version 2. Its manifest names the backup (`id`), its `kind` (`full` or `incremental`), the `chain` (the ID of the full backup it starts from), the `base` it builds on and its `seq` in the chain, and — as before — lists every file of the data directory with size and SHA-256, now *as it is after the backup is applied*. An incremental backup holds, for each file:

- of a log that is still the base's file: nothing;
- of a log whose first bytes hash to the base's file: only the appended bytes, in a tar entry with the PAX record `WWBB.from` giving the offset;
- of an attachment's file the base already lists with the same size: nothing;
- of anything else (a log that was rewritten, as when keys are destroyed or audit entries expire): the whole file.

A file the base lists and the new manifest does not was deleted and is removed on restore. Format 1 backups restore as full backups.

**Restore.** `RestoreChain` applies a full backup and the incremental ones after it, in order, into a staging directory: it checks that each builds on the one before (`base`, `chain`), that a continued file has exactly the base's length, and after each backup that the files are exactly those its manifest lists, with their hashes; the result must open (`Inspect`) before it is moved into place. The `restore` command takes several `-from` files in order, a directory of automatic backups (the newest version, or the backup `-at` names, with the ones it builds on), or one incremental backup, whose chain it finds beside it.

**Names.** A stored backup is named `wikiwikibangbang-<time>-<chain>-<seq>-full|inc.wwbb`, so a destination can be read — grouped into versions, checked for gaps, pruned — without decrypting anything.

**Schedule.** The schedule is a site setting (journaled like the design, ADR-0052): switched on or off, the destination, a full backup every `fullDays` (default 14), an incremental one every `incrementalDays` (default 1, 0 for none), and `keep` versions (default 3, at most 10). A *version* is a full backup with its incremental backups; after each backup the versions older than the newest `keep` are deleted, each with all of its files. The server checks every 15 minutes; a backup is due a check early so a daily one does not drift; after a failure it waits an hour before trying again. Administrators can take a full or incremental backup at once and test a destination; the page shows the next backup, the last 20 runs and the versions stored. Each run is audited (`backup.create`, `backup.failed`), and changes to the schedule (`backup.change`).

**State.** Which backup the next incremental one builds on is kept in `backups.json` beside the data, not in the data's backups: a restored instance has none and starts with a full backup. An incremental backup is taken only when the whole chain up to its base is still at the destination, and the destination is the one the chain was written to; otherwise the backup becomes a full one.

**Secrets.** The passphrase comes from `WWBB_BACKUP_PASSPHRASE` (at least 12 characters); without it nothing is backed up and the page says so. A destination address that holds a password is refused. A remote destination's credentials come from the environment as well: `WWBB_BACKUP_USERNAME` (unless the address names the user) and `WWBB_BACKUP_PASSWORD`, `WWBB_BACKUP_ACCESS_KEY` and `WWBB_BACKUP_SECRET_KEY` for S3, `WWBB_BACKUP_SSH_KEY` (a key file) and `WWBB_BACKUP_SSH_HOST_KEY` for SFTP, and `WWBB_BACKUP_CA_FILE` for a server whose certificate the system does not trust, such as a NAS that signs its own.

**Destinations.** The kind is named by the address; only encrypted connections are used, so `http://` and `ftp://` are refused:

| Address | Kind | How a backup is stored |
|---|---|---|
| `/path` or `file:///path` | a directory of the server: a mounted file server or NAS, or a volume in a container | written as `name.part`, renamed when complete |
| `https://user@host/folder` | WebDAV (Basic authentication over TLS) | `PUT` of `name.part`, then `MOVE` |
| `s3://bucket/folder?endpoint=https://…&region=…` | S3-compatible storage, path-style, signature version 4 | one `PUT`, or a multipart upload in parts of 16 MiB that is aborted on failure; the object appears only when complete |
| `ftps://user@host/folder` | FTP with explicit TLS (`AUTH TLS`), data connections encrypted (`PROT P`) and offering to resume the control connection's TLS session, passive mode (`EPSV`, then `PASV`) | `STOR` of `name.part`, then `RNFR`/`RNTO` |
| `sftp://user@host/folder` | SFTP version 3 over SSH | written as `name.part`, renamed when complete |

A directory must exist — a missing mount fails the backup instead of filling the server's own disk — and lie outside the data directory, where backups would back up backups and be lost with the data; a remote folder must exist too. An SFTP server must present the host key whose SHA-256 fingerprint `WWBB_BACKUP_SSH_HOST_KEY` holds; without it the connection is refused and the error names the fingerprint the server presented, to be checked and set — trust on first use would accept whoever answers first. A backup is streamed to the destination without knowing its length; only S3 needs a length per request, hence its parts held in memory. `restore -from` takes a remote address too, with the credentials from the environment.

SFTP needs an SSH implementation; it comes from `golang.org/x/crypto/ssh` (BSD-3-Clause, maintained by the Go project), the project's first dependency outside OIDC. The SFTP protocol on top, a few packet types, is implemented here, as are WebDAV, the S3 signature and FTPS, on the standard library.

### Consequences

- **Positive:** backups happen without anybody remembering; a daily backup costs about what changed that day; the files at the destination explain themselves; a restore needs no server and no index.
- **Negative / trade-offs accepted:** restoring the newest state needs every backup of its chain — a lost file in the middle loses the backups after it (which is why the newest `keep` versions are kept whole, and why a gap makes the next backup a full one); each incremental backup reads the logs once more to compare their start; two instances writing to the same directory would prune each other's versions.
- **Follow-ups / risks to watch:** the remote destinations are tested against fakes that follow the protocols, not against every server: a WebDAV server that refuses uploads of unknown length (411), an FTPS server that insists on a resumption Go's TLS does not offer, or an SSH key protected by a passphrase are not supported yet; a notification to administrators when backups fail repeatedly; a destination that is very slow to list holds the administration page for up to 15 seconds.

## Pros and cons of the options

### Option 1: full backups only

Strongest form: one file is one restorable state; nothing can break a chain.

- Good: simplest; every file stands alone.
- Bad: copies the whole log and every attachment each time; a daily schedule multiplies storage and transfer by the data's size.

### Option 2: incremental chain

Strongest form: matches append-only logs exactly — the increment is the appended bytes.

- Good: small daily backups; restore by applying in order.
- Bad: a restore needs the whole chain; a gap breaks the backups after it.

### Option 3: differential backups

Strongest form: a restore needs only two files, the full backup and the newest differential.

- Good: shorter restore path; a lost differential costs only itself.
- Bad: each differential grows with every day since the full backup, so by day 13 it holds almost two weeks of changes — every day.

## Links

- extends ADR-0034 (the encrypted archive) and ADR-0016 (what a backup holds)
- relies on ADR-0008 (append-only logs) and ADR-0045 (attachment files never change)
