// Builds the palettes of the drawing editor (ADR-0117) in
// ../static/assets/palettes from libraries of the Excalidraw library
// collection, at a pinned commit. Every library there is published under
// the MIT license; palettes.LICENSE.txt names the libraries, their authors
// and the license text. Run after changing the selection below:
//
//   cd internal/web/vendor && node palettes.mjs
//
// Commit the palettes and their license file together, and update
// THIRD_PARTY_NOTICES.md; internal/web tests check both.
import { mkdir, writeFile } from "node:fs/promises";

const commit = "297a349eaff859e678f78d4dbc8e68df5fce42e5";
const base = `https://raw.githubusercontent.com/excalidraw/excalidraw-libraries/${commit}`;
const out = new URL("../static/assets/palettes/", import.meta.url);

// A palette takes items from one or more libraries: all of them, the ones
// named in only, or all but the ones named in skip. A library of format 1
// has no names; names gives them in order.
const palettes = {
  archimate: [{ source: "timothygalvin/excalidraw-archimate-template" }],
  bpmn: [{ source: "fraoustin/bpmn" }],
  network: [
    { source: "ei-au/computers", names: ["Server rack", "Desktop", "Laptop", "Tower"] },
    { source: "dwelle/network-topology-icons" },
    { source: "dday987/common-home-network-basics" },
    { source: "jgodoy/network-locations" },
  ],
  it: [{
    source: "mateuszbaransanok/it-icons",
    // The BPMN shapes of this library are in the BPMN palette already.
    skip: ["Event", "Message", "Timer", "Conditional", "Link", "Signal", "Error", "Escalation", "Compensation", "Cancel",
      "Multiple", "Multiple Parallel", "Gateway", "Exclusive Gateway", "Inclusive Gateway", "Parallel Gateway", "Comment",
      "Block", "Lifeline", "Pool"],
  }],
};

const credits = [];
for (const [palette, parts] of Object.entries(palettes)) {
  const items = [];
  for (const part of parts) {
    const res = await fetch(`${base}/libraries/${part.source}.excalidrawlib`);
    if (!res.ok) throw new Error(`${part.source}: ${res.status}`);
    const lib = await res.json();
    const found = lib.libraryItems || lib.library.map((elements, i) => ({ elements, name: part.names?.[i] }));
    let n = 0;
    for (const item of found) {
      if (part.only && !part.only.includes(item.name)) continue;
      if (part.skip?.includes(item.name)) continue;
      if (!item.name) throw new Error(`${part.source}: an item has no name`);
      n++;
      items.push({
        id: `${palette}-${items.length + 1}`,
        name: item.name,
        status: "published",
        created: 0,
        elements: item.elements.filter((e) => !e.isDeleted),
      });
    }
    credits.push({ palette, source: part.source, items: n });
  }
  await mkdir(out, { recursive: true });
  await writeFile(new URL(`${palette}.json`, out), JSON.stringify({ type: "excalidrawlib", version: 2, libraryItems: items }) + "\n");
}

const index = await (await fetch(`${base}/libraries.json`)).json();
const license = await (await fetch(`${base}/LICENSE`)).text();
const lines = [
  "The palettes in this directory are taken from libraries of the Excalidraw",
  "library collection, github.com/excalidraw/excalidraw-libraries, commit",
  `${commit}. Every library there is published under the MIT`,
  "license below.",
  "",
];
for (const c of credits) {
  const entry = index.find((e) => e.source === `${c.source}.excalidrawlib`);
  const authors = entry.authors.map((a) => a.name + (a.github || a.url ? ` (${a.github || a.url})` : "")).join(", ");
  lines.push(`${c.palette}.json: "${entry.name}" by ${authors}, ${c.items} items`);
}
lines.push("", license.trim(), "");
await writeFile(new URL("palettes.LICENSE.txt", out), lines.join("\n"));
