# ADR-0038: The container starts as root only to hand over the data directory, then drops to an unprivileged account

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer chose this option on 2026-10-19 so that the image runs on a NAS without manual steps)

## Context and problem statement

ADR-0024 runs the image as uid 65532 from the start and leaves open whether a `/data` owned by that uid is enough. It is not, for the most common way of running the image on a NAS or a small server: a directory of the host is mounted at `/data`, and a directory created on the host, or by Docker for a missing path, belongs to root. The server then cannot write it and stops with `open /data/LOCK: permission denied`; with a restart policy it restarts in a loop. Reproduced with the image: a root-owned and a missing host directory fail, a Docker volume and a directory owned by 65532 work. Until now the remedy was a manual `chown 65532:65532` on the host, which a NAS user must do over SSH.

## Decision drivers

- The image runs with any mounted directory, without a step on the host.
- The server never serves as root.
- Platforms that run the image as non-root (e.g. Kubernetes' `runAsUser`) keep working unchanged.
- No shell or extra binary in the image (ADR-0024).

## Considered options

1. **Start as root, hand `/data` over, drop to the account irreversibly**, inside the binary
2. **Stay non-root** and explain the remedy in the error message
3. **Prefer a Docker volume** and document a bind mount as an advanced setup
4. **Run as root**

## Decision outcome

Chosen option: **1**.

- **Image:** runs as `USER 0:0` with `WWBB_RUN_AS=65532:65532`; the base stays distroless.
- **Binary:** `serve` with `WWBB_RUN_AS` set and running as root creates the data directory if needed, gives every entry in it that the account does not own to that account (`lchown`, symbolic links are never followed), drops supplementary groups, sets the group and then the user, and checks that it is no longer root, all before it opens any file or socket. Any failure stops the server. Without root nothing is changed, so `--user 65532:65532` and `runAsUser` work as before; without `WWBB_RUN_AS` the binary behaves as it did, outside the container too.
- **Setting:** `WWBB_RUN_AS` takes `uid:gid` of an unprivileged account; root is refused. It may name another account, e.g. the NAS user who should own the files.
- **Verified** with the image: root-owned, missing and volume mounts start; the running process has uid and gid 65532 for real, effective and saved IDs and no supplementary groups.

### Consequences

- **Positive:** the image works with a mounted host directory as is; the files belong to the service account afterwards; the restart loop on a fresh NAS directory is gone.
- **Negative / trade-offs accepted:** the process is root for the first moments of `serve`, before it reads any input from the network; a scanner flags the image for its root `USER`; the binary changes the owner of files in the data directory, which surprises an operator who expected their own ownership (they set `WWBB_RUN_AS` to their account).
- **Follow-ups / risks to watch:** answers the open question of ADR-0024 on the data directory's ownership; `docker exec` now runs as root by default, so administrative commands run that way write root-owned files, which the next start hands over again.

## Pros and cons of the options

### Start as root and drop

- Good: works everywhere a directory is mounted; the long-running process is unprivileged.
- Bad: privilege handling in the binary, which must be right; a root `USER` in the image.

### Stay non-root with a better error message

Strongest form: the image never holds root at all, so no bug in the drop can leave root behind, and the remedy is one documented command.

- Good: smallest attack surface; no change to ADR-0024.
- Bad: every NAS user needs SSH and the right `chown` before the first start, the step this decision removes.

### Prefer a Docker volume

Strongest form: a named volume is seeded from the image with the right owner, so nothing has to be fixed, and Docker manages it.

- Good: no root, no host step.
- Bad: the data lies in Docker's own storage, outside the NAS's shared folders, its file browser and its backup tasks.

### Run as root

- Good: nothing to configure.
- Bad: a compromised server is root in the container; rejected by ADR-0024 for good reason.

## Links

- answers the open question of [ADR-0024](0024-release-targets-and-container-image.md) on the data directory and changes its runtime user; uses [ADR-0025](0025-event-log-file-and-in-memory-state.md) (data directory)
