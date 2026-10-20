# ADR-0058: A data protection review holds open items as drafts until a second person releases them

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer set the rules: regulations such as the DSG and the GDPR linked or attached under governance; an assessment, the final decision a person's; personal data in need of protection allowed only in items restricted to people or groups; a draft until resolved; four eyes; restriction on the item itself; templates shipped, 2026-10-20)

## Context and problem statement

Governance (ADR-0040) finds secrets such as passwords, not personal data in the sense of data protection law: health, religion, criminal records and the like. Such data may be kept in a wiki, but never where the whole space reads it. Software cannot reliably tell such data from harmless text; a person must decide. How does the wiki point to such content, keep it from the space until someone has looked, and record who decided what?

## Decision drivers

- The law is the reference for the person who reviews; the software only gives hints.
- Content with indicators is not readable by the whole space before someone has decided.
- Four eyes: the one who saved the content does not release it.
- Allowed are restricted items: reading limited to named people or groups on the item itself.
- On every write path: editor, API, MCP, import (as ADR-0040).
- No regulation is wired into the code; an organisation adapts them.

## Considered options

1. **A hold on the item**, set on save when content brings in indicators of an active regulation and the item is open; released by a second person.
2. **Restrict the item to who saved it**, as for secrets (ADR-0040).
3. **Report only**, in the governance check.

## Decision outcome

Chosen option: **1**.

- **Regulations** live in the installation's settings (`site.wal`, ADR-0052): name, link to the law, an attached PDF of its text, whether active, and categories with indicators (words matched at the start of a word, regardless of case, or regular expressions after `re:`). Templates for the DSG (Art. 5 lit. c) and the GDPR (Art. 9 and 10) are shipped with starting indicators; administrators adopt them, inactive, then adapt and activate them.
- **On save**, the content an event brings in is assessed against the active regulations. Findings the item did not hold before put an item that is open to its space on hold (`HoldSet`, codec version 14) in the same batch, so nobody else reads it in between. Making an item open again (lifting its restriction) assesses its whole content. An item restricted to people or groups is assessed and listed in the governance check, not held.
- **A held item** is read only by those who may edit it (and space administrators), a narrowing like a restriction (D9). It shows its findings, the regulation and who saved them.
- **Release** (`HoldReleased`) by a person who may edit the item and did not bring the findings in: *restricted*, which requires a reading restriction on the item, or *false alarm*, which requires a reason and keeps these findings from holding the item again.
- **Notifications** (ADR-0057): a hold reaches who saved and the space's administrators; a release reaches who saved. Holds waiting for one's release are listed among one's findings.
- The hold's findings and the release's reason are sealed with the item's key and erased with it (ADR-0026).
- The assessment runs on live commands only; replay applies the logged outcome, so it stays deterministic.

### Consequences

- **Positive:** content with indicators never reaches the space unreviewed, and every release names a second person and, for a false alarm, a reason.
- **Positive:** regulations are data, not code; the organisation tunes them.
- **Negative / trade-offs accepted:** indicators are words; they miss paraphrases and flag harmless text ("Diagnose" of a server). The release as false alarm is the remedy; the cleared findings do not come back.
- **Negative / trade-offs accepted:** editors see the draft; the hold narrows to editors, not to who saved it, so that a second person can review.
- **Negative / trade-offs accepted:** existing content is assessed only when it is changed or opened; a check of what an installation holds already is a follow-up.
- **Negative / trade-offs accepted:** participants of a live session see what is typed before it is saved (as ADR-0040).

## Pros and cons of the options

### Option 2: restrict to who saved it

Strongest form: the strictest protection, as for secrets.

- Bad: nobody else could review it, which defeats four eyes; a false alarm would lock editors out.

### Option 3: report only

Strongest form: no false alarm ever hides anything.

- Bad: the content is readable by the whole space until someone acts.

## Links

- relates to ADR-0022 (permissions), ADR-0026 (erasure), ADR-0040 (governance), ADR-0052 (settings), ADR-0057 (bell)
