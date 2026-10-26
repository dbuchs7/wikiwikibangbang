# ADR-0034: Backups are encrypted with a passphrase and cut from the logs while the server runs

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-17
- **Deciders:** Project initiators (the maintainer asked to continue with the next feature, 2026-10-17)

## Context and problem statement

ADR-0016 decides that a backup is an allowlist of stores, taken as a consistent point-in-time read that does not block the writer, with secrets "excluded or exported only on an explicit, separate request", and restore tested end to end. It leaves the mechanism open. Two facts of this design shape it: item content is encrypted with per-item keys (ADR-0026), so a backup without the keys restores titles and structure but no content; and principals are permanent user IDs (ADR-0027), so a backup without the accounts restores items whose roles point at nobody. A backup without secrets is therefore not a backup.

## Decision drivers

- A backup restores a working instance on another machine.
- Taking it does not stop or slow down the writer noticeably.
- The file can be stored off-site without becoming the weakest copy of the data.
- Nothing ends up in a backup by accident (ADR-0016's allowlist).
- Restore refuses anything it did not write.

## Considered options

1. **Copy the data directory with the server stopped** (operator's tools)
2. **A plain archive of the logs**, protected only by where it is stored
3. **An archive of the logs encrypted with a passphrase given at backup time**
4. **An archive without keys and accounts**, which ADR-0016's wording suggests

## Decision outcome

Chosen option: **3**. The passphrase is the explicit request ADR-0016 asks for: the backup holds the secrets, and only the person who knows the passphrase can use them.

- **Consistency without stopping:** the logs are append-only with checksummed records (ADR-0025), so the first N bytes of a log are a complete log. Under the writer's lock the server records the durable length of the event log and of the keys file and opens both; the copy then runs without the lock. A later rewrite of a file (a destroyed key, expired audit entries) replaces its name, not the content being read. The accounts and audit logs are written independently of items and are read to the end of their last complete record. A test takes backups while another goroutine writes and checks that every restored item can be read with its key.
- **Allowlist:** `storage.BackupFiles` names the four logs; `storage.NotBackedUp` names what is left out and why (the directory lock). A test fails when the data directory holds a file in neither list.
- **Format:** a tar archive of the files followed by a manifest (format version, time, build, size and SHA-256 per file), encrypted in 64 KiB chunks with AES-256-GCM under a key derived by PBKDF2-SHA256 (600,000 iterations) from the passphrase. The header (parameters and salt) is authenticated with every chunk; a chunk counter and a last-chunk flag in the nonce refuse reordered or truncated files. Passphrases have at least 12 characters.
- **Taking a backup:** `POST /api/backup` with the passphrase in the body, administrators only, recorded in the audit trail as `backup.create`; the administration page offers it as a download.
- **Restore:** `wikiwikibangbang restore -from FILE -data-dir DIR` with the passphrase from `WWBB_BACKUP_PASSPHRASE` or a file, never a flag. It writes into a staging directory, accepts only allowlisted files, checks each against the manifest, opens the result as a data directory and only then moves it into place; the target must not exist or be empty.
- **Tested end to end:** in CI the browser test downloads a backup, and the job restores it with the binary and checks it.

### Consequences

- **Positive:** backups of a running server, safe to store off-site, restorable to a working instance with users, roles and content.
- **Negative / trade-offs accepted:** **erasure does not reach backups.** A backup taken before a purge holds the purged item's key, so whoever has the file and the passphrase can read the content until the backup is destroyed. Erasure through destroyed keys (ADR-0026) is complete for the live system only; operators must expire backups to honour deletion requests. A lost passphrase makes a backup worthless. On Windows a file held open by a running backup cannot be replaced, so a purge during a backup may fail and must be repeated.
- **Follow-ups / risks to watch:** scheduled backups by the server itself; a per-backup random key wrapped for several operators instead of one shared passphrase; the metrics half of ADR-0016.

## Pros and cons of the options

### Copy the stopped data directory

Strongest form: no code, no format, no crypto; every operator knows how.

- Good: simple and obviously complete.
- Bad: downtime; the copy is unencrypted and often lands on less protected storage.

### Plain archive

Strongest form: encryption belongs to the storage layer (encrypted disks, object storage with server-side encryption); a second layer adds a passphrase to lose.

- Good: no passphrase to manage.
- Bad: the archive holds every key and every account hash in the clear; one misplaced copy undoes ADR-0026 and ADR-0027.

### Archive without keys and accounts

Strongest form: the letter of ADR-0016 — secrets never leave the server.

- Good: a stolen backup reveals no content.
- Bad: it cannot be restored to a working instance: content stays encrypted under keys that are gone, and every role points at user IDs that no longer exist.

## Links

- refines [ADR-0016](0016-metrics-and-backup.md); relies on [ADR-0025](0025-event-log-file-and-in-memory-state.md); limits [ADR-0026](0026-crypto-shredding-of-item-content.md) as described; audited per [ADR-0029](0029-persisted-security-audit-trail.md)
- extended by [ADR-0124](0124-restoring-spaces-from-a-backup.md) (restoring single spaces from a backup)
