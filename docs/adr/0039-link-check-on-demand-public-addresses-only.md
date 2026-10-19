# ADR-0039: Web links are checked on demand, from public addresses only

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-19
- **Deciders:** Project initiators (the maintainer asked for a link checker with responsibility for each link and chose the on-demand option, 2026-10-19)

## Context and problem statement

Bodies link to web pages that move or disappear. Readers find out one dead link at a time; nobody sees them all, and nobody knows whose job the repair is. References between items are not the problem: D8 refuses to trash an item that a live item still references, so an embed or mention points into the void only after a logged redaction. Checking web links means the server calls addresses that users wrote into content. Done naively, that lets content make the server reach into its own network (server-side request forgery): the NAS's administration, a router, a cloud metadata service.

## Decision drivers

- A list of broken links per space, each with the person to fix it.
- Content can never make the server call an internal address.
- The server calls nobody unless its operator wants it to.
- A report shows nothing a reader may not read (D10).
- Bounded work per run (ADR-0015).

## Considered options

1. **On demand, by space administrators, public addresses only**, off unless enabled
2. **The same, plus a daily run** over every space
3. **Also internal addresses**
4. **In the reader's browser** instead of on the server

## Decision outcome

Chosen option: **1**.

- **Switch:** `WWBB_LINKCHECK=on` enables it; it is off by default, and another value stops the server.
- **What is checked:** the `http` and `https` links (link marks, ADR-0032) in the bodies of the live items of one space. Each distinct address is called once: `HEAD`, and `GET` where the server refuses `HEAD`; the body is never read. Up to 5 redirects; 10 s per address; 4 at a time; at most `WWBB_BUDGET_LINK_CHECK_LINKS` addresses per run (500 by default); 30 minutes per run.
- **Address guard:** the checker resolves the name itself, refuses it when any of its addresses is not public (loopback, private, link-local including cloud metadata, shared 100.64/10, multicast, unspecified), and then connects to exactly the address it checked, so a changing DNS answer cannot slip past. This holds for every redirect, and the checker never uses a proxy. An internal link is reported as refused, not called.
- **Who:** a space administrator starts a run (`POST /api/spaces/{space}/linkcheck`); one run per space at a time. Anyone with a role in the space reads the latest report (`GET`).
- **Report:** kept in memory, the latest per space; lost on restart. It lists the broken links with item, block, address, status or problem, and the responsible person: whoever committed the item's body last. Items the reader cannot read now, and items no longer live, are left out when the report is read.
- **UI:** "Link check" on the space page; the view starts a run, refreshes while it runs, and shows the list.

### Consequences

- **Positive:** dead links are found together, with a name next to each; content cannot be used to probe the internal network; no outbound traffic unless enabled.
- **Negative / trade-offs accepted:** links to intranet pages are never checked; a server that answers `HEAD` and `GET` differently, or blocks automated clients, shows as broken; link targets see requests from the server's address; reports vanish on restart.
- **Follow-ups / risks to watch:** a daily run (option 2) once the on-demand check has proven itself; links in comments (plain text, ADR-0035); an allowlist for chosen internal hosts if an operator needs it.

## Pros and cons of the options

### On demand, public addresses only

- Good: outbound calls only when a responsible person asks; no path into the internal network.
- Bad: someone has to remember to run it.

### Plus a daily run

Strongest form: dead links are found without anyone thinking of it, which is the point of a checker.

- Good: always current.
- Bad: regular traffic to every linked site from the server's address before the feature has proven its worth; more to run and to explain.

### Also internal addresses

Strongest form: a wiki on a NAS links to the home network's own services, and those links rot too.

- Good: checks every link.
- Bad: any editor can make the server call any internal address and read whether it answers: a scanner of the internal network in the hands of content.

### In the reader's browser

Strongest form: no server-side calls at all; the browser already reaches what the reader can reach.

- Good: no SSRF; intranet links are checked from where they are used.
- Bad: the content-security policy (`default-src 'self'`, ADR-0014) forbids calls to other sites, and cross-origin rules hide the answer from the page; it would only work by weakening both.

## Links

- uses [ADR-0014](0014-closed-renderer-for-user-content.md), [ADR-0015](0015-resource-budgets-and-paged-listings.md), [ADR-0022](0022-permission-model.md), [ADR-0032](0032-prosemirror-rich-text-editor.md); D8 of [ADR-0021](0021-deletion-only-when-unreferenced.md) is why item references need no checker
