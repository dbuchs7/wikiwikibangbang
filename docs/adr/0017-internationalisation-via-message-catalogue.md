# ADR-0017: The UI is translated through a message catalogue, with English as source

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Project initiators
- **Open question:** Should user content (page titles, field labels of custom types, workflow state names) be translatable per language, or is translation limited to the product's own UI?
- **Question checked:** 2026-10

## Context and problem statement

English is the project language (AGENTS.md), but the product will be used in German, French and other languages. Translation is cheap to plan and expensive to retrofit: every hard-coded string has to be found again later.

Adapted from atlas [ADR-0267](https://github.com/pblumer/atlas/blob/main/docs/adr/0267-console-speaks-german-first.md) and [ADR-0414](https://github.com/pblumer/atlas/blob/main/docs/adr/0414-a-missing-translation-is-reported-not-refused.md). Atlas introduced its catalogue after much of its UI existed; this project introduces it before the first screen.

## Decision drivers

- No user-visible string is hard-coded.
- A missing translation is visible and reported, not silently replaced.
- No translation framework dependency.
- The language is chosen explicitly per user, not guessed.

## Considered options

1. **English only**, translation later
2. **A small message catalogue from the first screen, English as source locale**
3. **A full i18n framework** (ICU message format, extraction tooling)

## Decision outcome

Proposed option: **2.**

- Every UI string is a key in a catalogue; English is the source locale and the fallback.
- A **missing key renders as the key** in development, so a hole is visibly broken; a test reports keys present in the source locale but missing in another, as a list of gaps rather than a build failure.
- The language is a per-user setting (with the browser's language as the initial suggestion on first sign-in only); dates, numbers and time zones follow the same setting.
- Plurals and simple placeholders are supported; ICU message format is not, until a language needs it.

### Consequences

- **Positive:** translation is possible from day one without rework; gaps are measurable.
- **Negative / trade-offs accepted:** every string costs a key; a hand-written catalogue has fewer features than an ICU framework.
- **Follow-ups / risks to watch:** decide the open question before custom item types (ADR-0001) are designed, because translatable type definitions change their schema.

## Pros and cons of the options

### English only

Strongest form: no overhead while the product is still changing daily; translation of an unstable UI is wasted work.

- Good: fastest now.
- Bad: retrofitting costs far more later, as atlas experienced.

### Small catalogue

- Good: cheap, dependency-free, visible gaps.
- Bad: limited features.

### Full i18n framework

Strongest form: correct plural and gender rules for every language, extraction tooling and translator workflows out of the box.

- Good: complete.
- Bad: a dependency and tooling in a buildless UI (ADR-0009).

## Links

- atlas [ADR-0267](https://github.com/pblumer/atlas/blob/main/docs/adr/0267-console-speaks-german-first.md), [ADR-0414](https://github.com/pblumer/atlas/blob/main/docs/adr/0414-a-missing-translation-is-reported-not-refused.md)
- [AGENTS.md](../../AGENTS.md), "Project language: English"
