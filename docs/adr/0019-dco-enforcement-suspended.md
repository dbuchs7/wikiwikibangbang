# ADR-0019: DCO enforcement is suspended until the maintainer switches it on

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** At which event is enforcement switched on — activating the `protect-main` ruleset (ADR-0003), the first external contribution, or the first release?
- **Question checked:** 2026-10

## Context and problem statement

[ADR-0002](0002-license-agpl-3.0-only.md) and [ADR-0018](0018-pull-request-level-dco-sign-off.md) require a human DCO sign-off on every pull request, and CI checks it. In practice all four pull requests so far (#1–#4) were merged with the DCO check red, by merge commit, without a sign-off — including #4, whose only purpose was to record a retroactive sign-off. The `protect-main` ruleset that would block such merges (ADR-0003) is not active.

The repository therefore describes a rule it does not follow. That is worse than having no rule: a check that is always red and always overridden teaches everyone — people and agents — to ignore red, and documents claim certifications that were never given.

The question is: **what does the repository state and check about the DCO until the rule is actually enforced?**

## Decision drivers

- What the repository says must match what actually happens.
- A red check must mean "do not merge"; a permanently red check destroys that signal for the test job too.
- The rule that agents never sign off must stay intact; only enforcement on merge is in question.
- Turning enforcement back on must be a single, visible change.

## Considered options

1. **Keep enforcing** — the check stays red until a human signs, and merges keep overriding it
2. **Suspend enforcement: the check reports, but does not fail**, until the maintainer switches it on
3. **Remove the DCO requirement** and the check altogether

## Decision outcome

Chosen option: **2.**

- The DCO job runs with `DCO_MODE: advisory`. `dcocheck -advisory` prints the same per-commit report, raises a **warning annotation** when sign-offs are missing or invalid, and exits 0. The job is green; the warning stays visible on the run and the pull request.
- **Unchanged:** agents never write a `Signed-off-by` line anywhere (AGENTS.md); a human may still sign off, per commit or in the description (ADR-0018), and is encouraged to; the report says which commits lack a sign-off.
- **Switching on** means setting `DCO_MODE: enforce` in `.github/workflows/ci.yml`, in a pull request that supersedes this record. It is the maintainer's decision; an agent does not make that change on its own.
- Commits merged while enforcement is suspended carry no DCO certification. `docs/dco/retroactive-2026-10.md` lists the earliest of them and states that it is unsigned.

### Consequences

- **Positive:** CI is green when the tests are green, so a red check means something again; the repository no longer claims certifications that were not given; enforcement is one line away.
- **Negative / trade-offs accepted:** contributions made during the suspension have no DCO statement; if a second contributor's work is merged in this phase, its provenance is undocumented; a warning is easier to overlook than a failure.
- **Follow-ups / risks to watch:** answer the open question and switch enforcement on before the first external contribution at the latest; then decide whether commits from the suspension phase are certified retroactively.

## Pros and cons of the options

### 1. Keep enforcing

Strongest form: the rule is right, and a red check is the honest state as long as nobody signed; weakening the check because people ignore it rewards ignoring it.

- Good: the check states the truth about every pull request.
- Bad: without the ruleset nothing stops the merge, and four merges in a row show that the red signal is being overridden; the habit spills over to the test job.

### 2. Suspend enforcement

- Good: repository and practice agree; red keeps its meaning; the information is still shown; quick to reverse.
- Bad: no DCO statements during the suspension.

### 3. Remove the requirement

Strongest form: with a single author the DCO protects nothing yet, so the whole mechanism is overhead and can be rebuilt when needed.

- Good: least code and process.
- Bad: the mechanism (PR-level sign-off, agent detection, tests) was just built and works; rebuilding it later costs more than keeping it in advisory mode.

## Links

- suspends enforcement of [ADR-0018](0018-pull-request-level-dco-sign-off.md); relates to [ADR-0002](0002-license-agpl-3.0-only.md) and [ADR-0003](0003-repository-governance-settings.md)
- [AGENTS.md](../../AGENTS.md), "DCO sign-off: only humans sign"
- [`docs/dco/retroactive-2026-10.md`](../dco/retroactive-2026-10.md)
