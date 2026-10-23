// Applies a chosen light or dark look before the page is drawn, so it
// does not flash in the system's look first (ADR-0095); theme.js does the
// rest once the app runs.
(function () {
  var v = "";
  try { v = localStorage.getItem("wwbb.theme") || ""; } catch (e) { /* follow the system */ }
  var link = document.getElementById("dark-css");
  if (link && (v === "dark" || v === "light")) {
    link.setAttribute("data-media", link.media);
    link.media = v === "dark" ? "screen" : "not all";
  }
})();
