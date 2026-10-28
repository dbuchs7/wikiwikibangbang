# ADR-0150: The wiki watches its storage

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-28
- **Deciders:** Project initiators (the maintainer asked for a control of the disk space and chose to measure, show, warn and protect, with warnings below 15 % or 5 GB free and a critical level below 5 % or 1 GB, 2026-10-28)

## Context and problem statement

The wiki keeps everything in its data directory: the event log, which only grows (ADR-0008, ADR-0025), the content keys, the journals of accounts, audit, settings and notes, and the attachments beside the log (ADR-0045). Deleting does not make the log smaller; erasure destroys a key (ADR-0026). Nothing measured the free space. When the file system filled up, a command failed while its record was written; the log then refused further appends until it was reopened (ADR-0025), and people saw a change not made without knowing why. ADR-0045 named the missing total bound itself.

How does the wiki notice a storage getting full, tell those who can act, and fail in a way people understand?

## Decision drivers

- **Seen before it is too late:** administrators learn of a filling storage while there is time to act.
- **Only the wiki knows what takes the space:** log, attachments, journals.
- **Fail gracefully:** refuse the largest growth first, then stop writing in an orderly way, never half a command.
- **Room can always be made:** deleting, purging and cleaning up stay possible, and sign-in too.
- **Monitoring that exists is served, not replaced:** the metrics carry the same numbers (ADR-0016).
- **Only the standard library:** no new dependency.

## Considered options

1. **Measure, show, warn and protect in the wiki**, with thresholds an administrator sets
2. **Metrics only,** and leave warning and protecting to the operator's monitoring
3. **A quota** per space or for the whole wiki, counted from what the wiki stores

## Decision outcome

Chosen option: **1**.

- **Measuring:** every five minutes, the server measures the file system the data directory lives on: free space for an unprivileged process and total size (`statfs` on Linux, macOS and FreeBSD, `GetDiskFreeSpaceExW` on Windows; elsewhere it reports that it cannot measure). It adds up the data files and the attachment files. It keeps a sample of the data's size at most every half hour for a week, and once six hours are known it estimates the growth per day and the days left at that pace.
- **Levels:** *warning* below 15 % or 5 GB free, *critical* below 5 % or 1 GB, whichever is reached first; *full* below 200 MB free, a fixed floor. Administrators may change the four thresholds under *Administration → Storage* (shares 1–50 %, megabytes up to a terabyte, critical not above warning); the change is kept in the settings journal and audited (`disk.thresholds.change`). The floor is fixed: it is what the log needs to record the commands that make room.
- **Telling:** when the level gets worse, every administrator is told once over their channels (ADR-0064), with the free space and its share; while the level stays, nothing more is sent. When it gets better, the next worsening is told again. The start page shows administrators a notice while the level is not *ok*, linking to the administration.
- **Protecting:**
  - from *critical* on, uploads are refused (HTTP 507, "the storage is almost full"), wherever they come from: the UI, the API, copying a page with its files, a connector. Texts still change.
  - at *full*, the wiki only reads: every change of items is refused with the same answer, the live editor too. Moving to the trash, deleting, purging and cleaning up older album versions stay possible, so room can be made; signing in stays possible.
  - a measurement that fails keeps the level the store had: it neither locks the wiki nor lifts a lock.
- **Showing:** *Administration → Storage* shows the level, the free and total space with a bar, the size of each part (event log, attachments and album, keys, accounts, audit trail, settings, notes), the growth per day and the days left, the thresholds, and how room is made. `GET /api/storage` answers the last measurement, `?fresh=1` a new one; `PUT /api/settings/storage` sets the thresholds. The metrics add `wwbb_disk_free_bytes`, `wwbb_disk_total_bytes`, `wwbb_disk_level` (0 ok to 3 full) and `wwbb_attachments_bytes`.

### Consequences

- **Positive:** a filling storage is seen in the administration, in the metrics and in a message, days before it is full; when it is full, the wiki says why and stays readable.
- **Negative / trade-offs accepted:**
  - A wiki on a file system with little room is held at *warning* or *critical* from the start; the thresholds can be lowered.
  - What the wiki measures is the whole file system: other programs on it count too, which is right for the question "will the next write succeed" but makes the wiki's share only one part of the picture.
  - The backup destinations of ADR-0073 are not measured; a local destination on the same file system shows only as less free space.
  - Five minutes between measurements: a single very large upload can still meet a full disk; the per-file budget (ADR-0045) bounds it.
- **Follow-ups / risks to watch:**
  - snapshots and compaction of the log (ADR-0008), the only way the log itself gets smaller;
  - a quota per space, should spaces need bounds of their own.

## Pros and cons of the options

### Option 2: metrics only

Strongest form: operators already watch their disks; the wiki exports numbers and does nothing else, so it adds no behaviour that could itself go wrong.

- Good: the least code in the wiki.
- Bad: small installations, on a NAS or a single server, often run without monitoring; and no monitoring can make the wiki refuse uploads first and stop in an orderly way. The metrics are part of option 1 anyway.

### Option 3: a quota

Strongest form: a bound per space or per wiki counted from what the wiki stores, independent of the disk.

- Good: fair shares between spaces; predictable.
- Bad: it answers a different question — who may use how much — and does not tell whether the next write succeeds. It needs decisions about who sets which bound. It is a follow-up, not a replacement.

## Links

- amends [ADR-0045](0045-attachments-encrypted-files-beside-the-log.md) (uploads are refused when the storage is almost full)
- relates to ADR-0008 and ADR-0025 (the log only grows), ADR-0016 (metrics), ADR-0026 (erasure), ADR-0064 (messages), ADR-0073 (backups), ADR-0104 (album retention)
