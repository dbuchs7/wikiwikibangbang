# ADR-0083: Search finds any word and names the words of each hit

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-21
- **Deciders:** Project initiators (the maintainer found the rating too coarse, as hits hardly ever rose above three stars, and asked for five stars when there is only one hit, and for each hit to name the words it matched and those it did not, 2026-10-21)

## Context and problem statement

The search found only items that held every word (ADR-0033) and rated them by where the words occurred (ADR-0072). Four stars needed every word in the title as a substring, five the key or the exact title. A search for one or two ordinary words therefore stopped at three stars for nearly every hit, even for a page whose title was the word searched for. A reader also could not see why an item was a hit, and a single misspelt word emptied the list.

How are hits found and rated so that the stars tell strong hits from weak ones, and a reader sees which words each hit holds?

## Decision drivers

- A hit that answers the search plainly gets five stars; the scale is used.
- The rating stays absolute: the same match gets the same stars in every search (ADR-0072).
- One word that occurs nowhere does not hide the hits for the others.
- A reader sees for each hit which words it holds and which it lacks.
- The list never contradicts its stars.

## Considered options

1. **Keep every word required; only loosen the stars.**
2. **Any word finds a hit; the stars and the order count how many words it holds; each hit names its words.**
3. **Stars relative to the best hit** (option 1 of ADR-0072).

## Decision outcome

Chosen option: **2**.

- **Matching:** an item is a hit when **any** word occurs in it. The words are split and folded as before (ADR-0033).
- **Stars:**

  | Stars | The hit |
  |---|---|
  | 5 | its key is typed in full; its title is the search; every word is a whole word of the title; or it is the only hit and holds every word |
  | 4 | every word is in the title; or every word is a whole word of the item, all in one paragraph or property, or as typed |
  | 3 | every word occurs somewhere |
  | 2 | at least half of the words occur |
  | 1 | fewer |

- **Order:** by stars, then by how many words the hit holds, then by score, then newer first.
- **Words of a hit:** the API answers `matched` and `missing` with each hit, the words as typed; the MCP tool `search_items` returns them too. The UI shows them below a hit when it lacks a word or holds more than one, the missing ones struck through.

The maintainer asked for five stars for every only hit. An only hit that lacks a word is not raised: under any-word matching it may hold one word of three, and five stars would claim it answers the search. It keeps the stars its words give it, and its missing words are shown.

### Consequences

- **Positive:** a page named after the word searched for has five stars. A misspelt word no longer empties the list; the hits that hold it are shown first, the others after with the word struck through.
- **Negative / trade-offs accepted:** a search for several words lists more hits than before, among them items that hold only one word. They are rated low and come last; the filters by space, type and state narrow them as before.
- **Negative / trade-offs accepted:** an only hit's five stars depend on the other items, which the rating otherwise does not. It is the one place where the absolute rule of ADR-0072 yields, at the maintainer's request.
- **Negative / trade-offs accepted:** an export of the search holds the partial hits too.

## Pros and cons of the options

### Option 1: every word required

Strongest form: a hit always holds every word, so the list is short and precise, and only the star rule needs to change.

- Good: no partial hits.
- Bad: one misspelt or unknown word empties the list, and a hit has no missing words to name; half of what was asked for would have nothing to show.

### Option 3: stars relative to the best hit

Strongest form: the first hit always has five stars, which meets the expectation directly.

- Bad: five stars for the best of a poor set; the same item gets different stars for the same match in different searches. ADR-0072 rejected it for this reason, and it still holds.

## Links

- amends [ADR-0033](0033-full-text-search-by-scanning-the-live-state.md) (any word matches instead of every word) and [ADR-0072](0072-search-hits-rated-with-stars.md) (the star rules and the order)
