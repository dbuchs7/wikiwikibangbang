# ADR-0024: One binary for every common platform, and a distroless container image

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators (maintainer requirement 2026-10-17: "a single binary in Go that, like atlas, runs on all common platforms, also as Docker")
- **Open question:** Once persistence exists (ADR-0008): where does the data directory live by default on each platform, and is the container image's `/data` volume owned by uid 65532 enough for Kubernetes and rootless Docker alike?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0009 proposes a single binary with an embedded web UI. It leaves open how that binary reaches people: which operating systems and architectures are supported, how releases are built and published, and how the product runs in a container. The maintainer has set the requirement: one Go binary that runs on all common platforms, as atlas does, and also as a Docker image.

Adapted from atlas [ADR-0011](https://github.com/pblumer/atlas/blob/main/docs/adr/0011-single-binary-distribution-and-web-ui.md) and its `release.yml`, `docker.yml` and `Dockerfile`.

## Decision drivers

- One download and one command from nothing to a running server, on a laptop, a server or a Raspberry Pi.
- Releases built from one runner, reproducibly, without per-OS build machines.
- A container image that is small, runs as non-root and has as little in it as can be attacked.
- A platform that is released must not break unnoticed between releases.

## Considered options

1. **Release only a container image**; run everywhere through Docker
2. **Native packages per platform** (deb, rpm, MSI, Homebrew), built on per-OS runners
3. **Cross-compiled archives of one static binary for every platform, plus a multi-arch distroless image built from the same source** (atlas)

## Decision outcome

Chosen option: **3.**

- **One binary.** `cmd/wikiwikibangbang` is the only executable. It embeds the web UI (`internal/web`, `go:embed`) and needs no CGO (ADR-0004), so `go build` for any `GOOS`/`GOARCH` produces a self-contained file. Commands: `serve`, `healthcheck`, `version`.
- **Release platforms:** linux/amd64, linux/arm64, linux/arm (GOARM=6, every Raspberry Pi), darwin/amd64, darwin/arm64, windows/amd64, windows/arm64. atlas ships the same set except windows/arm64, which costs one matrix entry.
- **Releases** are cut by pushing a `v*` tag. `.github/workflows/release.yml` cross-compiles every platform on one Linux runner with `-trimpath -s -w`, stamps the version into `internal/buildinfo.Version`, packages `.tar.gz` (`.zip` for Windows) with the license and README, and publishes a GitHub Release with `SHA256SUMS`. Every 0.x release is a prerelease.
- **Container image:** a multi-stage `Dockerfile`; the build stage cross-compiles on the build host's platform, the runtime stage is `gcr.io/distroless/static-debian12:nonroot` (no shell, no package manager, uid 65532). Platforms: linux/amd64, linux/arm64, linux/arm/v7. The image probes itself with `HEALTHCHECK` → `wikiwikibangbang healthcheck`, because it has no curl. `.github/workflows/docker.yml` builds and smoke-tests the image on pull requests and pushes it to `ghcr.io/<owner>/<repo>` from `main` and from tags.
- **Guard:** CI builds every release platform on every push (`cross-build` job), and `internal/repoconfig` asserts that this list equals the release matrix.
- **Server defaults:** timeouts on every phase of a request, graceful shutdown on SIGINT/SIGTERM, a content-security policy of `default-src 'self'` on every response. The route table already follows ADR-0011: a route whose access class is not declared is `authenticated`, and because authentication (ADR-0013) is not built, such routes answer 401. Only the liveness probe and the UI's static files are public.

**Implementation is Partial:** the binary, the release workflow, the image and the guards exist. No release has been tagged yet, Windows executables carry no version resource (atlas generates one with go-winres; adding it means a build-time tool dependency), and the image has no data volume because there is no persistence yet (ADR-0008).

### Consequences

- **Positive:** installing is downloading one file; the same source yields every platform and the image; the image contains the binary, CA certificates and time zone data and nothing else; a platform break fails CI on the commit that causes it.
- **Negative / trade-offs accepted:** no OS packages (deb, rpm, MSI, Homebrew) and no auto-update; macOS binaries are not notarised, so Gatekeeper warns on first start; Windows binaries are not code-signed; a distroless image cannot be debugged with a shell inside it (use `docker debug` or an ephemeral debug container).
- **Follow-ups / risks to watch:** the open question on the data directory; image digests are not pinned (`golang:1.26-bookworm`, `distroless/static-debian12:nonroot` follow their tags), which trades reproducibility for automatic security updates; signing (cosign for images, Authenticode and notarisation for binaries) and an SBOM are not decided; a Helm chart as in atlas is not planned yet.

## Pros and cons of the options

### Release only a container image

Strongest form: one artifact to build, test and support; the runtime environment is identical everywhere; Kubernetes, Docker Desktop and Podman cover Linux, macOS and Windows hosts alike.

- Good: a single deliverable; no platform matrix to maintain.
- Bad: requires a container runtime, which many evaluators and small teams do not run; on macOS and Windows the container runs in a VM; contradicts the single-binary goal of ADR-0009 and the maintainer's requirement.

### Native packages per platform

Strongest form: what administrators expect; packages integrate with system services (systemd units, Windows services), upgrades come through the package manager, and signed packages pass corporate software policies.

- Good: best integration and the cleanest upgrade path per platform.
- Bad: a build and test pipeline per format and per OS runner; signing infrastructure for each; much more release machinery than an early project can maintain. Packages can still wrap the binary later, so nothing is lost by starting without them.

### Cross-compiled binary plus distroless image

- Good: one source, one runner, every platform; the image and the archives contain the same binary; consistent with atlas.
- Bad: no service integration or auto-update out of the box; unsigned binaries.

## Links

- atlas [ADR-0011](https://github.com/pblumer/atlas/blob/main/docs/adr/0011-single-binary-distribution-and-web-ui.md), `release.yml`, `docker.yml`, `Dockerfile`
- relates to ADR-0004 (Go without CGO), ADR-0008 (persistence), ADR-0009 (single binary with embedded web UI), ADR-0011 (route table and access classes), ADR-0013 (authentication), ADR-0016 (metrics and probes)
