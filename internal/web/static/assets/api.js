// A thin client of the HTTP API (ADR-0028). Requests carry the session
// cookie; bodies are always JSON, which is also the CSRF defence (ADR-0027).

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

export async function api(method, path, body) {
  const init = { method, credentials: "same-origin", headers: {} };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
  let data = null;
  if (res.status !== 204) {
    const text = await res.text();
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { error: text };
      }
    }
  }
  if (!res.ok) throw new ApiError(res.status, (data && data.error) || res.statusText);
  return data;
}

export const get = (p) => api("GET", p);
export const post = (p, b = {}) => api("POST", p, b);
export const put = (p, b = {}) => api("PUT", p, b);
export const del = (p) => api("DELETE", p);

// upload sends a file as an attachment of an item (ADR-0045). The body is
// the file itself, as application/octet-stream, a type a cross-site form
// cannot send. source ({item, attachment}) names the file it is a copy or
// a new version of (ADR-0055).
export async function upload(itemPath, file, source) {
  const q = new URLSearchParams({ name: file.name });
  if (source) q.set("sourceItem", source.item), q.set("sourceAttachment", source.attachment);
  const res = await fetch(`${itemPath}/attachments?${q}`, {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/octet-stream" }, body: file,
  });
  let data = null;
  try { data = await res.json(); } catch { /* not JSON */ }
  if (!res.ok) throw new ApiError(res.status, (data && data.error) || res.statusText);
  return data.attachment;
}

// download POSTs body and returns the response as a file: its contents and
// the name the server suggests.
export async function download(path, body) {
  const res = await fetch(path, {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  if (!res.ok) {
    let message = res.statusText;
    try { message = (await res.json()).error || message; } catch { /* not JSON */ }
    throw new ApiError(res.status, message);
  }
  const name = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") || "")?.[1] || "download";
  return { blob: await res.blob(), name };
}
