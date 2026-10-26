# ADR-0128: The Atlassian import as a connector

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-26
- **Deciders:** Project initiators (the maintainer asked for the Atlassian import of ADR-0023 as a kind of connector, the first follow-up ADR-0127 named, 2026-10-26)

## Context and problem statement

ADR-0023 imports the spaces and projects of an Atlassian Cloud site with the command `import -from atlassian-cloud`. The command opens the data directory itself, so the server must be stopped; it runs once, by hand, with credentials and account lists in the shell. A migration rarely ends in one run: the source is used for weeks while teams move, and each re-run means stopping the wiki. ADR-0077 and ADR-0127 made connectors that run inside the server on a schedule, with a dry run, a report, an audited run and credentials from the environment, but their runs write entries of one kind into one space. How does the import run as a connector without losing what ADR-0023 decided — spaces per source space, types per issue type, restrictions never wider than in the source, roles only added — and without importing twice what the command imported before?

## Decision drivers

- What ADR-0023 decided for an import holds unchanged: one engine (`migrate.Run`), re-runs that change nothing that is in step, nothing deleted (D8), restrictions read conservatively, roles only added.
- A site imported by the command is continued by a connector, and the other way round, without a duplicate.
- What people change here is not silently overwritten by the next run.
- Credentials come from the environment, as for every connector (ADR-0077); the token travels only over https, or plain http to the local machine.
- The server keeps running while a connector imports.

## Considered options

1. A connector kind `atlassian` that runs the import engine itself, its items managed by the connector until a cut-over
2. The import mapped onto the entries of ADR-0127, one space and one type per connector
3. Keep the command only, and schedule it outside the server

## Decision outcome

Chosen option: **1**, because the import's engine already decides spaces, types, keys, restrictions and roles, and a reader that hands the engine its snapshot keeps all of it, while the connector adds the schedule, the dry run, the report and the audit.

- **A reader that writes itself.** A reader may return a write of its own instead of entries (`internal/connector/reader.go`). The kind `atlassian` reads the site with the readers of ADR-0023 (`migrate.ReadCloud`, shared with the command) through the connector's guarded client, and writes with `migrate.Run`: inside one write of the store, or, for a dry run, into a copy of it under a read. Scheduling, the report and the audited run are those of every connector; the report counts pages and issues new, changed and unchanged, and lists the engine's problems, the first 50 of them.
- **Settings.** The site's address, the keys of its spaces and projects, source accounts mapped to local usernames, source groups to local groups, and the roles to give in every imported space. They are what the command takes as flags and files. The usernames are resolved to accounts at every run, and checked when the connector is saved. A connector of this kind has no space, type or fields of its own: the import names its spaces, keeping their keys, and its types. The site must be https, or http to the local machine only, as for the command.
- **Credentials.** `WWBB_CONNECTOR_<ID>_EMAIL` and `WWBB_CONNECTOR_<ID>_TOKEN` hold the account's e-mail address and API token. The administration shows whether both are set, never their values.
- **The same origin as the command.** Items keep the origin of ADR-0023: system `confluence` or `jira`, the site's host as instance, the source's ID. A connector therefore continues what the command imported, and the command what a connector imported; the first connector run marks the command's items as its own.
- **Managed until the cut-over.** The connector's items are managed by it (`Managed`, ADR-0023): people read and watch them, but change or comment on them only after the cut-over, as every run writes them anew (comments, like every change, wait for the cut-over as ADR-0023 decided for managed items). *Cut over* in the administration (`POST /api/connectors/{id}/release`) releases them in every space the connector wrote to and turns its schedule off, as a further run would take the items back under the connector. The cut-over is audited.
- **Removed in the source.** As ADR-0023 decided, an item whose source object is gone is reported, never deleted here.

### Consequences

- **Positive:** a migration runs alongside the source for as long as teams need, on a schedule, without stopping the wiki; the dry run shows each run's effect first.
- **Positive:** the command stays for an import without a running server, with the same result; both continue each other.
- **Positive:** people cannot lose changes to a run: until the cut-over they cannot make them.
- **Negative / trade-offs accepted:** a run holds the store's write lock while it writes, and a dry run its read lock while it copies the store. A large site makes other writes wait for that time, as the command needed the server stopped altogether; the schedule should run when the wiki is quiet.
- **Negative / trade-offs accepted:** until the cut-over, nothing of the imported items is changed or commented on here, not even a typo; that is what keeps them in step. Discussion stays in the source until then.
- **Negative / trade-offs accepted:** a run after the cut-over takes the items back under the connector and overwrites what was changed since; the confirmation of the cut-over says so, and the schedule is off.
- **Follow-ups / risks to watch:**
  - an incremental run that reads only what changed since the last one (`Options.Partial`), once sites are large enough to need it;
  - choosing the cut-over per space instead of for the whole connector;
  - allowing comments on managed items, which a run does not overwrite;
  - a mapping of source accounts by e-mail address where the site shows them.

## Pros and cons of the options

### Option 2: the import as entries of ADR-0127

Strongest form: one model for every connector; the import is a JSON-like reader of pages, and the mapping UI of ADR-0127 applies.

- Bad: a connector writes one type into one space, while an import writes a space per source space, a type per issue type with its workflow, and restrictions and roles. Mapped onto entries, all of that would be lost or rebuilt in the connector.

### Option 3: the command, scheduled outside the server

Strongest form: nothing new in the server; a timer on the host runs the command.

- Bad: the server must stop for each run, and the dry run, the report and the audit stay in a terminal no administrator sees.

## Links

- amends [ADR-0023](0023-import-and-sync-from-jira-and-confluence.md) (the import runs as a connector too, managed until a cut-over)
- amends [ADR-0127](0127-connectors-read-any-json-source-into-a-type-of-ones-own.md) (a reader may write itself; a connector without a space)
- relates to ADR-0077 (connectors, credentials from the environment), ADR-0124 (the other source of the import command)
