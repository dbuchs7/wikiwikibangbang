# wikiwikibangbang

<picture>
  <source srcset="docs/brand/svg/wwbb-horizontal-white.svg" media="(prefers-color-scheme: dark)">
  <img src="docs/brand/svg/wwbb-horizontal-black.svg" alt="wikiwikibangbang" width="360">
</picture>

Knowledge and work in one model: wiki pages and issues as typed items of one data model, in one open-source application written in Go ([ADR-0001](docs/adr/0001-unified-item-data-model.md)).

> **Status: early prototype.** The binary stores items durably and serves an authenticated web UI and HTTP API. Nothing is ready for production use yet. The decisions so far are recorded in the [ADRs](docs/adr/README.md).

Trying a release? The [test guide](docs/testing/test-guide.md) says what to look at first and how to report a finding.

## Run

The product is one binary for Linux, macOS and Windows on amd64 and arm64, and 32-bit ARM Linux ([ADR-0024](docs/adr/0024-release-targets-and-container-image.md)). Download the archive for your platform from the releases page, unpack it and run:

```bash
./wikiwikibangbang serve -addr :8080
```

Then open <http://localhost:8080>. The first start creates the administrator `admin` and logs a generated password once; set `WWBB_ADMIN_USERNAME` and `WWBB_ADMIN_PASSWORD` to choose them instead. The API is described in [ADR-0027](docs/adr/0027-local-accounts-sessions-and-api-tokens.md):

```bash
curl -c cookies -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' http://localhost:8080/api/login
curl -b cookies http://localhost:8080/api/me
```

