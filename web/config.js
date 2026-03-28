window.IOT_FRONTEND_CONFIG = Object.assign(
  {
    // Update this for your Traefik-routed API origin.
    apiBaseUrl: "http://localhost:8080",
  },
  window.IOT_FRONTEND_CONFIG || {},
);
