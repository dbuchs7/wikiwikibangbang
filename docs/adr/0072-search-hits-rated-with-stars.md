# ADR-0072: Search hits rated with up to five stars

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked for a rating of up to five stars that shows how relevant a hit is to the search terms, 2026-10-20)

## Context and problem statement

The search (ADR-0033) finds the items in which every term occurs and orders them by a score: the key typed in full, then terms in the title, then in the body, properties and comments. The score is internal; a reader sees a list and cannot tell a strong hit from a weak one. How is a hit's relevance shown?

## Decision drivers

- The rating says how well the item answers the search, in a way a reader can understand and predict.
- It must not contradict the order of the list.
- It costs the search no noticeable time: it scans the live state (ADR-0033).

## Considered options

1. Stars relative to the best hit of the search: the first gets five.
2. Stars from the score, scaled by a fixed table.
3. Stars from where and how the terms occur, on each hit alone.

## Decision outcome

Chosen option: **3**, because it means the same for every search and can be explained in one sentence per star.

| Stars | The hit |
|---|---|
| 5 | its key is typed in full, or its title is exactly the search |
| 4 | every term is in the title |
| 3 | some terms are in the title, or the words occur as typed, in order, in one paragraph or property |
| 2 | every term occurs in one paragraph or property, not as typed |
| 1 | the terms are spread over the item, or only in comments |

Words are compared folded, as the search folds them (case, ß as ss), and phrases as whole words. The list is ordered by stars first, then by score, then newer first, so it never contradicts its stars. The rating is computed for hits only. The API answers `stars` with each hit; the UI shows them before each result, with the meaning as a tooltip and as text for screen readers.

### Consequences

- **Positive:** a reader sees at a glance which hits answer the search; the same rating means the same everywhere; the MCP search tool returns it to assistants too.
- **Negative / trade-offs accepted:** a search whose best hit is weak shows few stars — which is the information; the rating knows nothing of how often a term occurs or how recent an item is.
- **Follow-ups / risks to watch:** if readers expect the best hit to always have five stars, option 1 could be shown in addition.

## Pros and cons of the options

### Option 1: relative to the best hit

Strongest form: always uses the whole scale, and the first hit always looks right.

- Good: simple.
- Bad: five stars for the best of a poor set; the same item gets different stars in different searches for the same quality of match.

### Option 2: scaled score

Strongest form: reuses the existing ranking exactly.

- Good: no second rule.
- Bad: the score adds up per term, so the thresholds depend on how many terms were typed; hard to explain.

### Option 3: by where the terms occur

Strongest form: explainable per star and stable across searches.

- Good: predictable, cheap.
- Bad: a second rule beside the score, which the order then has to follow.
