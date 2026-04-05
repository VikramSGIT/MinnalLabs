#!/bin/sh
# Register the Google Smart Home OAuth2 client in Hydra.
# Run this once after Hydra is up:
#   docker compose exec hydra sh /etc/scripts/create-hydra-client.sh

set -e

HYDRA_ADMIN="${HYDRA_ADMIN_URL:-http://127.0.0.1:4445}"
CLIENT_ID="${OAUTH_CLIENT_ID:-google-client}"
CLIENT_SECRET="${OAUTH_CLIENT_SECRET:?Set OAUTH_CLIENT_SECRET}"
REDIRECT_URI="${OAUTH_REDIRECT_URI:-https://oauth-redirect.googleusercontent.com/}"

# Delete existing client if present (idempotent).
hydra delete oauth2-client \
  --endpoint "$HYDRA_ADMIN" \
  "$CLIENT_ID" 2>/dev/null || true

hydra create oauth2-client \
  --endpoint "$HYDRA_ADMIN" \
  --id "$CLIENT_ID" \
  --secret "$CLIENT_SECRET" \
  --grant-types authorization_code,refresh_token \
  --response-types code \
  --scope openid,offline_access \
  --redirect-uris "$REDIRECT_URI" \
  --token-endpoint-auth-method client_secret_post

echo "OAuth2 client '$CLIENT_ID' registered successfully."