Single sign-on with OpenID Connect ([ADR-0030](docs/adr/0030-openid-connect-single-sign-on.md), [ADR-0066](docs/adr/0066-several-identity-providers-entra-id-and-google.md)) offers Microsoft Entra ID, Google and any other provider side by side. Microsoft: `WWBB_ENTRA_TENANT_ID` (the tenant's ID, a GUID), `WWBB_ENTRA_CLIENT_ID`, `WWBB_ENTRA_CLIENT_SECRET`. Google: `WWBB_GOOGLE_CLIENT_ID`, `WWBB_GOOGLE_CLIENT_SECRET` and `WWBB_GOOGLE_DOMAINS` (e.g. `example.org`; without it any Google account may sign in). Another provider: `WWBB_OIDC_ISSUER`, `WWBB_OIDC_CLIENT_ID`, `WWBB_OIDC_CLIENT_SECRET`, optionally `WWBB_OIDC_NAME` and `WWBB_OIDC_DOMAINS`. Register `https://<host>/api/oidc/callback` as the redirect address at each provider; it is taken from `WWBB_PUBLIC_URL` unless `WWBB_OIDC_REDIRECT_URL` sets it. New users get an account without roles; an administrator grants them.

Behind a reverse proxy that terminates TLS, set `WWBB_TRUSTED_PROXIES` to the proxy's address as the server sees it, e.g. `WWBB_TRUSTED_PROXIES=172.17.0.1` for a port published by Docker ([ADR-0037](docs/adr/0037-trusted-reverse-proxies.md)). Its `X-Forwarded-For` then names the client for the audit trail and the login throttle, and `X-Forwarded-Proto: https` makes the session cookie `Secure`. Without the setting these headers are ignored. The address to trust is the one the audit trail shows for logins before the setting is made.

`WWBB_LINKCHECK=on` lets space administrators check the web links of a space ([ADR-0039](docs/adr/0039-link-check-on-demand-public-addresses-only.md)): the server calls each linked address and lists the broken ones with whoever last edited the item. It is off by default, and it never calls an address in a private or local network.

Messages to people ([ADR-0064](docs/adr/0064-message-delivery-and-password-reset.md)) go to the channels on their accounts: mail, Slack, Microsoft Teams or another webhook, each set on the account page. Mail is sent through an SMTP server set by `WWBB_SMTP_HOST`, `WWBB_SMTP_PORT` (587, or 465 with `tls`), `WWBB_SMTP_SECURITY` (`starttls` by default, `tls`, or `none` for a relay on the same host or network), `WWBB_SMTP_USERNAME`, `WWBB_SMTP_PASSWORD` and `WWBB_SMTP_FROM` (e.g. `Wiki <wiki@example.org>`). `WWBB_PUBLIC_URL` (e.g. `https://wiki.example.org`) is the address links in messages lead to; with mail it enables *Forgot your password?* on the sign-in page. A digest of new notifications goes out every `WWBB_DIGEST_MINUTES` (15 by default, 0 for never). Webhooks reach only public addresses unless `WWBB_WEBHOOKS_PRIVATE=on`, for a chat server in the own network.

Content governance ([ADR-0040](docs/adr/0040-governance-checks-and-automatic-restriction.md)) is always on: saving content that holds a password, a private key, an access token, an IBAN or a card number restricts the item to whoever saved it, until a space administrator lifts the restriction under *Governance*, which also lists items unchanged for a year and formal contradictions.

AI agents use the API through the Model Context Protocol ([ADR-0012](docs/adr/0012-mcp-adapter-over-http-api.md)), with exactly the rights of the credential they are given: remotely at `POST /mcp` with a bearer token, or locally as a stdio server — `WWBB_TOKEN=<API token> ./wikiwikibangbang mcp -url http://localhost:8080`. A token of scope `read` gives an agent that can only read — and watch, for its own account ([ADR-0121](docs/adr/0121-a-change-feed-own-roles-and-watching-with-read-tokens.md)).

A system that copies pages elsewhere stays in step through the change feed ([ADR-0121](docs/adr/0121-a-change-feed-own-roles-and-watching-with-read-tokens.md)): it watches what it copied (`PUT /api/items/{ref}/watch`, allowed to a token of scope `read`), then asks `GET /api/changes?after=<last>&wait=30` again and again. Each answer lists every event on what it watches since its cursor — content, restrictions, holds, protected passages, moves, trash, purge — and `last`, the cursor for the next request; an entry whose `item` is empty concerns something it may no longer read, so it checks again what it holds. `GET /api/me/roles` lists every role its account holds.

A hosted assistant such as Claude connects with OAuth instead ([ADR-0068](docs/adr/0068-oauth-for-ai-assistants.md)): an administrator registers it under *AI access* and enters the three values shown there — the MCP address, the client ID and the secret — in the assistant's connector dialog; each person then approves it once and can withdraw the approval on the same page. Its tokens act as that person on `/mcp` only. `WWBB_OAUTH_DYNAMIC_REGISTRATION=on` lets applications register themselves (RFC 7591); they are marked as such on the consent page. The server names itself by `WWBB_PUBLIC_URL` in OAuth; without it, by the address a request arrived at, which behind a reverse proxy is `https` only when `WWBB_TRUSTED_PROXIES` is set.

Resource budgets ([ADR-0015](docs/adr/0015-resource-budgets-and-paged-listings.md)) — request body size, the size of an attached file (25 MiB by default, [ADR-0045](docs/adr/0045-attachments-encrypted-files-beside-the-log.md)), page size, search query size, live editing sessions, tracked logins — have defaults in [`internal/budget`](internal/budget/budget.go) and are changed with `WWBB_BUDGET_<NAME>`, e.g. `WWBB_BUDGET_PAGE_ITEMS=200`. A value that is not a positive number is logged and ignored; the server starts with the default.

The UI calls a space a *room* (German *Raum*); the API, the MCP tools and the code keep the name space ([ADR-0118](docs/adr/0118-spaces-are-called-rooms.md)). Items, types and spaces are managed through the item API ([ADR-0028](docs/adr/0028-item-api.md)); `GET /api/openapi.json` lists every operation:

```bash
curl -b cookies -H 'Content-Type: application/json' -d '{"key":"PLAT","name":"Platform"}' http://localhost:8080/api/spaces
curl -b cookies -H 'Content-Type: application/json' -d '{"name":"page","body":true,"fields":[]}' http://localhost:8080/api/types
curl -b cookies -H 'Content-Type: application/json' -d '{"space":"PLAT","type":"page","title":"Runbook"}' http://localhost:8080/api/items
curl -b cookies http://localhost:8080/api/items/PLAT-1
```

A type can describe a catalog kept in step with another system ([ADR-0074](docs/adr/0074-catalogs-kept-in-step-with-other-systems.md)): a field of kind `url` holds a checked web address, `"unique": true` keeps an identifier from being held twice in a space, `"recommended": true` has the governance check list items where a field is empty, and `"review": {"everyDays": 365, "field": "reviewed", "owner": "owner"}` lists items due to be checked again and makes the owner answer for them. `GET /api/items?where=source-id=abc` (and the MCP tool `find_items`) finds an item by a property's value.

Catalog spaces are kept in step with external sources of interfaces by connectors ([ADR-0077](docs/adr/0077-connectors-keep-catalogs-in-step-with-their-sources.md)), set up under **Administration → Connectors**; the first kind reads CKAN portals, and a new connector starts from the template of opendata.swiss ([ADR-0075](docs/adr/0075-mirroring-the-interfaces-of-opendata-swiss.md)). Choose the space, the formats that count as an interface (API, WMS, WFS, WCS, WMTS and SPARQL by default) and, if wished, the publishers; a dry run shows what a run would change, and when it finds nothing it says why, naming the formats the source really has. Each data set with interfaces becomes one entry of the type `interface`, linked to its data set on the portal; its page is written from everything the source says of it, in the language chosen for the connector, and replaced by every run ([ADR-0080](docs/adr/0080-one-page-per-data-set-written-from-the-source.md)). Owner, review, an added contact and comments are kept by every run, and a data set no longer published is withdrawn, not deleted. A source that needs an API token gets it from `WWBB_CONNECTOR_<ID>_TOKEN` in the server's environment (the ID in capitals, other characters as `_`), never from the page. A second kind reads any source that answers JSON ([ADR-0127](docs/adr/0127-connectors-read-any-json-source-into-a-type-of-ones-own.md)): name the address of the list and where in the answer each entry's ID, title, link, text and further values are; the defaults read the connectors' feed format, `{"entries": [{"id", "title", "url", "text", …}], "next": "…"}`, which an adapter for any other system can write. Where the API has an OpenAPI or Swagger description, in JSON or YAML, the connector *OpenAPI* reads it and offers its lists; the one picked fills these settings, which can still be changed ([ADR-0130](docs/adr/0130-an-api-description-fills-a-json-source.md)). Every connector names the type of the items it writes and which of the type's fields take which of the source's values. Connectors reach only public addresses; `WWBB_CONNECTOR_ALLOW_PRIVATE=on` allows sources in the server's own network. Agents keep the catalog complete over MCP as the [guide for agents](docs/guides/catalog-agents.md) describes.

A text shows BPMN processes ([ADR-0129](docs/adr/0129-bpmn-diagrams-and-processes-from-a-process-engine.md)), drawn with bpmn-js of bpmn.io. *Insert → BPMN diagram (new)* opens the modeler, *BPMN file* uploads a `.bpmn` file; either is kept as a file of the page and can be drawn anew while editing. *Process from an engine* shows a process of an atlas engine (github.com/pblumer/atlas) in its newest version, read from the engine whenever the page is shown. Engines are set up under **Administration → Process engines**; each one's API token comes from `WWBB_ENGINE_<ID>_TOKEN` in the server's environment (the ID in capitals, other characters as `_`), and an engine in the server's own network needs `WWBB_ENGINE_ALLOW_PRIVATE=on`, as connectors need theirs. On atlas, a token of the scope `diagrams` reaches exactly what is needed. Every signed-in person may list and read the engine's processes; on atlas that is every deployed process, whatever account the token belongs to. Where an administrator allows it, readers switch on the operations view: per element, how many tokens passed, wait there and are held by an incident, refreshed every 30 seconds; incident messages, instances and variables are not shown.
 Data is kept in `./data` (change it with `-data-dir`); `wikiwikibangbang check -data-dir data` verifies it without changing it.

Back up a running server from **Administration → Backup**, or with an administrator's API token ([ADR-0034](docs/adr/0034-encrypted-backups-from-log-prefixes.md)):

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"passphrase":"a long passphrase"}' -o backup.wwbb http://localhost:8080/api/backup
WWBB_BACKUP_PASSPHRASE='a long passphrase' ./wikiwikibangbang restore -from backup.wwbb -data-dir data-restored
```

The backup is encrypted and holds everything, attached files included, and the keys to the content: keep the passphrase apart from the file, and delete old backups when content must be gone for good — a backup can still read what was purged after it was made.

Automatic backups ([ADR-0073](docs/adr/0073-automatic-full-and-incremental-backups.md)) are set up under **Administration → Backup**: a destination — a directory of the server (a mounted file server or NAS, or a volume in a container), a WebDAV folder (`https://user@host/folder`), S3-compatible storage (`s3://bucket/folder?endpoint=https://…&region=…`), or a folder on an FTPS or SFTP server (`ftps://user@host/folder`, `sftp://user@host/folder`) — a full backup every 14 days and an incremental one every day by default, and how many versions to keep (up to 10; a version is a full backup with its incremental ones). The passphrase comes from `WWBB_BACKUP_PASSPHRASE` in the server's environment, never from the page, and so do a remote destination's credentials: `WWBB_BACKUP_USERNAME`, `WWBB_BACKUP_PASSWORD`, `WWBB_BACKUP_ACCESS_KEY` and `WWBB_BACKUP_SECRET_KEY` for S3, `WWBB_BACKUP_SSH_KEY` (a key file) and `WWBB_BACKUP_SSH_HOST_KEY` (the server's `SHA256:` fingerprint, shown by *Test destination* when it is missing) for SFTP, and `WWBB_BACKUP_CA_FILE` for a server with a certificate of its own. Plain `http://` and `ftp://` are refused. With the server stopped, `restore` takes the newest version from the directory or remote destination, or an earlier backup with `-at`:

