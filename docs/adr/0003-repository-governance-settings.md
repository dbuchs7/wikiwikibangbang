# ADR-0003: Repository governance settings for main

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Can a scheduled CI job read the live ruleset with the default `GITHUB_TOKEN`, so drift between `.github/rulesets/protect-main.json` and GitHub is detected automatically, or does it need a separate token with administration read access?
- **Question checked:** 2026-10

## Context and problem statement

The project's rules — English as project language, invariants, human-only DCO sign-off ([AGENTS.md](../../AGENTS.md)), license ([ADR-0002](0002-license-agpl-3.0-only.md)) — are checked by CI. CI only reports; it does not stop anything. Pull request #1 demonstrated the gap: it was merged with the DCO check red, and before that an agent could create `main` by a plain push.

What actually enforces the checks are repository settings on GitHub: default branch, allowed merge methods and branch protection. These are click settings. They appear in no commit and no test, so a later change to them goes unnoticed, although the DCO rule is only effective with them.

The question is: **which repository settings protect `main`, and how are they kept traceable?**

## Decision drivers

- CI checks must block merges, not only report.
- No direct pushes, force pushes or deletions on `main` — for humans and agents alike.
- Every change to `main` passes through a pull request with green checks.
- A single maintainer must be able to merge without a second person.
- The settings must be reviewable and versioned like code.
- Merging must not create unsigned commits (consistent with the DCO rule).

## Considered options

1. **Configure in the UI, undocumented**
2. **Configure in the UI, document the target state in an ADR**
3. **ADR plus the ruleset as versioned JSON in the repository**, with an automatic drift check as follow-up
4. **Manage settings with an external tool** (e.g. Terraform GitHub provider, a settings app)

## Decision outcome

Chosen option: **option 3, ADR plus versioned ruleset JSON**, because it makes the target state reviewable in pull requests without adding infrastructure, and it prepares an automatic drift check.

### Target state

**General settings** (*Settings → General*):

| Setting | Value | Reason |
|---|---|---|
| Default branch | `main` | rulesets target the default branch |
| Allow merge commits | off | GitHub's merge commit is unsigned |
| Allow squash merging | on | one commit per pull request |
| Allow rebase merging | on | keeps individual signed-off commits |
| Automatically delete head branches | on | merged branches do not linger |

**Ruleset `protect-main`**, defined in [`.github/rulesets/protect-main.json`](../../.github/rulesets/protect-main.json) and applied to the default branch:

| Rule | Value | Reason |
|---|---|---|
| Enforcement | active | |
| Bypass list | empty | rules apply to everyone, including the maintainer and agents acting under the maintainer's account |
| Restrict deletions | on | |
| Block force pushes | on | history on `main` is never rewritten |
| Require linear history | on | consistent with squash/rebase only |
| Require a pull request | on, **0 approvals** | a single maintainer cannot approve their own pull request; review is done by the person signing off |
| Require conversation resolution | on | open review threads block the merge |
| Required status checks | `fmt · vet · test (race)`, `cross-build (release platforms)`, `e2e (browser)`, `DCO sign-off (humans only)` | every job in `.github/workflows/ci.yml`: a pull request that breaks a release platform or the UI cannot be merged either |
| Require branches to be up to date | on | checks run against the current `main` |

The JSON file is the source of truth. A change to the settings is made in the file by pull request first and then applied on GitHub (*Settings → Rules → Rulesets → Import a ruleset*, or by editing the existing ruleset to match).

**Coupling to CI:** the required status checks are matched by job **name**. Renaming a job in `ci.yml` without updating the ruleset blocks every merge, because the ruleset then waits for a check that is never reported. A test in `internal/repoconfig` verifies that every required check in the JSON names an existing job in `ci.yml`.

### Consequences

- **Positive:** CI checks become merge gates; `main` is protected against direct and force pushes, including by agents; the target state is versioned, reviewable and partly self-tested.
- **Negative / trade-offs accepted:** every change, however small, needs a pull request; a broken CI job blocks all merges until it is fixed by pull request; with 0 required approvals the protection rests on the checks and the sign-off, not on a second reviewer.
- **Follow-ups / risks to watch:**
  - The settings must be applied on GitHub by the maintainer; set *Implementation* to `Landed` once they are configured and verified (a direct push to `main` is rejected, and a pull request with a red check cannot be merged).
  - Drift check: a scheduled job comparing the live ruleset with the JSON (see open question).
  - Raise *required approvals* to 1 once there is a second maintainer.

## Pros and cons of the options

### Option 1: UI only, undocumented

Strongest form: the fastest way to get protection, with no extra files; for a one-person project the maintainer knows the settings.

- Good: minimal effort.
- Bad: invisible to contributors and agents; changes go unnoticed; the reasons are lost.

### Option 2: UI plus ADR

Strongest form: the reasons are recorded where all other decisions live; no machine-readable format to maintain.

- Good: traceable reasoning.
- Bad: prose cannot be compared with the live state; a mismatch between ADR and GitHub is detected only by reading.

### Option 3: ADR plus ruleset JSON

- Good: target state versioned in GitHub's own import format; consistency with CI job names testable; a drift check can build on it.
- Bad: settings must still be applied by hand; the general settings (merge methods, default branch) are not part of a ruleset and remain documented only in this ADR.

### Option 4: External tool

Strongest form: full infrastructure as code — the tool applies the settings and corrects drift, nothing is done by hand.

- Good: complete automation, including general settings.
- Bad: adds a tool, state and a privileged token to a project that does not yet have product code; disproportionate for a single repository with one maintainer.

## Amendment (2026-10-16): squash merge only

[ADR-0018](0018-pull-request-level-dco-sign-off.md) lets the DCO sign-off be given once in the pull request description. That sign-off reaches the history of `main` only through a squash commit whose message is the pull request description. Therefore:

- *Allow rebase merging* is **off**; squash is the only merge method, and the ruleset's `allowed_merge_methods` is `["squash"]`.
- *Settings → General → Pull Requests → Allow squash merging → Default commit message* is **"Pull request title and description"**.

Everything else above stands.

## Applied (2026-10-19)

The settings are configured on GitHub:

- The default branch is `main`, and *Automatically delete head branches* is on.
- Squash is the only merge method (*Allow merge commits* and *Allow rebase merging* are off), and the squash commit message defaults to the pull request title and description.
- The ruleset `protect-main` was imported from [`.github/rulesets/protect-main.json`](../../.github/rulesets/protect-main.json). Read back through the REST API (`/repos/{owner}/{repo}/rulesets/{id}`), it matches the file rule for rule, and `/rules/branches/main` lists all five rules as active on `main`.
- GitHub stores three parameters the file does not set: `required_reviewers: []`, `do_not_enforce_on_create: false` and `require_extra_approval_for_unattributed_changes: true`. The first two change nothing. The effect of the third on pull requests with agent commits is to be observed; if it blocks merges, it goes into the file and this record.

A direct push to `main` from the maintainer's clone was rejected by the ruleset.

The ruleset only allows squash merges into `main`, whatever the general merge settings allow.

## Links

- amended by [ADR-0018](0018-pull-request-level-dco-sign-off.md)
- relates to [ADR-0002](0002-license-agpl-3.0-only.md) (license, DCO)
- [AGENTS.md](../../AGENTS.md), section "DCO sign-off: only humans sign"
- [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml), [`.github/rulesets/protect-main.json`](../../.github/rulesets/protect-main.json)
