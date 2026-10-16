# ADR-0012: An MCP server as an adapter over the HTTP API, acting as its caller

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

AI agents will read and write pages and issues: summarise a space, create tasks from meeting notes, update a status, draft a decision record. The Model Context Protocol (MCP) is the common way to offer such tools to agents. The question is **where the MCP server sits and whose permissions an agent uses**.

Adapted from atlas [ADR-0016](https://github.com/pblumer/atlas/blob/main/docs/adr/0016-mcp-server-over-http-api.md) and [ADR-0196](https://github.com/pblumer/atlas/blob/main/docs/adr/0196-authenticated-mcp-transport.md).

## Decision drivers

- Agents must not be able to do anything a person with the same credentials could not do.
- The MCP surface must not bypass invariants (single writer, validated changes, permissions).
- Every agent action is attributable to the person or token that triggered it.
- No new dependency for the protocol.

## Considered options

1. **MCP tools calling internal services directly**
2. **A separate MCP service with its own service account**
3. **A thin MCP adapter that only calls the public HTTP API, with the caller's credentials**

## Decision outcome

Proposed option: **3.**

- The MCP adapter holds no state and calls no internal function: every tool is an HTTP call to the API of ADR-0011. Invariants are therefore enforced in exactly one place, whatever bugs the adapter has.
- The protocol (JSON-RPC 2.0; `initialize`, `tools/list`, `tools/call`, `ping`) is implemented by hand, without a dependency.
- **Two transports:** stdio for a local agent process, configured with its own API token (ADR-0013); HTTP at `/mcp`, mounted as an `authenticated` route, which forwards the caller's `Authorization` and cookie headers unchanged to every API call it makes.
- **The agent acts as its caller.** The caller's credential always takes precedence; the adapter never lends a stronger token to a weaker request.
- Tools map closely onto API endpoints; list tools return the API's paging envelope (ADR-0015).

### Consequences

- **Positive:** agents can do exactly what their caller can; no second permission model; every change in history shows the human or token behind it.
- **Negative / trade-offs accepted:** every tool costs an HTTP round trip; tools are limited to what the public API offers.
- **Follow-ups / risks to watch:** decide which API routes are deliberately not offered as tools (e.g. permission changes, deletions) and record why; mark agent-originated changes in the item history.

## Pros and cons of the options

### Direct internal calls

Strongest form: fastest, and tools can offer composite operations the API does not have.

- Good: performance; flexibility.
- Bad: a second path into the core that can bypass permissions and invariants.

### Separate service with a service account

Strongest form: clear operational separation; the agent integration can be scaled and deployed independently.

- Good: isolation.
- Bad: a service account is a super-user; agent actions are no longer attributable to a person.

### Thin adapter with the caller's credentials

- Good: one enforcement point; full attribution; proven in atlas.
- Bad: HTTP overhead; limited to the API.

## Links

- atlas [ADR-0016](https://github.com/pblumer/atlas/blob/main/docs/adr/0016-mcp-server-over-http-api.md), [ADR-0196](https://github.com/pblumer/atlas/blob/main/docs/adr/0196-authenticated-mcp-transport.md)
- relates to ADR-0010 (agents in collaboration sessions), ADR-0011, ADR-0013
