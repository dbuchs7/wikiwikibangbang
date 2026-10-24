# ADR-0118: Spaces are called rooms

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-24
- **Deciders:** Project initiators (the maintainer asked to rename Bereich/Bereiche to Raum/Räume across the app, in English room/rooms, 2026-10-24)

## Context and problem statement

The top level of the content, which holds pages and tasks, a key such as PLAT and the roles of people, has been called a *space* in English and a *Bereich* in German. The maintainer wants it called *room*, in German *Raum*.

The word appears in three kinds of places:

- **Text people read:**
  - the UI's message catalogues;
  - the e-mail digest;
  - the column header of CSV and Word exports.
- **Names programs rely on:**
  - the API's paths and fields (`/api/spaces`, `space`);
  - the MCP tools;
  - the role `space-admin`;
  - the event log;
  - the codec.
- **Developer documents:** code, comments, ADRs and the README.

## Decision drivers

- Everyone who uses the app reads one word for one thing.
- Integrations and stored data keep working unchanged.

## Considered options

1. Rename the word in everything people read; keep the names programs rely on.
2. Rename everything, the API and the stored role name included.

## Decision outcome

Chosen option: **1**, because it gives people the new word without breaking an integration or touching stored data.

### What is renamed

- **The UI:** every message that names a space says *room* (German *Raum*, plural *Räume*). Compounds follow: *Raumadministration*, *Raumrolle*, *room administrator*.
- **The e-mail digest** says *rooms*.
- **The export's column header** says *Room* (German *Raum*).

### What is not renamed

- **The word in its other meanings:** the German "im Bereich der Umgebungsvariablen" (a part of a form), and the English "spaces become -" of labels (blanks).
- **Names programs rely on:**
  - the API's paths, parameters and fields;
  - the MCP tools;
  - the role value `space-admin`;
  - events, the codec, the code's identifiers, and the CSS classes the e2e test uses.
- **Error messages of the API**, which are in English for programs and logs.
- **The README and earlier ADRs**, which describe the API and the code.

### Consequences

- **Positive:** the new word everywhere one reads, with no migration and no change for agents or integrations.
- **Negative / trade-offs accepted:**
  - The API and the UI use different words for one thing; the README names both.
  - An API error that the UI shows as it is can still say "space".
- **Follow-ups / risks to watch:** new messages must say room. A message-catalogue check could flag "space" and "Bereich" in values, except where they have their other meanings.

## Pros and cons of the options

### Option 2: renaming everything

Strongest form: one word in the API, the code and the UI, so nobody has to translate between them, and new code cannot bring the old word back into the UI.

- Good: no two words for one thing.
- Bad:
  - `/api/spaces` and the field `space` break every script, agent and connector.
  - The role value `space-admin` is stored in the log and would need a migration.
  - The change would touch most of the code base for no visible gain.

## Links

- relates to [ADR-0017](0017-internationalisation-via-message-catalogue.md) (the message catalogues) and [ADR-0028](0028-item-api.md) (the item API)
