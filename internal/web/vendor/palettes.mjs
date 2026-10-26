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
import { icon } from "./fluent.mjs";

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

// The collection has no ArchiMate relationships, and Excalidraw 0.17 has
// no diamond or hollow triangle as an arrowhead. They are drawn here
// (ADR-0125): a line with Excalidraw's heads where ArchiMate's match
// them, and a small diamond or triangle grouped with the line where they
// do not. Each is 160 pixels long, crisp, in the text colour.
const ink = "#1e1e1e";
function relationship(name, n, { style = "solid", start = null, end = null, from = null, to = null, line = true, dots = [] }) {
  const group = `archimate-rel-${n}`;
  let seed = 1000 + n * 37;
  const base = (type, id, x, y, width, height, extra) => ({
    id: `${group}-${id}`, type, x, y, width, height, angle: 0, strokeColor: ink, backgroundColor: "transparent",
    fillStyle: "solid", strokeWidth: 2, strokeStyle: "solid", roughness: 0, opacity: 100, groupIds: [group],
    frameId: null, roundness: null, seed: seed++, version: 1, versionNonce: 0, isDeleted: false, boundElements: null,
    updated: 0, link: null, locked: false, ...extra,
  });
  const elements = [];
  // A diamond at the start takes its first 24 pixels, a triangle at the
  // end its last 14; the line runs between.
  const x0 = from ? 24 : 0, x1 = to ? 146 : 160;
  if (line) {
    elements.push(base("arrow", "line", x0, 0, x1 - x0, 0, {
      strokeStyle: style, points: [[0, 0], [x1 - x0, 0]], lastCommittedPoint: null,
      startBinding: null, endBinding: null, startArrowhead: start, endArrowhead: end,
    }));
  }
  if (from) {
    elements.push(base("diamond", "from", 0, -7, 24, 14, { backgroundColor: from === "filled" ? ink : "#ffffff" }));
  }
  if (to) {
    elements.push(base("line", "to", x1, -8, 14, 16, {
      backgroundColor: "#ffffff", points: [[0, 0], [14, 8], [0, 16], [0, 0]], lastCommittedPoint: null,
      startBinding: null, endBinding: null, startArrowhead: null, endArrowhead: null,
    }));
  }
  for (const [i, filled] of dots.entries()) {
    elements.push(base("ellipse", `dot-${i}`, 0, -8, 16, 16, { backgroundColor: filled ? ink : "#ffffff" }));
  }
  return { name, elements };
}
// In the order of the ArchiMate 3.1 specification: structural, dependency,
// dynamic and other relationships, then the junctions.
const relationships = [
  ["Composition", { from: "filled" }],
  ["Aggregation", { from: "hollow" }],
  ["Assignment", { start: "dot", end: "triangle" }],
  ["Realization", { style: "dotted", to: "hollow" }],
  ["Serving", { end: "arrow" }],
  ["Access", { style: "dotted", end: "arrow" }],
  ["Influence", { style: "dashed", end: "arrow" }],
  ["Association", {}],
  ["Association (directed)", { end: "arrow" }],
  ["Triggering", { end: "triangle" }],
  ["Flow", { style: "dashed", end: "triangle" }],
  ["Specialization", { to: "hollow" }],
  ["And junction", { line: false, dots: [true] }],
  ["Or junction", { line: false, dots: [false] }],
].map(([name, spec], i) => relationship(name, i + 1, spec));

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
  if (palette === "archimate") {
    for (const r of relationships) {
      items.push({ id: `${palette}-${items.length + 1}`, name: r.name, status: "published", created: 0, elements: r.elements });
    }
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
// The Microsoft palette: Fluent UI System Icons, which Microsoft publishes
// under the MIT license, at a pinned commit (ADR-0125). No product logos.
const fluentCommit = "82ecf7cbdb43684403c7125efa160be04a33dca5";
const fluentBase = `https://raw.githubusercontent.com/microsoft/fluentui-system-icons/${fluentCommit}`;
const fluentIcons = [
  "Person", "People", "People Team", "Person Support", "Person Key", "Organization", "Building", "Building Factory", "Home",
  "Desktop", "Desktop Mac", "Laptop", "Phone", "Tablet", "Phone Laptop", "Print", "Headset",
  "Server", "Server Link", "Database", "Storage", "Hard Drive", "Cloud", "Globe", "WiFi 1", "Router", "Plug Connected", "Link",
  "Shield", "Shield Lock", "Shield Checkmark", "Lock Closed", "Key",
  "Document", "Folder", "Archive", "Mail", "Calendar", "Chat", "Call", "Video", "Clipboard Task",
  "Settings", "Apps", "Window", "Code", "Bug", "Bot", "Toolbox", "Search", "Data Bar Vertical", "Data Usage", "Flowchart", "Branch",
  "Box", "Cart", "Money", "Lightbulb", "Clock", "Location", "Flash", "Alert", "Warning", "Checkmark Circle", "Dismiss Circle", "Arrow Sync", "Hexagon",
];
const fluentItems = [];
for (const [n, name] of fluentIcons.entries()) {
  const file = `ic_fluent_${name.toLowerCase().replaceAll(" ", "_")}_24_regular.svg`;
  const res = await fetch(`${fluentBase}/assets/${encodeURIComponent(name)}/SVG/${file}`);
  if (!res.ok) throw new Error(`fluent ${name}: ${res.status}`);
  fluentItems.push({ id: `microsoft-${n + 1}`, name, status: "published", created: 0,
    elements: icon(await res.text(), `microsoft-${n + 1}`, { seed: 5000 + n * 100 }) });
}
await writeFile(new URL("microsoft.json", out), JSON.stringify({ type: "excalidrawlib", version: 2, libraryItems: fluentItems }) + "\n");
const fluentLicense = await (await fetch(`${fluentBase}/LICENSE`)).text();

lines.push(`archimate.json: ${relationships.length} relationships and junctions drawn for this project (ADR-0125), under its license`);
lines.push("", license.trim(), "");
lines.push(
  "microsoft.json: Fluent UI System Icons by Microsoft Corporation,",
  `github.com/microsoft/fluentui-system-icons, commit ${fluentCommit},`,
  `${fluentItems.length} icons turned into shapes, under the MIT license below.`,
  "",
  fluentLicense.trim(),
  "",
);
await writeFile(new URL("palettes.LICENSE.txt", out), lines.join("\n"));
