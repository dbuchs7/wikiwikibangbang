# wikiwikibangbang single-binary server image (ADR-0024).
#
# Stage 1 compiles the self-contained binary; the web UI is embedded with
# go:embed and the build needs no CGO (ADR-0004), so it cross-compiles for any
# target platform. Stage 2 is a distroless static image: no shell, no package
# manager, only CA certificates, time zone data and a nonroot user.
#
# Build:
#   docker build -t wikiwikibangbang:dev .
# Multi-arch (requires buildx):
#   docker buildx build --platform linux/amd64,linux/arm64 -t wikiwikibangbang:dev .
# Stamp a version:
#   docker build --build-arg VERSION=1.2.3 -t wikiwikibangbang:1.2.3 .

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
WORKDIR /src

# Download modules first so the layer caches until go.mod or go.sum change.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# TARGETOS/TARGETARCH are set by buildx; they default to the build host with a
# plain `docker build`. The build stage runs on the build host's platform and
# cross-compiles, so multi-arch images need no emulation for the Go build.
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
# For linux/arm/v7, TARGETVARIANT is "v7" and GOARM becomes 7.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} GOARM=${TARGETVARIANT#v} \
    go build -trimpath \
      -ldflags="-s -w -X github.com/dbuchs7/wikiwikibangbang/internal/buildinfo.Version=${VERSION}" \
      -o /out/wikiwikibangbang ./cmd/wikiwikibangbang

# The data directory, created here because the runtime image has no shell.
RUN mkdir -p /out/data

# ---------------------------------------------------------------------------

# The nonroot variant provides uid/gid 65532, the account the server runs as.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=build /out/wikiwikibangbang /wikiwikibangbang
# The licenses the binary is distributed under (AGPL-3.0, and the notices of
# the modules it includes).
COPY --from=build /src/LICENSE /src/THIRD_PARTY_NOTICES.md /licenses/
# Owned by the nonroot user, so a volume mounted at /data starts writable.
COPY --from=build --chown=65532:65532 /out/data /data

EXPOSE 8080
VOLUME ["/data"]
# The process starts as root only to hand a mounted /data to uid 65532 (a
# directory created on the host belongs to root); it gives root up before
# it opens anything (ADR-0038). Running the image as 65532 directly, e.g.
# with Kubernetes' runAsUser, skips that step; WWBB_RUN_AS may name another
# account, e.g. the NAS user who should own the files.
USER 0:0
ENV WWBB_RUN_AS=65532:65532

# The image has no shell or curl; the binary probes itself.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/wikiwikibangbang", "healthcheck"]

ENTRYPOINT ["/wikiwikibangbang"]
CMD ["serve", "-addr", ":8080", "-data-dir", "/data"]
