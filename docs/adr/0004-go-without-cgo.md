# ADR-0004: Go as implementation language, without CGO

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

The project needs one implementation language for its server. The choice determines the deployment model, the contributor pool, the available libraries and how the product can be distributed. It was set as a premise when the project was started ("open source, written in Go"); this record writes down why it holds and which constraint comes with it.

Adapted from atlas [ADR-0010](https://github.com/pblumer/atlas/blob/main/docs/adr/0010-go-and-no-cgo.md).

## Decision drivers

- A single, statically linked binary that is trivial to deploy and cross-compile.
- A concurrency model suited to a server with many simultaneous editors and long-lived connections.
- Mature pure-Go libraries for storage, HTTP, search and observability.
- A broad contributor pool and fast builds.
- Consistency with the reference project atlas, whose working method this project adopts.

## Considered options

1. **Go, with CGO allowed**
2. **Go, without CGO**
3. **TypeScript/Node.js**
4. **Rust**

## Decision outcome

Chosen option: **Go, without CGO.** Every package in the module builds with `CGO_ENABLED=0`. A dependency that requires CGO is not admitted; where only a C library exists, the capability is provided by a pure-Go alternative or not at all.

CI enforces the rule with a `CGO_ENABLED=0 go build ./...` step. The race detector needs CGO, so `go test -race` keeps running with the toolchain default; that is a test-time tool, not part of the product build.

### Consequences

- **Positive:** static binaries for every target from one machine; no C toolchain in the build; fast, reproducible builds; goroutines fit many concurrent connections.
- **Negative / trade-offs accepted:** some libraries are unavailable (e.g. SQLite through its C implementation, some image or document converters); the garbage collector must be engineered around on hot paths; less control over memory layout than Rust.
- **Follow-ups / risks to watch:** the collaborative editor ecosystem (CRDT/OT) is strongest in JavaScript and Rust; server-side document logic in Go may need to be written in-house (see ADR-0010).

## Pros and cons of the options

### Go, with CGO allowed

Strongest form: access to every mature C library (SQLite, libvips, full-text engines) without reimplementation.

- Good: largest library choice.
- Bad: cross-compilation and static builds become hard; a C toolchain enters CI and the release process; C calls interfere with the Go scheduler.

### Go, without CGO

- Good: one static binary, simple builds, consistent with atlas.
- Bad: smaller library choice.

### TypeScript/Node.js

Strongest form: the same language on server and client, and the best ecosystem for rich-text editing and CRDTs (Yjs, ProseMirror, Hocuspocus).

- Good: the collaborative-editing stack is available off the shelf.
- Bad: no single-binary distribution; a runtime and dependency tree to operate; contradicts the project premise.

### Rust

Strongest form: memory safety without a garbage collector, and mature CRDT libraries (e.g. yrs, Automerge).

- Good: performance and safety; CRDT libraries available.
- Bad: slower development and a smaller contributor pool; contradicts the project premise.

## Links

- atlas [ADR-0010](https://github.com/pblumer/atlas/blob/main/docs/adr/0010-go-and-no-cgo.md)
- relates to ADR-0009 (single binary), ADR-0010 (real-time collaboration)
