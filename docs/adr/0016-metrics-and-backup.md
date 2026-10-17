# ADR-0016: Operational metrics behind authentication, and an allowlisted backup

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17

## Context and problem statement

A self-hosted single binary (ADR-0009) must be observable and recoverable by the operator alone, without external services. Two capabilities are needed from the first production use: metrics that a monitoring system can scrape, and a backup that can be restored on another machine.

Adapted from atlas [ADR-0142](https://github.com/pblumer/atlas/blob/main/docs/adr/0142-prometheus-metrics.md), [ADR-0198](https://github.com/pblumer/atlas/blob/main/docs/adr/0198-metrics-behind-the-boundary.md) and [ADR-0107](https://github.com/pblumer/atlas/blob/main/docs/adr/0107-backup-and-restore.md).

## Decision drivers

- Metrics in the format monitoring systems already expect.
- Operational data is not exposed to anonymous callers.
- A backup contains exactly what it should — never a secret or a store added later by accident.
- Backup does not interfere with the single writer (ADR-0008).

## Considered options

1. **No built-in metrics or backup**; operators copy the data directory and parse logs
2. **Prometheus endpoint behind authentication, with a dedicated token scope; backup as an explicit allowlist of stores**
3. **OpenTelemetry SDK for metrics and traces from the start**

## Decision outcome

Chosen option: **2.**

- **Metrics:** a `/metrics` endpoint in Prometheus exposition format, using `prometheus/client_golang` on a dedicated registry. It is an `authenticated` route (ADR-0011); a token scope `metrics` allows exactly `GET /metrics`, the narrowest credential to hand to a scraper. Liveness and readiness probes stay public and reveal nothing.
- **Backup:** an export of the data directory defined by an **allowlist** of stores; a store added later is excluded until it is added deliberately. Secrets (encryption keys, password hashes, token hashes) are excluded or exported only on an explicit, separate request. A backup is a consistent point-in-time read that does not block the writer.
- **Restore** is tested end to end in CI: back up, restore into an empty directory, compare.

**Implementation so far:** the backup and restore, as [ADR-0034](0034-encrypted-backups-from-log-prefixes.md) details. The secrets are in every backup, which is encrypted with a passphrase given at backup time: without the item keys and the accounts a backup could not be restored to a working instance. Not built: the metrics endpoint and the `metrics` token scope.

### Consequences

- **Positive:** monitoring works with standard tooling; backups are predictable and safe to store.
- **Negative / trade-offs accepted:** one more dependency (the Prometheus client); restore on another machine needs secrets re-entered or transferred separately.
- **Follow-ups / risks to watch:** tracing is decided later, once the metric contract is stable; the consistent read depends on the snapshot design in ADR-0008.

## Pros and cons of the options

### Nothing built in

Strongest form: no code and no surface; copying a stopped data directory is a valid backup.

- Good: nothing to maintain.
- Bad: downtime for backups; no insight into a running system; secrets end up in backups unnoticed.

### Prometheus behind authentication, allowlisted backup

- Good: standard formats; secure defaults; proven in atlas.
- Bad: a dependency; allowlist must be maintained.

### OpenTelemetry from the start

Strongest form: one vendor-neutral standard for metrics, traces and logs.

- Good: future-proof.
- Bad: heavier dependency; decides the tracing question before it is needed.

## Links

- atlas [ADR-0142](https://github.com/pblumer/atlas/blob/main/docs/adr/0142-prometheus-metrics.md), [ADR-0198](https://github.com/pblumer/atlas/blob/main/docs/adr/0198-metrics-behind-the-boundary.md), [ADR-0107](https://github.com/pblumer/atlas/blob/main/docs/adr/0107-backup-and-restore.md)
- relates to ADR-0008, ADR-0011, ADR-0013
