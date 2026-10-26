# ADR-0140: A JSON source is read for what changed

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for incremental reading by the date of change, 2026-10-26)

## Context and problem statement

Every run of a connector reads its source in full (ADR-0077, ADR-0127): every page of the list, every entry, whether it changed or not. With paging (ADR-0135) a run may read 500 pages. Many APIs can answer only what changed since a time (`?updated_since=…`, `?modified_after=…`). Reading in full is also what tells a run what the source deleted: an entry it no longer answers is withdrawn. How does a connector read only what changed, without losing that?

## Decision drivers

- A run that reads only what changed must never withdraw an entry for not being in its answer.
- Deleted entries are still found, within a known time.
- Nothing is missed: an entry changed while a run reads, or a source whose clock is a little behind.
- Nothing new to keep durably for a decision that a full run can always make up for.
- An API description (ADR-0130) names the parameter; it is taken from it.

## Considered options

1. A parameter that asks for what changed since the last run began, minus an overlap; runs of what changed withdraw nothing; a full run after a restart, a change of the settings, on request and at least every so many days
2. The same, with the time kept durably in the settings, so a restart does not cost a full run
3. The time taken from the entries' own dates of change, at the source's clock

## Decision outcome

Chosen option: **1**, because it needs nothing from the entries and nothing kept on disk, and every case it cannot decide safely ends in a full run, which is always correct.

- **Settings.** A JSON source may name `since`: the parameter of the list's address that asks for what changed since a time; the time's format, `rfc3339` (default), `date`, `unix` or `unixms`; and `fullDays`, how often a run reads in full at the least (1 to 90, default 7). The parameter goes into the list's address and so into every page's (ADR-0135).
- **Which run reads what.** A run reads only what changed when the runner knows when the last run that wrote began, the settings it read by are unchanged, and the last full run began less than `fullDays` days ago. It asks from the last run's beginning less an hour, for entries changed while it ran and a source's clock a little behind; an entry read twice changes nothing. Otherwise it reads in full: the first run, the first after the server started (the runner keeps the times in memory only), the first after the space, type, fields or source changed, and the one an administrator asks for (`POST /api/connectors/{id}/run` with `full`, *Sync in full now*). A dry run reads as the next run would and moves nothing on, as does a run that fails.
- **Nothing withdrawn.** A run of what changed writes what it read and withdraws nothing; the report says from when it read (`since`), and so does its audit entry. An entry withdrawn before that comes back changed is no longer withdrawn, as in any run.
- **From an API description.** A query parameter named as is usual for this — `since`, `updated_since`, `modified_after`, `updatedAfter`, `changed_since` and the like — without a default becomes `since`: a text of format `date-time` as `rfc3339`, of format `date` as `date`, a number as `unix`.
- **In the administration.** The JSON form has a section *Reading only what changed* with the parameter, the time's format and the days between full runs; a run of what changed is marked as such in the history.

### Consequences

- **Positive:** a large source is read in a few requests between full runs.
- **Positive:** what the source deleted is still withdrawn, at the latest after `fullDays` days or the next restart.
- **Negative / trade-offs accepted:** a restart of the server, and every change of a connector's settings, costs one full run.
- **Negative / trade-offs accepted:** a source whose clock is more than an hour behind the server's, or that dates a change later than it makes it visible, can let a change slip until the next full run.
- **Negative / trade-offs accepted:** a source that ignores the parameter answers in full; the run then reads in full but still withdraws nothing, and the next full run withdraws as before.
- **Follow-ups / risks to watch:**
  - the time taken from the entries' own dates of change, where a source's clock cannot be trusted;
  - CKAN sources read for what changed by `metadata_modified`;
  - a source's own list of deleted entries, where it offers one.

## Pros and cons of the options

### Option 2: the time kept durably

Strongest form: a server that restarts often never reads in full for it.

- Bad: every run would write the settings' journal, an audited change of configuration, for a time a full run makes up for; and a time kept across a restore of a backup could ask from a moment the restored data never saw.

### Option 3: the entries' own dates

Strongest form: no clock skew; the source's own time decides.

- Bad: it needs the path to each entry's date of change and its format, and an entry without one, or a list sorted otherwise, defeats it; it is a follow-up where the source's clock is the problem.

## Links

- amends [ADR-0127](0127-connectors-read-any-json-source-into-a-type-of-ones-own.md) (a run of what changed withdraws nothing)
- relates to ADR-0130 (the parameter from an API description), ADR-0135 (paging)
