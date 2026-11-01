// Applies a chosen light, dark or cockpit look before the page is drawn,
// so it does not flash in the system's look first (ADR-0095, ADR-0156);
// theme.js does the rest once the app runs. The cockpit is dark-surfaced.
(function () {
  var v = "";
  try { v = localStorage.getItem("ot.theme") || ""; } catch (e) { /* follow the system */ }
  var link = document.getElementById("dark-css");
  if (link && (v === "dark" || v === "light" || v === "cockpit")) {
    link.setAttribute("data-media", link.media);
    link.media = v === "light" ? "not all" : "screen";
  }
  if (v === "cockpit") document.documentElement.setAttribute("data-look", "cockpit");
})();