```bash
WWBB_BACKUP_PASSPHRASE='a long passphrase' ./wikiwikibangbang restore -from /volume1/backup/wiki -data-dir data-restored
```

Older versions of pictures, e.g. of a drawing saved again, are removed by rules ([ADR-0104](docs/adr/0104-rules-for-the-older-versions-of-pictures.md)): a default under **Administration → Album: older versions**, and a rule of a picture's own in the album, which takes precedence; both with a preview of what goes. With *Apply the rules automatically every day at 03:00* the server cleans up at 03:00 of its local time ([ADR-0105](docs/adr/0105-the-automatic-clean-up-runs-at-a-fixed-hour.md)). The container runs in UTC unless `TZ` is set, e.g. `TZ=Europe/Zurich`.

Confluence spaces and Jira projects of an Atlassian Cloud site are imported with the server stopped ([ADR-0023](docs/adr/0023-import-and-sync-from-jira-and-confluence.md)): `WWBB_IMPORT_EMAIL=you@example.com WWBB_IMPORT_TOKEN=<API token> ./wikiwikibangbang import -from atlassian-cloud -site https://example.atlassian.net -spaces ENG,OPS -projects PLAT -data-dir data -users users.txt`. Import spaces and the projects their pages link to in one run, so the links resolve (ADR-0023). Run it with `-dry-run` first; a re-run updates what changed and creates nothing twice, and the report lists everything that could not be mapped. Atlassian Cloud usually hides e-mail addresses, so source accounts are mapped to local accounts by `users.txt`, one `accountId username` per line; a page restricted to an unmapped account or a group, or whose restriction cannot be read, is restricted to no one here until an administrator checks it — an import never shows a page to more people than the source did (ADR-0023). Imported spaces are visible only to system administrators until roles are given: `-grant @platform=editor -grant anna=space-admin` gives roles in every space the import writes to, and `-groups groups.txt` maps source groups to local groups, one `source group @local-group` per line, so a page restricted to a source group stays readable by the local group's members (ADR-0023). A run only adds roles; it never lowers or removes one that an administrator changed, and it reports local groups that have no members yet. Jira workflows arrive with their statuses and transitions, without conditions or validators; where a workflow cannot be read, every state can be reached from every other until the type is edited (ADR-0023). The same import runs inside the server as a connector ([ADR-0128](docs/adr/0128-the-atlassian-import-as-a-connector.md)): under **Administration → Connectors**, a new Atlassian Cloud connector takes the site, the keys, the accounts, groups and roles; `WWBB_CONNECTOR_<ID>_EMAIL` and `WWBB_CONNECTOR_<ID>_TOKEN` hold the account. It runs on a schedule with a dry run and continues what the command imported. Its pages and issues are read-only here until **Cut over** makes them ordinary items and turns the schedule off.

