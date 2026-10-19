# ADR-0022: Space roles, narrowing restrictions, and a logged erasure role

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-17
- **Deciders:** Project initiators; accepted by the maintainer 2026-10-17
- **Open question:** Should space admins be able to read restricted items (recoverability: nothing can be locked away for good) or must restrictions bind them too (privacy: HR and security content hidden even from space admins)?
- **Question checked:** 2026-10

## Context and problem statement

ADR-0013 decides *who* a caller is. This record decides *what* that principal may do with items. Several records depend on it: ADR-0001 (relations to items the viewer cannot see show as "restricted item"), ADR-0012 (agents act with their caller's rights), ADR-0020 (an embed never grants access), ADR-0021 (who may trash, restore and purge, and the open need for immediate erasure).

Reasoning from how knowledge and work are actually shared: most content is open within a team, and the team is the natural unit — in this model, the **space**. A minority of content is sensitive (HR, security, contracts) and must be visible to fewer people than the team. Sharing outside the team happens by giving people a role in the space, not by handing out single items. Access therefore has a **default** (the space) and **exceptions that narrow** (restrictions). Exceptions that widen would make the space boundary meaningless.

## Decision drivers

- One simple default that covers most content; exceptions only where needed.
- Access can only be narrowed below the space boundary, never widened, so the space remains the unit an administrator reasons about.
- An item one may not read is indistinguishable from an item that does not exist — no leaks through errors, embeds, relations, search or history.
- Agents get exactly their caller's rights (ADR-0012).
- Content that must go immediately can go, with accountability.
- The same rules for every type (D6).

## Considered options

1. **Space roles only**
2. **Space roles plus restrictions that only narrow, inherited down the containment tree** (Confluence-like)
3. **Per-item access lists that grant** (sharing like a file system or Google Drive)
4. **Attribute- or policy-based access** (ABAC, e.g. rules in a policy engine)

## Decision outcome

Chosen option: **2.**

**Roles.**
- Per space: **viewer** (read), **editor** (read, create, change, trash, restore), **space admin** (all of it, plus purge and granting roles). Higher includes lower. Roles can be given to a principal or to a group (`@name`).
- System roles: **admin** (space admin everywhere; defines types and spaces) and **erasure** (see below).

**Restrictions.**
- An item can carry a **read** and an **edit** restriction: lists of principals and groups. An empty list does not restrict.
- A restriction applies to the item and **everything below it** in the containment tree (D5). A principal may read an item only if its space role allows it **and** no restriction on the item or its ancestors excludes it; the same for edit.
- A restriction **never grants**: naming someone without a space role gives them nothing.
- An edit restriction does not hide the item.
- Someone who is not a space admin cannot set a restriction that excludes themselves (no self lock-out). Space admins are not bound by restrictions (see open question).

**Visibility of what one cannot read.**
- Get, history, relation and search queries treat an unreadable item as non-existent (`ErrNotFound`). A readable item one may not change yields `ErrForbidden`.
- An embed of an unreadable item renders as "restricted", **without its ID, key or title**. Relations to unreadable items are hidden.
- An item's history omits events of other items the principal may not read (a body that embeds the item, a relation to it).
- New references (embeds, mentions, relations) may only point at readable items; references the previous body version had may stay, so a page remains editable for someone who cannot see everything it embeds.

**Commands.**
- Create: editor in the space, or edit right on the parent. Inline creation: edit right on the host.
- Change title, properties, state, body, restriction: edit right on the item.
- Add a relation: edit right on its source, read right on its target. Remove a relation: edit right on its source.
- **Move, trash, restore: edit right on every live item of the subtree**, plus the right to create at the target of a move. A move changes the restrictions every descendant inherits, so it is a change to all of them.
- Purge: space admin.

**Erasure.**
- A principal with the system role **erasure** may **redact** an item: its content is removed at once and a tombstone remains, **whether or not the item is referenced** and without going through the trash. A reason is mandatory and recorded in the event with the actor.
- Redaction targets one item; its children stay. Restrictions survive (they are access data, not content).
- Redaction is the **logged exception to D8**: references to a redacted item render as "deleted item".

**Where checks happen.** The store's commands are a trusted core used for setup, replay and tests. Everything that acts for a person or agent — the HTTP API (ADR-0011), the MCP adapter (ADR-0012) — goes through a **session** bound to the authenticated principal, which performs the checks above.

### Derived invariants

- **D9 – Access only narrows below the space.** An item is readable (editable) only if the space role allows it and no restriction on it or its ancestors excludes the principal; restrictions never grant.
- **D10 – Unreadable is non-existent, and only sessions face callers.** Nothing a caller receives — errors, embeds, relations, search results, history — reveals an item the caller may not read; API and MCP code use sessions, never the trusted core.

### Consequences

- **Positive:** a default that matches how teams work; sensitive exceptions without a second permission system; no leaks through the many paths that touch an item; agents are bounded by their caller; immediate erasure exists and is accountable.
- **Negative / trade-offs accepted:** sharing a single item with someone outside the space requires a role in the space (or moving the item); permission checks walk the ancestor chain on every access; moving or trashing a subtree that contains an item one cannot edit is refused, even when that item is not visible to the actor; space admins can read everything in their space (see open question).
- **Follow-ups / risks to watch:** group membership from the identity provider (ADR-0013); a test that API and MCP handlers never reach the trusted core (D10); caching of permission decisions once performance matters; who holds the erasure role is an organisational decision.

## Pros and cons of the options

### 1. Space roles only

Strongest form: the simplest model that can be explained in one sentence; no inheritance to reason about; fast checks.

- Good: trivial to understand and to check.
- Bad: sensitive content needs its own space, which fragments navigation and breaks the page tree; a confidential child of a public page is impossible.

### 2. Space roles plus narrowing restrictions

- Good: default plus exceptions; the space stays the boundary; proven in Confluence.
- Bad: inheritance must be considered on moves; one more concept than option 1.

### 3. Per-item granting access lists

Strongest form: maximum flexibility — any item can be shared with anyone, exactly as people share documents in cloud drives; no need to create spaces for collaboration across teams.

- Good: fine-grained sharing across boundaries.
- Bad: the space no longer tells anyone who can see what; audits require walking every item; embeds and relations across differently shared items multiply the leak paths this record has to close.

### 4. Attribute- or policy-based access

Strongest form: rules such as "contracts are readable by legal and by the owning team" express organisational policy directly and scale to large organisations.

- Good: very expressive; central policy.
- Bad: a policy language and engine to operate and debug; users cannot predict access from what they see; disproportionate before there are users.

## Prototype (2026-10-17)

`internal/item` implements this record (`permissions.go`): roles, groups, system roles, restrictions, the session with all checks, redaction. Tested: role gating, group roles, inheritance and narrowing of restrictions, edit restrictions, self lock-out, restricted embeds without identity, hidden relations, reference checks, subtree rights for move and trash, purge and grant rights, system admin, redaction (role, reason, referenced item, children, log entry), replay; a matrix test runs every session command as outsider, viewer and editor. Five mutations of the checks are caught.

Found while prototyping:

- **A move needs edit rights on the whole subtree**, even within one space: re-parenting changes the restrictions every descendant inherits. The first test expected otherwise and was wrong.
- **History leaked.** An item's history included the full body commit of a restricted page that embeds it. Fixed by filtering events of unreadable items; a test pins it. This is the kind of path D10 exists for.
- **Trusted core and session must stay apart.** The prototype keeps the unchecked store API for setup and replay; the risk is that later code calls it from a handler. D10 names this; a guard test is a follow-up.

## Links

- relates to [ADR-0001](0001-unified-item-data-model.md) (decision 5, D5), [ADR-0011](0011-http-api-route-table-and-access-classes.md), [ADR-0012](0012-mcp-adapter-over-http-api.md), [ADR-0013](0013-authentication-and-security-boundary.md), [ADR-0020](0020-embedding-and-containment-are-independent.md), [ADR-0021](0021-deletion-only-when-unreferenced.md) (D8 and its exception)
