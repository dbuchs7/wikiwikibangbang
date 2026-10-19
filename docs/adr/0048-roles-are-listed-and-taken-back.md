# ADR-0048: Roles, group memberships and system roles are listed and taken back

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked that every feature be visible and usable in the interface, 2026-10-19)

## Context and problem statement

Access was granted by events (`RoleGranted`, `GroupMemberAdded`, `SystemRoleGranted`) but never taken back, and nobody could see who held what: the administration offered forms to grant, nothing to review. People leave teams and projects; a wiki where access only grows is not safe to run. Making the administration usable needs both the listing and the revocation.

## Decision drivers

- Access follows the event log like everything else (ADR-0008): a revocation is an event, replayed, audited and backed up.
- No state in which nobody can administer the server.
- A space administrator manages their space without being a system administrator, and needs to pick the people to grant a role to.

## Considered options

1. **Revocation events** next to the grant events, with listings derived from the state.
2. **A grant with "no role"**, reusing `RoleGranted` with an empty role.

## Decision outcome

Chosen option: **1**. Option 2 would avoid three event kinds, but a grant that means its opposite misreads in every audit and in every older reader; the codec would have to change anyway for the empty role.

- **Events:** `RoleRevoked` (space, principal), `GroupMemberRemoved` (group, principal) and `SystemRoleRevoked` (principal, system role), codec version 11. Revoking what is not held is refused as not found.
- **The last administrator** keeps the role: revoking it is refused.
- **What a revocation takes:** only the grant named. A role held through a group, or everywhere as administrator, stays.
- **Listings:** `GET /api/spaces/{space}/roles` (space administrators), `GET /api/groups` and `GET /api/system-roles` (administrators). `GET /api/users` is open to space administrators too, so they can choose whom to grant a role.
- **Revocation:** `DELETE` on the routes that grant: `/api/spaces/{space}/roles/{principal}`, `/api/groups/{group}/members/{principal}`, `/api/system-roles/{role}/{principal}`. Each is recorded in the audit trail (`role.revoke`, `group.remove`, `system-role.revoke`).
- **MCP:** none of these is offered to agents (ADR-0012): who may see what is a person's decision.
- **UI:** the administration is one page with a table of contents: spaces (with their members), item types (ready-made types, a list with a summary of each and editing as JSON), users with their system roles as checkboxes, groups with their members, granting a role, backup, import (the command to run on the server) and the audit trail. Each space has a page *Members and roles* for its administrators: change a role, take it back, grant one to a person or a group.

### Consequences

- Good: access can be reviewed and reduced; every change of it is in the log and in the audit trail.
- Good: a space administrator manages the space alone.
- Bad: space administrators learn every account's username. In an internal wiki that is the directory people expect; a server for unrelated parties would need a narrower lookup.
- Neutral: watches and notifications need no change: a notification shows only what its reader may read now (ADR-0046).
