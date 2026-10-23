# ADR-0104: Rules for the older versions of pictures

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators. The maintainer asked for retention rules for the album: defined in the administration, and applied per picture directly in the album, by age and by the number of versions kept. They chose:
  - manual and automatic clean-up, with a preview of the versions that go;
  - a picture's own rule takes precedence over the administration's;
  - the values may be changed in the album for one picture.

## Context and problem statement

A drawing saved again keeps every earlier version (ADR-0053, ADR-0055). Since ADR-0103 the album shows them as one stack, but they still take storage and backup space, and nothing ever removes them. Deleting them one by one is possible (ADR-0090) but tedious.

How are older versions removed by a rule, set once for the album and per picture where needed, without losing what a text shows or deleting by surprise?

## Decision drivers

- **Nothing lost unseen:** what a clean-up removes is shown before it happens. The current version and the versions a text shows are never removed.
- **The person nearest the picture decides:** a picture's own rule takes precedence over the default.
- **One model (D1):** a picture's rule is recorded in its item's log like its labels (ADR-0090). The default is a setting of the site, like the schedule of backups (ADR-0073).
- **Access stays per item (ADR-0022):** only whoever may edit an item sets its pictures' rules or cleans them up. The automatic clean-up acts for the administrators who switched it on.
- **Deleting stays deleting:** a removed version is deleted as by hand. Its content is destroyed (ADR-0045) and backups taken before still hold it until they expire (ADR-0073).

## Considered options

1. **A default in the administration and an optional rule per picture. Both say how many newest versions to keep and how young a version must be to stay. Applied by hand in the album, and every day when the administrators switch that on, always with a preview.**
2. **A default only, applied automatically.**
3. **Per-picture clean-up by hand only, without rules.**

## Decision outcome

Chosen option: **1**.

### The rule

- **Keep** is the number of newest versions kept, the current one counted. **Days**: versions younger than this many days are kept.
- A version stays when either limit keeps it. 0 sets no limit of its kind, and a rule with neither keeps everything. For example, "keep 3, days 30" keeps the three newest versions and every version of the last 30 days.
- **Always kept:** the current version, and the versions the item's text shows. The preview names these as "shown in the text".
- **Scope:** a rule applies to the versions of one file in one item, the stack of ADR-0103, and to pictures only.
- **Limits:** keep is at most 1000, days at most 36500.

### The default (administration)

- **Section:** "Album: older versions" in the administration holds keep, days and "apply the rules automatically every day". It is stored in the site's journal (`AlbumRetentionSet`).
- **Until set:** every version is kept and nothing runs on its own.
- **Preview:** it lists the pictures a clean-up would touch now and how many versions each would lose. Each picture is counted by its own rule, or by the values typed, before they are saved.
- **Clean up now:** this runs the clean-up at once, with the saved rules.
- **Audit:** a change of the default and every clean-up that removed something are written to the audit trail (`album.retention.change`, `album.clean`).

### A picture's own rule (album)

- **Where:** a stack offers "Clean up versions" to whoever may edit its item.
- **The dialog:**
  - It starts from the picture's own rule or, without one, the default, and says which applies.
  - It lists every version with what becomes of it (stays, stays because the text shows it, removed). The list follows the values as they are typed.
  - From there, the values can be saved as the picture's own rule, the picture can be returned to the default, or the versions shown as removed can be removed now. Removing asks for confirmation first.
- **Storage:** the own rule is held by the first version of the stack (`AttachmentRetentionSet`, codec version 26). It survives the removal of that version, as the chain of sources does (ADR-0055). A stack with its own rule shows a chip "Own rule".

### Automatic clean-up

- **When:** once a day, while the default says so, the server applies each picture's own rule or the default to every live item.
- **Recording:** each item's removals are one change, recorded as the actor `retention`.
- **Skipped:**
  - items a connector keeps (ADR-0077);
  - items held for a data protection review (ADR-0058).

### API

- **The default:**
  - `GET` and `PUT /api/album/retention`;
  - `GET /api/album/retention/preview` takes `keep` and `days` to try other values;
  - `POST /api/album/retention/run`.
- **One picture:**
  - `GET /api/items/{ref}/attachments/{id}/retention` returns the plan; `keep` and `days` try other values;
  - `PUT` sets the picture's own rule, or `{"default": true}`;
  - `POST …/retention/clean` removes what the rule does not keep, and answers what it removed and the plan after.
- **The album** tells a stack's own rule as `retention`.

### Consequences

- **Positive:** storage and backups no longer grow with every save of a drawing, and each picture can keep what it needs.
- **Positive:** a clean-up is never blind: the same plan is shown before and applied after.
- **Negative / trade-offs accepted:**
  - An older build cannot read a log that holds a picture's rule (codec version 26).
  - Removed versions are gone from the album and from the item. Only a backup taken before brings them back.
  - The rule acts on versions within one item. Copies in other items (ADR-0055) are stacks of their own with their own rules.

## Pros and cons of the options

### Option 2: a default only, automatic

Strongest form: one setting, nothing to do per picture.

- Bad: one picture's history is worth keeping, another's is not. A single rule either keeps too much or removes what someone needed.
- Bad: removal without seeing it first.

### Option 3: by hand only

Strongest form: nothing is ever removed without a person looking.

- Bad: it does not scale to many pictures, and a picture edited daily needs attention daily.
- Option 1 keeps this as its manual path: the preview and the clean-up per picture.

## Links

- relates to ADR-0103 (versions as a stack), ADR-0055 (sources and versions), ADR-0090 (picture labels, delete), ADR-0073 (backups and their retention), ADR-0058 (data protection holds), ADR-0045 (attachments), ADR-0025 (codec versions)
