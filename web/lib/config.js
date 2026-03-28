export function getFrontendConfig() {
  const config = window.IOT_FRONTEND_CONFIG || {};
  return {
    apiBaseUrl: normalizeBaseUrl(config.apiBaseUrl || window.location.origin),
  };
}

function normalizeBaseUrl(value) {
  const normalized = String(value || "").trim();
  if (!normalized) {
    return window.location.origin;
  }

  return normalized.endsWith("/") ? normalized.slice(0, -1) : normalized;
}
