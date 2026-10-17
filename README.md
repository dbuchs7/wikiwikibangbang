# wikiwikibangbang

Knowledge and work in one model: wiki pages and issues as typed items of one data model, in one open-source application written in Go ([ADR-0001](docs/adr/0001-unified-item-data-model.md)).

> **Status: design phase.** The binary starts and serves a placeholder page. There is nothing to use yet. The decisions so far are recorded in the [ADRs](docs/adr/README.md).

## Run

The product is one binary for Linux, macOS and Windows on amd64 and arm64, and 32-bit ARM Linux ([ADR-0024](docs/adr/0024-release-targets-and-container-image.md)). Download the archive for your platform from the releases page, unpack it and run:

```bash
./wikiwikibangbang serve -addr :8080
```

Then open <http://localhost:8080>.

As a container:

```bash
docker run --rm -p 8080:8080 ghcr.io/dbuchs7/wikiwikibangbang:latest
```

From source (Go 1.26):

```bash
go run ./cmd/wikiwikibangbang serve
```

## Contribute

Read [AGENTS.md](AGENTS.md) — it is the operating guide for humans and agents alike: invariants, commands, layout and how to approach a change.

## License

[AGPL-3.0-only](LICENSE).
