# ADR-0077: Connectors keep catalog spaces in step with their sources

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer asked that one or more external APIs can be connected to the wiki, chose two ways of connecting — sources read into the wiki on a schedule, and the wiki as the catalog that describes interfaces — and chose opendata.swiss as the first connector, 2026-10-21)

## Context and problem statement

ADR-0075 mirrors the interfaces of opendata.swiss into one catalog space. The synchronisation is built into the server for that one source: one set of settings, one schedule, one status, `opendata.swiss` fixed in every origin. Further sources are to follow, such as another CKAN portal, an internal registry or a source with a credential. Each would need the same scheduling, dry run, report, audit and withdrawal. The cause of an empty import also stayed hidden: when the source's search found data sets but none of their resources counted as an interface, a run reported nothing at all. How does the wiki connect several sources without building the synchronisation once per source?

## Decision drivers

- Everything ADR-0075 decided for a run holds for every source: upsert by origin (ADR-0023), what is added here is kept, withdrawal instead of deletion, a dry run before writing, an audited run.
- Credentials are secrets: they come from the server's environment, never from the settings, the page or the data, as for backups (ADR-0073).
- The synchronisation of ADR-0075 continues without being set up again.
- A run that imports nothing says why.

## Considered options

1. **Connectors: a list of sources in the site settings, each of a kind the server reads, all run by one runner.**
2. **No connectors in the server**: agents and scripts write sources into the catalog over the API and MCP.
3. **One more built-in synchronisation per source**, as ADR-0075 did.

## Decision outcome

Chosen option: **1**, with option 2 still open for sources the server does not read.

- **Connector.** A connector is kept in `site.wal`, next to the other site settings: an ID, a name, a kind, the space it fills, whether it runs on its schedule, the interval, the steward of new entries, and the settings of its kind. The ID (lower-case letters, digits, dots and hyphens, at most 40) names the source in the origin of every entry it writes. It is fixed once the connector exists, so the entries stay its own. A site keeps up to 20 connectors.
- **Kinds.** The first kind is `ckan`: base URL, portal URL, formats and organizations, read as ADR-0075 describes. A further kind adds a reader and its settings; scheduling, applying, the report and the audit stay shared.
- **Origin and actor.** An entry's origin is the connector's ID as the system and the source's address as the instance. Changes are recorded as `sync:<ID>`. Two connectors never touch each other's entries. Two connectors that read the same source into one space collide on `source-id`, which is unique: the second one's run reports each collision and writes nothing for it.
- **Continuation.** Replaying ADR-0075's settings (`OpenDataSet`) yields the connector `opendata.swiss`, the ID the origins already carry. Nothing has to be set up again.
- **Credentials.** A connector sends the token in `WWBB_CONNECTOR_<ID>_TOKEN` as the `Authorization` header CKAN expects, if the variable is set; the ID is in capitals with every other character as `_`. The page shows whether the variable is set and its name, never its value.
- **Running.** One runner ticks every ten minutes and runs each connector that is switched on and due. Each connector runs one run at a time; different connectors are independent. A connector is run or dry-run on request. Its last ten runs are held in memory, per connector. Removing a connector keeps its entries and their origins; a running one cannot be removed.
- **A run says why it imports nothing.** Besides what ADR-0075 reports, a run names two more mismatches. It reports when the search finds data sets but none of their resources has a format of the list, together with the resources' formats and how often each occurs. It also reports when the source counts data sets in the formats but its search answers none.
- **API.** The routes are:
  - `GET /api/connectors`: the connectors, their status and the templates.
  - `POST /api/connectors`: add a connector.
  - `PUT /api/connectors/{id}`: change one; the ID stays.
  - `DELETE /api/connectors/{id}`: remove one.
  - `POST /api/connectors/{id}/run`: run one now.
  - `GET /api/connectors/{id}/formats`: the formats of a CKAN source.

  All are for administrators. The routes of ADR-0075 under `/api/catalog/opendata` are gone.
- **In the UI.** *Administration → Connectors* lists the connectors with their kind, space, last run and schedule. A new connector starts from the template of opendata.swiss; opening one shows its settings, runs and their reports.
- **The catalog (D).** What the wiki holds about each interface — owner, review, contact, specification, governance findings — stays as ADR-0074 decided, whichever connector or agent wrote the entry.

### Consequences

- **Positive:** a second source is a new connector, not new code, as long as it runs CKAN. A new kind of source needs only a reader.
- **Positive:** the cause of an empty import is in the run's report: the formats the source's resources really have, or a search that contradicts the source's own counts.
- **Negative / trade-offs accepted:** the routes of ADR-0075 changed; a script that called them must use `/api/connectors`.
- **Negative / trade-offs accepted:** a connector reads any address an administrator sets, as the synchronisation of ADR-0075 did, including addresses in the server's own network. Only administrators set connectors, and a source's answer reaches them only as counts, formats and error messages.
- **Follow-ups / risks to watch:**
  - a reader for generic JSON or REST sources, once real examples are known;
  - limiting connectors to public addresses unless allowed, as webhooks are (ADR-0062);
  - writing back to a source once the wiki becomes the master.

## Pros and cons of the options

### Option 2: no connectors, agents and scripts write

Strongest form: the server never calls out to the network, and any source can be connected in any language. Agents already have the API and MCP with the caller's rights, and the catalog rules (ADR-0074) apply to them too.

- Good: no outbound calls and no readers to maintain.
- Bad: scheduling, the dry run, the report and withdrawal would be rebuilt by every script, mostly not at all. Someone would have to run the scripts, and what they did would not be visible in one place. It stays the way for sources the server does not read.

### Option 3: one built-in synchronisation per source

Strongest form: each source gets exactly the settings and page it needs, with no abstraction to learn.

- Bad: the second source would copy the first's runner, status, routes and page, and a fix would have to be made in each copy.

## Links

- amends [ADR-0075](0075-mirroring-the-interfaces-of-opendata-swiss.md) (its synchronisation becomes the connector `opendata.swiss`)
- relates to ADR-0023 (origin), ADR-0062 (webhooks), ADR-0073 (credentials from the environment), ADR-0074 (catalog rules)
