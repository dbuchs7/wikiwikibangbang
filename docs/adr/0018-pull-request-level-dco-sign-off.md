# ADR-0018: The DCO sign-off may be given once per pull request

- **Status:** Accepted
- **Implementation:** Partial
- **Date:** 2026-10-16
- **Deciders:** Project initiators

## Context and problem statement

[ADR-0002](0002-license-agpl-3.0-only.md) and AGENTS.md require a human DCO sign-off, and agents may not give it. Until now the only accepted form was a `Signed-off-by` trailer on every commit, added by the human with `git rebase --signoff` and a force push. Most commits are written by agents, so every pull request needed a local Git step by the maintainer before it could pass the DCO check.

In practice the step was skipped: pull requests #1 and #2 were merged with the DCO check red. A check that is always red and always overridden teaches everyone to ignore red, including a real failure in the test job.

The question is: **how can the responsible human give the DCO sign-off with little enough friction that it is actually given, without giving up the rule that only a human signs?**

## Decision drivers

- The sign-off must be an explicit act of a named person, after review.
- It must be possible from the GitHub web interface, without a local Git checkout.
- It should end up in the Git history of `main`, not only on GitHub.
- Agent sign-offs must still be rejected wherever they appear.
- The check must not be weakened silently; the change is recorded here.

## Considered options

1. **A. Sign-off on every commit** (`git rebase --signoff`), as before
2. **B. Sign-off once in the pull request description**, covering all its commits, with squash merge carrying it into the history
3. **C. Suspend the DCO check** while there is a single author

## Decision outcome

Chosen option: **B, with A remaining valid.**

- A human `Signed-off-by: Full Name <email>` line in the **pull request description** — on a line of its own, outside code blocks — covers every commit of the pull request that has no sign-off of its own.
- A commit that carries a sign-off naming an AI agent or a bot, or a malformed one, still fails; a pull request sign-off does not cure it. A sign-off line in the description that names an agent or bot fails the check as a whole.
- The DCO job runs also on `edited`, so adding the line to the description re-runs the check without a new push.
- **Squash merge only** (amends [ADR-0003](0003-repository-governance-settings.md)), with the squash commit message defaulting to the pull request title and description. The sign-off line therefore becomes a trailer of the squash commit on `main`, which is what option A achieves per commit.
- Signing per commit (option A) stays valid and needs no description line.
- The description is passed to the check through an environment variable and a file, never interpolated into the workflow script.

### Consequences

- **Positive:** signing off is one edit in the browser; the sign-off still names a person, is given after review and, with squash merge, is recorded in the history of `main`; the DCO check can become a real merge gate instead of a permanently red signal.
- **Negative / trade-offs accepted:**
  - Agents act on GitHub under the maintainer's account (verified: comments and pull requests created by the agent appear as `dbuchs7`). GitHub therefore cannot tell whether the line was written by the human or by an agent. The check enforces the form; that only a human writes the line rests on AGENTS.md and on the maintainer. Option A has the same limit — an agent could run `git commit -s` with the maintainer's name — so this is not a new weakness, but it is stated plainly.
  - If the squash commit message is edited at merge time and the line is removed, the sign-off exists only on GitHub.
  - Rebase merge is no longer available.
- **Follow-ups / risks to watch:**
  - Apply the squash-only settings on GitHub (ADR-0003 amendment); then set *Implementation* to `Landed`.
  - A later, stronger option: accept the description sign-off only if the last edit of the description was made by a human, e.g. through the GitHub API's edit history or a signed comment by an account agents cannot use. This requires agents to act under an account of their own.

## Pros and cons of the options

### A. Sign-off on every commit

Strongest form: the DCO's canonical form; every commit in the history carries the person's statement, independent of GitHub, and it survives any merge method.

- Good: strongest record; tool-independent.
- Bad: a local Git step and a force push per pull request; skipped in practice, as #1 and #2 show.

### B. Sign-off in the pull request description

Strongest form: the same explicit, named statement after review, given where the review happens; with squash merge it lands in the history of `main` as a trailer, just as in option A.

- Good: low friction, works in the browser; history record preserved through squash.
- Bad: the record of individual agent commits is lost in the squash; relies on the squash message setting.

### C. Suspend the check

Strongest form: with a single author the DCO protects against nothing yet — there is nobody else whose rights could be in question — so the check is pure overhead until a second contributor arrives.

- Good: no friction at all.
- Bad: the habit of merging without a statement of responsibility forms exactly while agents write most of the code, and the rule must be re-introduced later against established practice.

## Links

- enforcement suspended by [ADR-0019](0019-dco-enforcement-suspended.md); the sign-off forms defined here stay valid
- amends [ADR-0002](0002-license-agpl-3.0-only.md) (how the DCO sign-off is given) and [ADR-0003](0003-repository-governance-settings.md) (merge methods)
- [AGENTS.md](../../AGENTS.md), "DCO sign-off: only humans sign"
- [`internal/dco`](../../internal/dco/dco.go), [`cmd/dcocheck`](../../cmd/dcocheck/main.go), [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)
