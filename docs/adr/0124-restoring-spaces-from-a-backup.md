# ADR-0124: Restoring spaces from a backup

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for a restore of single spaces through the import and set its rules, 2026-10-26)

## Context and problem statement

A backup restores a whole installation into a new data directory (ADR-0034, ADR-0073). Often only one space is wanted back, for example a space that was lost, or a space of one installation that another needs.

All spaces share one event log, and every item has its own key (ADR-0026). A backup holds files, not spaces, so part of one cannot be copied back. A space has to be written into the installation item by item. That raises four questions:

- What happens to an item the installation still has?
- What happens to an item it deleted for good, perhaps on an erasure request?
- What happens to a reference into another space?
- How are permissions kept?

## Decision drivers

- Nothing the installation has is overwritten.
- What was deleted for good never comes back, so an erasure request stays fulfilled (revDSG, GDPR).
- A restored item is the same item: same ID, same key where it is free (D2, D7).
- Permissions are never widened (ADR-0023, D9).
- What is left out is reported, never dropped silently.
- A dry run says beforehand what would happen.

## Considered options

1. **Restore as an import:** the import reads the backup and writes the chosen spaces' items through the store's validated commands.
2. **A second installation:** restore the backup beside the live one and copy what is needed by hand.
3. **Bring back what was deleted for good as new items**, unless it was redacted with a reason.

## Decision outcome

Chosen option: **1**, with the maintainer's rules:

- **Collision:** an item the installation has is skipped and reported, live or in its trash. An item in the trash is restored from the trash, with its current content, not from a backup.
- **Deleted for good:** an item the installation purged or redacted is reported and never comes back. The same holds for an item that was deleted for good, or in the trash, when the backup was taken.
- **References:** an item that a restored item refers to in another space comes back too when the installation does not know it, with its space if need be. This covers the parent, the items its body embeds or mentions, and the targets of its relations.

### Use

```
wikiwikibangbang import -from backup -backup FILE|DIR|URL [-at NAME] -spaces A,B [-grant user=role] [-dry-run] [-passphrase-file FILE]
```

- The server is stopped, as for every import.
- The backup is chosen as for `restore`: files, or a directory or destination of automatic backups, with `-at`.
- The passphrase comes from `WWBB_BACKUP_PASSPHRASE` or a file.
- The backup is unpacked into a temporary directory beside the data directory, read, and removed again.

### What comes back

- **The item itself:** its ID, its key (another one if the key is taken here, noted), type, title, fields, state, parent and labels.
  - The origin names the backup (`System` `backup`, the backup's ID, the item's ID and key), so the import's provenance (ADR-0023) holds.
  - A type the installation lacks is defined from the backup.
- **Its files,** if not deleted. A picture or video in the text shows the restored file. A file made from another keeps that link when the other came back first.
- **Its protected passages,** with their IDs, who protected them and their readers (ADR-0088).
- **Its text.** A picture whose file did not come back is left out. A reference to an item that is not here, live, is left out: an embed goes, a mention stays as plain text. Each is noted.
- **Its relations** to items that are here, live.
- **Its comments,** if not deleted, each by its author, with replies and resolved threads. The time is that of the restore.
- **Its restriction, as written,** set after its text. It only narrows (D9). It replaces the one the check for sensitive data gives the restore as the text's author (ADR-0040). Until then the item is restricted to the restore.
- **A space the installation does not know** is created with its name. The roles of the backup go to the users the installation has, by ID, and to the principals named with `-grant`. Groups are not granted: a group of the same name here may hold other people. Each group is noted.

### Not restored

- History before the backup's state.
- Watches and personal notes, which belong to people, not to the space.
- A picture's own labels and retention rule.
- Kanbans.
- The findings a data protection review cleared. The checks run again on the restored content (ADR-0040, ADR-0058).

### Record

- The run reports what it restored, what it skipped and why, and every note.
- An item deleted for good is named by its key and ID only. Its title is content and is not repeated.
- A restore that writes leaves `restore.partial` in the audit trail with the spaces, the backup, and how many items were restored, skipped and deleted for good. That is the evidence that an erasure held.
- A second run restores nothing and reports everything as existing.

### Consequences

- **Positive:**
  - A lost space comes back without touching anything else.
  - A space moves to another installation with its content, files and protected passages.
  - An erasure is never undone by a restore.
- **Negative / trade-offs accepted:**
  - **In the same installation a restore usually brings nothing back.** Every item of a backup is known there, live, in the trash, or deleted for good. It is then a comparison of the backup with the installation. It brings items back where the installation does not know them: a second installation, or one restored from an older backup.
  - The installation decides only by what it knows. In another installation it cannot know what the backup's own installation deleted for good after the backup was taken.
  - A comment's time is the restore's; its history shows the restore as the author's action.
  - A space deleted for good keeps its key (ADR-0069). Its items are not restored into it.
- **Follow-ups / risks to watch:**
  - A list of the IDs deleted for good, kept outside the backups, so that any restore, of a space or of a whole installation, applies erasures made after the backup was taken.
  - Restoring into a running server from the administration, once the dry run's report has a page.

## Pros and cons of the options

### Option 1: restore as an import

Strongest form: the installation's own rules decide what may be written, with validation, sensitive data checks and data protection holds, as for every change.

- Good: precise; reported; repeatable; nothing overwritten.
- Bad: only what the store's commands can write comes back; history does not.

### Option 2: a second installation

Strongest form: no new code, and the backup is shown exactly as it was.

- Good: complete, with history.
- Bad: copying by hand loses identity, links and files, and is not recorded.

### Option 3: bring back what was purged as new items

Strongest form: a space purged by mistake is the case most worth a partial restore, and a redaction with a reason marks the erasures that must hold.

- Good: recovers from a mistaken purge.
- Bad: an erasure request may have been met with an ordinary purge; it would then be undone. The maintainer chose the strict rule.

## Links

- extends [ADR-0023](0023-import-and-sync-from-jira-and-confluence.md) (a backup as a source of the import) and [ADR-0034](0034-encrypted-backups-from-log-prefixes.md) (a backup restored in part)
- relates to [ADR-0073](0073-automatic-full-and-incremental-backups.md) (automatic backups), [ADR-0021](0021-deletion-only-when-unreferenced.md) and [ADR-0026](0026-crypto-shredding-of-item-content.md) (deletion), [ADR-0088](0088-protected-passages-of-a-body.md) (protected passages)
