# ADR-0002: License the project under AGPL-3.0-only

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

wikiwikibangbang is meant to be open source. Without a license file the code is under full copyright: nobody may use, modify or redistribute it, and contributions have no defined terms. A license must be chosen before the first external contribution, because changing it later requires the consent of every contributor (or a contributor licence agreement, which the project does not have).

The product is a server application that is typically operated as a hosted service. Licenses whose obligations trigger only on *distribution* of binaries do not reach that mode of use.

## Decision drivers

- Improvements made by those who operate the software as a network service flow back to the community.
- Alignment with the reference project [pblumer/atlas](https://github.com/pblumer/atlas), which uses AGPL-3.0-only.
- A well-understood, OSI-approved license with established tooling and legal interpretation.
- Predictability: no automatic adoption of future license versions whose terms are unknown today.

## Considered options

1. **AGPL-3.0-only**
2. **AGPL-3.0-or-later**
3. **Apache-2.0** (permissive)
4. **MPL-2.0** (file-level copyleft)

## Decision outcome

Chosen option: **AGPL-3.0-only**, because it is the only option that extends copyleft to software offered over a network, and it matches atlas. The full license text is in [`LICENSE`](../../LICENSE).

Contributions are accepted under the same license. Contributors certify the [Developer Certificate of Origin](https://developercertificate.org/) by signing off their commits (`git commit -s`), as in atlas.

### Consequences

- **Positive:** hosted modifications must be offered to their users in source form; no proprietary fork can compete as a closed SaaS without contributing back; licensing is consistent with atlas.
- **Negative / trade-offs accepted:** some organisations prohibit AGPL software by policy, which limits internal adoption there; permissively licensed projects cannot incorporate our code; dependencies must be compatible with AGPL-3.0 (permissive and GPL-3.0-compatible licenses are, some others are not).
- **Follow-ups / risks to watch:** check the license of every new dependency for AGPL compatibility; decide whether a `THIRD_PARTY_NOTICES.md` is needed once dependencies exist; changing the license later, including to a newer AGPL version, requires the consent of all copyright holders.

## Pros and cons of the options

### AGPL-3.0-only

- Good: network copyleft; matches atlas; terms fixed.
- Bad: deterrent for some corporate users; restricts reuse in permissive projects.

### AGPL-3.0-or-later

Strongest form: lets the project benefit automatically from a future AGPL version that fixes loopholes, without collecting consent from all contributors.

- Good: forward compatibility with future FSF revisions.
- Bad: delegates future terms to a third party; diverges from atlas.

### Apache-2.0

Strongest form: the lowest barrier to adoption, an explicit patent grant, and acceptance by virtually every corporate legal department — the fastest route to a large user base.

- Good: maximal adoption; patent grant.
- Bad: allows closed SaaS forks of the whole product without any contribution back, which for a hosted collaboration tool is the realistic way the project would be captured.

### MPL-2.0

Strongest form: a middle ground — modified files stay open, while the code can be combined with proprietary components.

- Good: weak copyleft; corporate-friendly.
- Bad: no network clause; hosted modifications need not be disclosed.

## Links

- Reference project license: [pblumer/atlas LICENSE](https://github.com/pblumer/atlas/blob/main/LICENSE)
