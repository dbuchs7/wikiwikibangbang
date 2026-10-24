# ADR-0117: Palettes for the drawing editor from the Excalidraw library collection

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-24
- **Deciders:** Project initiators (the maintainer asked for palettes in the drawing editor: ArchiMate, BPMN and general IT symbols such as laptop, router, server, internet and WLAN; to look first at what the Excalidraw libraries offer and use what is usable; and to take only open source that asks for no license fees, 2026-10-24)

## Context and problem statement

The drawing editor (ADR-0053) is Excalidraw with an empty library. Architecture and process sketches need the same shapes again and again: the elements of ArchiMate and BPMN, and pictures of devices and networks. Drawing them by hand each time is slow, and the results differ from drawing to drawing.

Excalidraw keeps a public collection of libraries, github.com/excalidraw/excalidraw-libraries. Whoever submits a library agrees that it is published under the MIT license. The collection has these candidates:

- **ArchiMate:**
  - "Excalidraw Archimate Template": 38 elements of the business, application and technology layers, in ArchiMate's colours;
  - "ArchiMate – Application Layer": the application layer only.
- **BPMN:** "bpmn", 34 items: task, pool, events of each kind, the gateways, data object and data store.
- **IT and networks:**
  - "IT icons": document, internet, cloud, firewall, database, server, printer and more;
  - "Network topology icons": computers, VPN, firewall, server, switch, hub, router, client;
  - "Common Home Network Basics": wireless router, access point, WLAN symbol, switch, server, cloud;
  - "Computers": server rack, desktop, laptop, tower;
  - "Network locations": office, city, house, data centre;
  - "Network elements" and "Computer parts", drawn more roughly or out of scope.

## Decision drivers

- Only material under an open source license without fees or conditions beyond attribution.
- Use what exists before drawing anew.
- The library stays short enough to find a shape in it.
- Nothing of the editor is loaded before it is needed.
- Nothing is fetched from outside the installation.

## Considered options

1. Take libraries of the Excalidraw collection, pinned to a commit, as palettes that are loaded when chosen.
2. Draw own palettes.
3. Let users browse libraries.excalidraw.com, as Excalidraw offers.

## Decision outcome

Chosen option: **1**, because the collection has good, complete sets for all three requests under the MIT license.

### The palettes

| Palette | Libraries | Shapes |
|---|---|---|
| ArchiMate | Excalidraw Archimate Template | 38 |
| BPMN | bpmn | 34 |
| Network and devices | Computers, Network topology icons, Common Home Network Basics, Network locations | 27 |
| IT symbols | IT icons, without its BPMN shapes, which the BPMN palette has | 28 |

### How they are chosen

- A chooser at the editor's top right, next to the library button, loads a palette and opens the library. On a phone the top row has no room, so the chooser sits above the bottom bar.
- The chosen palette replaces the previous one in the library. Shapes the user added to the library themselves stay.

### Files and build

- The palettes are files in `internal/web/static/assets/palettes/`, fetched only when chosen. The network palette is about 500 KB.
- `internal/web/vendor/palettes.mjs` builds them from a pinned commit of the collection.
- `palettes.LICENSE.txt` names each library, its authors and the MIT license text. `THIRD_PARTY_NOTICES.md` names them as well.

### What is left out

- The collection's own "Browse libraries" button is hidden: the palettes are part of the installation, and a library added on that site would not come back to the frame the editor runs in.
- "ArchiMate – Application Layer" duplicates the template; "Network elements" and "Computer parts" add little.

### Consequences

- **Positive:**
  - ArchiMate, BPMN, network and IT shapes are one choice away.
  - The material is attributed and under MIT.
- **Negative / trade-offs accepted:**
  - The shapes keep their authors' styles, which differ: hand-drawn, flat grey, 3D.
  - Names are in English, as the libraries have them. Excalidraw 0.17 shows them only on the item's picture where an author drew them.
- **Follow-ups / risks to watch:**
  - Updating a palette means a new pinned commit and running `palettes.mjs`; the test checks that every palette is credited.
  - The palettes are not checked against ArchiMate 3.1 or BPMN 2.0 in every detail.

## Pros and cons of the options

### Option 2: own palettes

Strongest form: one consistent style, and exactly the shapes wanted, with no third-party material.

- Good: uniform look; no attribution needed.
- Bad: well over a hundred shapes to draw and keep. The existing sets already cover the requests, and the maintainer asked to use them.

### Option 3: browsing libraries.excalidraw.com

Strongest form: hundreds of libraries, with nothing to maintain here.

- Good: the widest choice.
- Bad: the site returns a chosen library to the page that opened it, which the framed editor cannot take. It also fetches from outside the installation, and the libraries chosen there are not reviewed.

## Links

- relates to [ADR-0053](0053-drawings-with-excalidraw-on-a-page-of-their-own.md) (the drawing editor) and [ADR-0116](0116-every-view-fits-a-phone.md) (phones)
