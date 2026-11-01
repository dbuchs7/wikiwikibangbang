# ADR-0153: The product is renamed Orbithek

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-11-01
- **Deciders:** Project initiators (the maintainer found "wikiwikibangbang" too long, chose "Orbithek" after a check of names in use, and chose a hard cut with the prefixes `OT_` and `ot_`, a new record and the earlier records left as they are, 2026-11-01)

## Context and problem statement

The product was called "wikiwikibangbang". The name is long, hard to say on the phone and to type, and it names only the wiki, while the product holds pages, tasks, boards, an album and processes as items of one model (ADR-0001). The name is everywhere: the Go module path, the binary, the container image, the environment variables (`WWBB_…`), the metrics (`wwbb_…`), the prefixes of tokens, the cookies, the keys the browser keeps, the file names of backups, the logo and the documentation.

The maintainer chose the new name "Orbithek": a library (*-thek*) in orbit, after the rooms the product already has and the star map of atlas. A check found no product or mark of that spelling; "Orbitek", which sounds alike, is used by several software firms, and the maintainer accepted that.

What is renamed, how far does the rename reach into installations that run, and what must stay as it was so that their data stays readable?

## Decision drivers

- **One name:** whoever reads the code, runs the binary, sets the environment or looks at the screen meets the new name only.
- **Data stays readable:** a data directory, a backup or a drawing written under the old name is read by the new binary as before.
- **Credentials stay valid:** tokens that agents and systems hold keep working; nobody has to issue them again because of a name.
- **Configuration is explicit:** an operator sees at once which variables to set; two sets of names that both count invite mistakes.
- **History is not rewritten:** the earlier records describe what was decided under the old name.

## Considered options

1. **Rename everything, with a hard cut of configuration and metrics, data formats unchanged:** the new name throughout; `WWBB_…` and `wwbb_…` no longer count; what is stored keeps its identifiers.
2. **Rename everything and read the old variables as well,** with a warning, for a transition period.
3. **Rename only what people see:** the display name and the logo; module, binary, variables and metrics stay.
4. **Rename everything, data formats included,** with a migration of data directories and backups.

## Decision outcome

Chosen option: **1, "rename everything, with a hard cut of configuration and metrics, data formats unchanged"**, because it gives one name everywhere a person or a tool looks, keeps every stored byte readable, and leaves no second set of variables that could silently win.

What is renamed:

- **Module and binary:** `github.com/dbuchs7/orbithek`, the command `cmd/orbithek`, the binary `orbithek`, the container image `ghcr.io/dbuchs7/orbithek`, the release archives `orbithek_<version>_<os>_<arch>`. The repository on GitHub is renamed by the maintainer; GitHub forwards the old address.
- **Environment variables:** every `WWBB_…` becomes `OT_…`, the budgets' `OT_BUDGET_…` included (ADR-0015). The old names are not read any more.
- **Metrics:** every `wwbb_…` becomes `ot_…` (ADR-0016).
- **Display name:** "Orbithek" in the web UI, the page title, the footer, the OpenAPI description and the OAuth resource metadata; the version line reads `orbithek <version> …`.
- **Logo:** the wordmark reads "orbithek" in one line, in Inter Display SemiBold; the mark, a question mark above a ticked box, stays. The files are named `orbithek-…`.
- **Tokens and cookies:** new API tokens start with `ot_`, OAuth access and refresh tokens with `ota_` and `otr_`, client secrets with `otc_`, OAuth client IDs with `ot-`; the cookies are `ot_session`, `ot_oidc_state` and `ot_engine_state`.
- **Keys of the browser:** `ot.…` in local storage, `ot-after-login` in session storage, the album's clipboard mark `data-ot-album`.
- **Backups:** a new backup is stored as `orbithek-<when>-<chain>-<seq>-full|inc.orbithek`; a download is named `orbithek-<when>.orbithek`.

What stays, because it is stored data or a credential someone holds:

- **The magics of the file formats:** `WWBBLOG1` (the event log, ADR-0025), `WWBBATT1` (attachments, ADR-0045), `WWBBBAK1` (backups, ADR-0034) and the marker `WWBB.from` in a backup. They are bytes in files, not names people read.
- **The names of stored backups:** `wikiwikibangbang-…-full|inc.wwbb` is read alike, so retention and restore find the backups made before the rename, and a chain begun under the old name goes on under the new one.
- **API tokens issued before:** a token that starts with `wwbb_` is still accepted until it expires or is revoked. OAuth access tokens are held in memory only and end with the restart into the new version; refresh tokens and client secrets are checked by their hash and do not depend on the prefix.
- **The sensitive data check** knows both prefixes, so a leaked old token is still found (ADR-0040).
- **The keys in drawings:** the customData key `wikiwikibangbang` of a process in a drawing and the PNG text chunk `wikiwikibangbang.processes` (ADR-0142) are part of the stored drawings.
- **The earlier records** are left as they are, with the old names; this record says what the names are now. `README.md`, `AGENTS.md` and the guides are written with the new names.

### Consequences

- **Positive:** one short name in the code, the operation and the interface; no stored data needs a migration; agents and systems keep their tokens.
- **Negative / trade-offs accepted:** an installation that updates must rename its environment variables from `WWBB_…` to `OT_…` before it starts the new version; without that, for example, no administrator is created, single sign-on and mail are off and the backup passphrase is missing. Dashboards and alerts must follow the metrics to `ot_…`. Everyone signs in again once, because the session cookie has a new name. Settings kept in the browser (theme, folded columns, views) start from their defaults once. The old identifiers remain in the formats, in the code that reads them and in the earlier records.
- **Follow-ups / risks to watch:** the release notes of the first version under the new name list the renamed variables and metrics. If a mark "Orbithek" is ever filed by someone else in the product's field, the name is reviewed again.

## Pros and cons of the options

### Option 1: rename everything, hard cut, data formats unchanged

Strongest form: the name is changed once and completely while the product is a prototype with few installations; what is stored keeps its identifiers, so nothing has to be migrated, and the configuration has exactly one valid set of names.

- Good: one name everywhere; no migration; no ambiguity in the configuration.
- Bad: every running installation must change its environment and monitoring on update.

### Option 2: read the old variables as well, for a transition

Strongest form: no installation breaks on update; operators move their variables at their own pace, warned by the log.

- Good: an update needs no preparation.
- Bad: two names for each of about a hundred settings, with a rule which one wins; warnings are overlooked, and the old names would live on in the code without an end date. With few installations, the cost buys little.

### Option 3: rename only what people see

Strongest form: the smallest change; nothing breaks, and the visible name is new.

- Good: no effect on operation.
- Bad: the binary, the image, the variables and the module keep the old name, so operators and developers meet two names forever; the request was to rename everything.

### Option 4: rename the data formats as well

Strongest form: no trace of the old name remains anywhere.

- Good: complete.
- Bad: every data directory, backup and drawing would need a migration or a second reader for each format; old binaries could no longer read new backups. The magics are bytes nobody reads, so the cost buys nothing a person sees.

## Links

- relates to ADR-0015 (budgets), ADR-0016 (metrics), ADR-0024 (single binary and container image), ADR-0025 (event log), ADR-0034 (backups), ADR-0040 (sensitive data), ADR-0073 (automatic backups and their names), ADR-0142 (processes in drawings)
- the logo: [`docs/brand/README.md`](../brand/README.md)
- amended by [ADR-0154](0154-the-mark-is-a-library-in-orbit.md) (the mark is a library in orbit)
