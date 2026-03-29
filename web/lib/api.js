import { getFrontendConfig } from "./config.js";

export class ApiError extends Error {
  constructor(status, payload, message) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
  }
}

export async function postJSON(path, body, baseUrl = getFrontendConfig().apiBaseUrl) {
  return requestJSON(path, { method: "POST", body }, baseUrl);
}

export async function postFormData(path, body, baseUrl = getFrontendConfig().apiBaseUrl) {
  return requestJSON(path, { method: "POST", body }, baseUrl);
}

export async function requestJSON(path, options = {}, baseUrl = getFrontendConfig().apiBaseUrl) {
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");

  let body;
  if (options.body !== undefined) {
    if (options.body instanceof FormData) {
      body = options.body;
    } else {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(options.body);
    }
  }

  const response = await fetch(buildUrl(baseUrl, path), {
    method: options.method || "GET",
    headers,
    body,
    credentials: options.credentials || "include",
  });

  const payload = await parsePayload(response);
  if (!response.ok) {
    throw new ApiError(response.status, payload, getErrorMessage(response.status, payload));
  }

  return payload;
}

async function parsePayload(response) {
  const contentType = response.headers.get("content-type") || "";
  if (contentType.includes("application/json")) {
    return response.json();
  }

  const text = await response.text();
  if (!text) {
    return {};
  }

  return { error: text };
}

function getErrorMessage(status, payload) {
  if (payload && typeof payload === "object" && typeof payload.error === "string") {
    return payload.error;
  }

  return `Request failed with status ${status}.`;
}

function buildUrl(baseUrl, path) {
  const prefix = String(baseUrl || "").replace(/\/$/, "");
  const suffix = path.startsWith("/") ? path : `/${path}`;
  return `${prefix}${suffix}`;
}
