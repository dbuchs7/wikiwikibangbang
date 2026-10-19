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

Single sign-on with an OpenID Connect provider ([ADR-0030](docs/adr/0030-openid-connect-single-sign-on.md)) is enabled by `WWBB_OIDC_ISSUER`, `WWBB_OIDC_CLIENT_ID`, `WWBB_OIDC_CLIENT_SECRET`, `WWBB_OIDC_REDIRECT_URL` (`https://<host>/api/oidc/callback`) and optionally `WWBB_OIDC_NAME`. New users get an account without roles; an administrator grants them.

Behind a reverse proxy that terminates TLS, set `WWBB_TRUSTED_PROXIES` to the proxy's address as the server sees it, e.g. `WWBB_TRUSTED_PROXIES=172.17.0.1` for a port published by Docker ([ADR-0037](docs/adr/0037-trusted-reverse-proxies.md)). Its `X-Forwarded-For` then names the client for the audit trail and the login throttle, and `X-Forwarded-Proto: https` makes the session cookie `Secure`. Without the setting these headers are ignored. The address to trust is the one the audit trail shows for logins before the setting is made.

`WWBB_LINKCHECK=on` lets space administrators check the web links of a space ([ADR-0039](docs/adr/0039-link-check-on-demand-public-addresses-only.md)): the server calls each linked address and lists the broken ones with whoever last edited the item. It is off by default, and it never calls an address in a private or local network.

Content governance ([ADR-0040](docs/adr/0040-governance-checks-and-automatic-restriction.md)) is always on: saving content that holds a password, a private key, an access token, an IBAN or a card number restricts the item to whoever saved it, until a space administrator lifts the restriction under *Governance*, which also lists items unchanged for a year and formal contradictions.

AI agents use the API through the Model Context Protocol ([ADR-0012](docs/adr/0012-mcp-adapter-over-http-api.md)), with exactly the rights of the credential they are given: remotely at `POST /mcp` with a bearer token, or locally as a stdio server — `WWBB_TOKEN=<API token> ./wikiwikibangbang mcp -url http://localhost:8080`. A token of scope `read` gives an agent that can only read.

Resource budgets ([ADR-0015](docs/adr/0015-resource-budgets-and-paged-listings.md)) — request body size, the size of an attached file (25 MiB by default, [ADR-0045](docs/adr/0045-attachments-encrypted-files-beside-the-log.md)), page size, search query size, live editing sessions, tracked logins — have defaults in [`internal/budget`](internal/budget/budget.go) and are changed with `WWBB_BUDGET_<NAME>`, e.g. `WWBB_BUDGET_PAGE_ITEMS=200`. A value that is not a positive number is logged and ignored; the server starts with the default.

Items, types and spaces are managed through the item API ([ADR-0028](docs/adr/0028-item-api.md)); `GET /api/openapi.json` lists every operation:

```bash
curl -b cookies -H 'Content-Type: application/json' -d '{"key":"PLAT","name":"Platform"}' http://localhost:8080/api/spaces
curl -b cookies -H 'Content-Type: application/json' -d '{"name":"page","body":true,"fields":[]}' http://localhost:8080/api/types
curl -b cookies -H 'Content-Type: application/json' -d '{"space":"PLAT","type":"page","title":"Runbook"}' http://localhost:8080/api/items
curl -b cookies http://localhost:8080/api/items/PLAT-1
```
 Data is kept in `./data` (change it with `-data-dir`); `wikiwikibangbang check -data-dir data` verifies it without changing it.

Back up a running server from **Administration → Backup**, or with an administrator's API token ([ADR-0034](docs/adr/0034-encrypted-backups-from-log-prefixes.md)):

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"passphrase":"a long passphrase"}' -o backup.wwbb http://localhost:8080/api/backup
WWBB_BACKUP_PASSPHRASE='a long passphrase' ./wikiwikibangbang restore -from backup.wwbb -data-dir data-restored
```

The backup is encrypted and holds everything, attached files included, and the keys to the content: keep the passphrase apart from the file, and delete old backups when content must be gone for good — a backup can still read what was purged after it was made.

Confluence spaces and Jira projects of an Atlassian Cloud site are imported with the server stopped ([ADR-0023](docs/adr/0023-import-and-sync-from-jira-and-confluence.md)): `WWBB_IMPORT_EMAIL=you@example.com WWBB_IMPORT_TOKEN=<API token> ./wikiwikibangbang import -from atlassian-cloud -site https://example.atlassian.net -spaces ENG,OPS -projects PLAT -data-dir data -users users.txt`. Import spaces and the projects their pages link to in one run, so the links resolve (ADR-0023). Run it with `-dry-run` first; a re-run updates what changed and creates nothing twice, and the report lists everything that could not be mapped. Atlassian Cloud usually hides e-mail addresses, so source accounts are mapped to local accounts by `users.txt`, one `accountId username` per line; a page restricted to an unmapped account or a group, or whose restriction cannot be read, is restricted to no one here until an administrator checks it — an import never shows a page to more people than the source did (ADR-0023). Imported spaces are visible only to system administrators until roles are given: `-grant @platform=editor -grant anna=space-admin` gives roles in every space the import writes to, and `-groups groups.txt` maps source groups to local groups, one `source group @local-group` per line, so a page restricted to a source group stays readable by the local group's members (ADR-0023). A run only adds roles; it never lowers or removes one that an administrator changed, and it reports local groups that have no members yet. Jira workflows arrive with their statuses and transitions, without conditions or validators; where a workflow cannot be read, every state can be reached from every other until the type is edited (ADR-0023).

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
