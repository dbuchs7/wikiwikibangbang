// Entry point of the buildless UI shell (ADR-0009).
const status = document.getElementById("status");

try {
  const res = await fetch("/healthz", { cache: "no-store" });
  status.textContent = res.ok ? "running" : `unhealthy (HTTP ${res.status})`;
} catch {
  status.textContent = "unreachable";
}
