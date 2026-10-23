# ADR-0105: The automatic clean-up of the album runs at a fixed hour

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-23
- **Deciders:** Project initiators (the maintainer chose 03:00, 2026-10-23)

## Context and problem statement

ADR-0104 has the server apply the rules for older versions of pictures once a day. It counted the day from the server's start. A server that restarts, as a container does whenever its image is updated, moved the run each time. If it restarted more often than once a day, the run never came at all.

When does the daily clean-up run?

## Decision drivers

- It runs every day, however often the server restarts.
- It runs when hardly anyone is working, so no one sees a picture's versions change under them.
- It needs no setting of its own.

## Considered options

1. **At 03:00 of the server's local time.**
2. **24 hours after the start, as before.**
3. **An hour set in the administration.**

## Decision outcome

Chosen option: **1**.

- The clean-up runs at 03:00 of the server's local time, every day, while the album's default says automatic (ADR-0104). After a restart, it waits for the next 03:00.
- **Local time:**
  - It is the time zone of the server process (`TZ`). The container image carries the time zone data, but runs in UTC unless `TZ` is set, e.g. `TZ=Europe/Zurich`.
  - On the days summer time starts or ends, 03:00 is the local 03:00 of that day.
- **Missed runs:** a run missed because the server was down at 03:00 is not made up. The next one removes what the rules say then, so nothing is lost by waiting.
- An administrator can still clean up at once in the administration (ADR-0104).

### Consequences

- **Positive:** the run no longer depends on when the server last started.
- **Negative / trade-offs accepted:**
  - Without `TZ`, 03:00 UTC is 04:00 or 05:00 in Switzerland.
  - A server that is always down at 03:00, e.g. a laptop, never runs it on its own. "Clean up all pictures now" remains for that case.

## Pros and cons of the options

### Option 2: 24 hours after the start

Strongest form: no clock and no time zone involved.

- Bad: restarts move the run, and frequent ones prevent it.

### Option 3: an hour set in the administration

Strongest form: each installation picks its quiet hour.

- Bad: one more setting for a choice almost no one needs to make. It can be added later without changing anything else.

## Links

- amends [ADR-0104](0104-rules-for-the-older-versions-of-pictures.md) (when the automatic clean-up runs)
