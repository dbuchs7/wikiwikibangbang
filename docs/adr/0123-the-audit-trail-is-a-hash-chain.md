# ADR-0123: The audit trail is a hash chain

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for hash chaining of the audit trail, 2026-10-26)

## Context and problem statement

The audit trail (ADR-0029) is the evidence of who did what, for example who gave the installation the official look (ADR-0120). It is a file in the data directory. Anyone with access to that file, such as an operator or an intruder on the server, can change, remove or insert an entry, and nothing shows it. ADR-0029 named this as an open risk.

The question is how a reader can tell that the trail is the one that was recorded.

## Decision drivers

- A change, removal or insertion of an entry is found.
- Expiry after ten years (ADR-0029) stays possible, and is told apart from a removal.
- Journals written before this decision keep working.
- No service outside the installation is needed, and no fees.
- The limits are stated, not hidden.

## Considered options

1. A hash chain over the entries, its newest link witnessed by the log lines.
2. An HMAC chain with a key from the environment.
3. Timestamps from an external time-stamping authority (RFC 3161).
4. Shipping the entries to a write-once store or a SIEM only.

## Decision outcome

Chosen option: **1**, because it finds every change inside the journal without a secret and without a service, and its one gap, a journal rewritten as a whole, is closed by the log lines, which operators already keep apart from the data directory.

### The chain

- Every entry carries `prev`: the SHA-256 of the record before it, exactly as stored in `audit.wal`.
- The first entry of a new journal carries 64 zeros.
- In a journal from before this decision, the first new entry names the last older one. The older entries count as *unchained*: only the last of them is covered.
- Each entry's log line carries its number and its own hash (`seq`, `hash`). Where log lines are collected outside the server, they witness every link.

### Expiry

- The expiry marker (`audit.expired`) carries in `hash` the link of the last entry it removed. The next entry names that value, so the chain is checkable from the marker on.
- A marker is accepted only as the first entry, and only for entries older than the retention period. Its cutoff (`before`) must not be later than ten years before the check, and the entry after it must not be older than the cutoff.
- Thus whoever writes a marker can drop only what expiry would drop anyway.

### Checking

- `GET /api/audit/verify` (administrators) and the button *Check the chain* in Administration › Audit read the whole journal.
- The result names how many entries were checked, how many are unchained, the newest entry's number and hash, and, if the chain is broken, the first entry that breaks it and why:
  - a missing number;
  - a start cut off without a marker;
  - a marker in the middle;
  - a marker that removed young entries;
  - an entry that does not name the one before it;
  - an entry without a link after the chain began.
- Every start checks the chain. A broken chain does not stop the start: it records `audit.chain.broken` with the entry and the reason, so it shows in the trail itself and in the log.

### Consequences

- **Positive:** a change, removal or insertion in the middle of the trail, and a removal at its start, are found by anyone who can read the file, without a secret.
- **Negative / trade-offs accepted:**
  - A journal cut at its end, or rewritten as a whole with new links, passes the check. Only a comparison of the newest hash with the one witnessed in the log lines, or noted elsewhere, finds it. Where no one keeps the log lines apart from the server, the chain shows changes that were made carelessly, not those made with care.
  - Entries from before this decision are covered only at the last one.
  - A clock set more than the retention period ahead at an expiry makes a later check report a marker that removed young entries. That is a false alarm in that case, and the right alarm in every other.
  - Each record grows by about 75 bytes, and every start hashes the whole journal.
- **Follow-ups / risks to watch:**
  - An HMAC chain with a key from the environment (option 2) would stop someone with the file but not the key, if a deployment keeps the two apart.
  - Time-stamping the newest hash periodically with an external authority (option 3) would anchor the chain without relying on log collection.
  - A daily check with an alert.

## Pros and cons of the options

### Option 1: hash chain, witnessed by log lines

Strongest form: no secret to keep and no service to depend on; the evidence can be checked by anyone with the file; the log lines that already leave the server carry the anchor.

- Good: simple, open, no key management; finds every change in the middle.
- Bad: on its own, a rewrite as a whole passes; it relies on log lines kept apart.

### Option 2: HMAC chain with a key from the environment

Strongest form: without the key, no one can forge a link, even with the whole file.

- Good: stops a forger who has the file but not the key.
- Bad: whoever runs the server usually has both; a lost key makes the chain uncheckable; a checker needs the secret, so the evidence cannot be shown to third parties without disclosing it.

### Option 3: an external time-stamping authority

Strongest form: a third party signs the newest hash with a time, which proves that the trail up to then existed in that form.

- Good: the strongest anchor; independent of the operator.
- Bad: a service outside the installation, often with fees or limits, and network access from the server; it fails where the installation is offline.

### Option 4: a write-once store or a SIEM only

Strongest form: storage built for this, outside the operator's reach.

- Good: the right place for evidence in larger organisations.
- Bad: not every installation has one; the product could not check its own trail.

## Links

- amends [ADR-0029](0029-persisted-security-audit-trail.md) (the tamper evidence it named as a follow-up)
- relates to [ADR-0120](0120-the-swiss-theme-follows-the-confederations-design-system.md) (the entry that records the official look) and [ADR-0122](0122-the-audit-listing-reaches-the-whole-journal.md) (listing the whole journal)