Operational metrics are served in the Prometheus format at `/metrics` to administrators and to API tokens of scope `metrics`, which an administrator creates under the account page and which can read nothing else ([ADR-0016](docs/adr/0016-metrics-and-backup.md)):

```yaml
scrape_configs:
  - job_name: wikiwikibangbang
    authorization: { credentials_file: /etc/prometheus/wwbb-token }
    static_configs: [{ targets: ["wiki.example:8080"] }]
```

Behind a reverse proxy, live editing ([ADR-0010](docs/adr/0010-real-time-collaboration-with-block-locks.md)) needs `/api/items/*/live` passed through unbuffered and without a short read timeout; the server sends a heartbeat every 20 seconds. For nginx:

```nginx
location ~ ^/api/items/[^/]+/live$ {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

As a container:

```bash
docker run --rm -p 8080:8080 -v wikiwikibangbang-data:/data \
  -e WWBB_ADMIN_PASSWORD='choose a long one' ghcr.io/dbuchs7/wikiwikibangbang:latest
```

A directory of the host works as well as a volume, e.g. `-v /volume1/docker/wikiwikibangbang/data:/data`: the server starts as root only to hand the directory to uid 65532 and then runs as that account ([ADR-0038](docs/adr/0038-container-starts-as-root-and-drops.md)). Set `WWBB_RUN_AS=<uid>:<gid>` to have another account own the files.

From source (Go 1.26):

```bash
go run ./cmd/wikiwikibangbang serve
```

## Contribute

Read [AGENTS.md](AGENTS.md) — it is the operating guide for humans and agents alike: invariants, commands, layout and how to approach a change.

## License

[AGPL-3.0-only](LICENSE).
