# ADR-0029: The security audit trail is its own append-only journal

- **Status:** Proposed
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer request 2026-10-17)
- **Open question:** How long are audit entries kept? They hold user IDs, usernames typed at failed logins and client addresses, which are personal data; erasing a person (ADR-0026's open question) and a retention period must be decided together.
- **Question checked:** 2026-10

## Context and problem statement

ADR-0013 requires a security audit trail: logins, failures, token creation and revocation, role changes. ADR-0027 wrote these as log lines only. Log lines rotate away, are not queryable from the product, and depend on how the operator collects stdout. The item history (ADR-0008) records content changes, but not logins, credentials or who granted an erasure role.

## Decision drivers

- Security events survive restarts and log rotation.
- Administrators can read them through the product.
- Recording an event never blocks or undoes the action it records.
- No secret ever enters the trail.

## Considered options

1. **Log lines only** (status quo of ADR-0027)
2. **Audit events in the item event log**
3. **A separate append-only journal**, `audit.wal`, with an administrator listing

## Decision outcome

Proposed option: **3.**

- `internal/audit` appends JSON entries (sequence number, time, event, actor, client address, subject, details) to `audit.wal` in the data directory, fsynced like every journal, and mirrors each entry as a log line.
- **Recorded:** login, failed and throttled login (with the username typed), logout, password change and failed change, token creation and revocation, account creation, space roles, group membership, system roles (including the first administrator, actor `system`), purge and redaction (with the reason).
- **Never recorded:** passwords, token and session secrets; a test checks the trail for them.
- **The action comes first.** An entry is recorded after the action succeeded; if it cannot be persisted, the action stands, the failure is logged with the entry's content, and counted.
- The newest 10 000 entries are kept in memory; `GET /api/audit` lists them newest first, paged with `before` and `limit`, for administrators. The journal keeps all.

### Consequences

- **Positive:** a durable, queryable record of security events, independent of the operator's log setup; no new mechanism (it reuses `internal/wal`).
- **Negative / trade-offs accepted:** one fsync per audited event, including failed logins. Each failed login already costs a PBKDF2 derivation of about 600 000 iterations, which dominates the fsync, so the trail adds no meaningful denial-of-service lever. The trail is append-only without retention, so it grows without bound (open question). Entries older than the in-memory window are only in the file.
- **Follow-ups / risks to watch:** retention and erasure of personal data in the trail; export to a SIEM; tamper evidence (hash chaining), since an operator with file access can rewrite the file.

## Pros and cons of the options

### Log lines only

Strongest form: operators already collect logs into systems built for retention, search and tamper-resistant storage; the product should not duplicate a SIEM.

- Good: nothing to build; integrates with existing tooling.
- Bad: nothing guarantees collection; not visible in the product; small installations have no log pipeline at all.

### In the item event log

Strongest form: one log, one ordering, one backup; audit entries interleave with content changes, which helps an investigation.

- Good: a single source of truth.
- Bad: failed logins would grow the log that every start replays; audit entries are not item state and would need their own retention and erasure rules inside an append-only content log; the crypto-shredding design (ADR-0026) does not fit them.

### Separate journal

- Good: independent retention, no replay cost, simple to list.
- Bad: a third file to back up with the data directory.

## Links

- implements the audit part of [ADR-0013](0013-authentication-and-security-boundary.md); extends [ADR-0027](0027-local-accounts-sessions-and-api-tokens.md); relates to ADR-0026 (erasure)
