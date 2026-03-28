#!/bin/sh
set -eu

API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"

cat > /srv/config.js <<EOF
window.IOT_FRONTEND_CONFIG = Object.assign(
  {
    apiBaseUrl: "${API_BASE_URL}",
  },
  window.IOT_FRONTEND_CONFIG || {},
);
EOF

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
