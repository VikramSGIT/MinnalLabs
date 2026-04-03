#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
config_path="${STRESS_CONFIG_PATH:-${script_dir}/config.env}"

if [[ -f "${config_path}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${config_path}"
  set +a
fi

BASE_URL="${BASE_URL:-http://backend:8080}"
PPROF_BASE_URL="${PPROF_BASE_URL:-http://backend:6060}"
CADDY_BASE_URL="${CADDY_BASE_URL:-http://caddy}"
WAIT_TIMEOUT_SECONDS="${WAIT_TIMEOUT_SECONDS:-180}"
SESSION_COOKIE_NAME="${SESSION_COOKIE_NAME:-user_session}"
OAUTH_CLIENT_ID="${OAUTH_CLIENT_ID:-google-client}"
OAUTH_REDIRECT_URI="${OAUTH_REDIRECT_URI:-http://127.0.0.1/oauth/callback}"
STRESS_HTTP_TIMEOUT="${STRESS_HTTP_TIMEOUT:-30s}"

STRESS_PRODUCT_PREFIX="${STRESS_PRODUCT_PREFIX:-${K6_PRODUCT_PREFIX:-stress-product-}}"
STRESS_EXPECTED_PRODUCT_COUNT="${STRESS_EXPECTED_PRODUCT_COUNT:-${K6_EXPECTED_PRODUCT_COUNT:-20}}"
STRESS_USER_COUNT="${STRESS_USER_COUNT:-${K6_USER_COUNT:-200}}"
STRESS_HOMES_PER_USER="${STRESS_HOMES_PER_USER:-${K6_HOMES_PER_USER:-2}}"
STRESS_DEVICES_PER_HOME="${STRESS_DEVICES_PER_HOME:-${K6_DEVICES_PER_HOME:-20}}"
STRESS_DELETE_DEVICES_PER_USER="${STRESS_DELETE_DEVICES_PER_USER:-${K6_DELETE_DEVICES_PER_USER:-10}}"
STRESS_DELETE_HOMES_PER_USER="${STRESS_DELETE_HOMES_PER_USER:-${K6_DELETE_HOMES_PER_USER:-1}}"
STRESS_DELETE_HOME_SLOT="${STRESS_DELETE_HOME_SLOT:-${K6_DELETE_HOME_SLOT:-0}}"
STRESS_DELETE_USERS_SELF_COUNT="${STRESS_DELETE_USERS_SELF_COUNT:-${K6_DELETE_USERS_SELF_COUNT:-100}}"
STRESS_DELETE_USERS_SELF_START_INDEX="${STRESS_DELETE_USERS_SELF_START_INDEX:-${K6_DELETE_USERS_SELF_START_INDEX:-0}}"
STRESS_DELETE_USERS_ADMIN_COUNT="${STRESS_DELETE_USERS_ADMIN_COUNT:-${K6_DELETE_USERS_ADMIN_COUNT:-100}}"
STRESS_DELETE_USERS_ADMIN_START_INDEX="${STRESS_DELETE_USERS_ADMIN_START_INDEX:-${K6_DELETE_USERS_ADMIN_START_INDEX:-${STRESS_DELETE_USERS_SELF_COUNT}}}"
STRESS_FULFILLMENT_REQUESTS_PER_DEVICE="${STRESS_FULFILLMENT_REQUESTS_PER_DEVICE:-${K6_FULFILLMENT_REQUESTS_PER_DEVICE:-6}}"
STRESS_FULFILLMENT_DEVICE_LIMIT="${STRESS_FULFILLMENT_DEVICE_LIMIT:-${K6_FULFILLMENT_DEVICE_LIMIT:-0}}"
STRESS_PHASE_BUFFER_SECONDS="${STRESS_PHASE_BUFFER_SECONDS:-${K6_SCENARIO_BUFFER_SECONDS:-0}}"
STRESS_DEVICE_PUBLIC_KEY="${STRESS_DEVICE_PUBLIC_KEY:-${K6_DEVICE_PUBLIC_KEY:-AQbnaqQshSiDwqVRxeH8lTij1x49dJjzhQqAwtbW4EI=}}"

ASYNC_HOME_READY_TIMEOUT_MS="${ASYNC_HOME_READY_TIMEOUT_MS:-90000}"
ASYNC_HOME_READY_POLL_MS="${ASYNC_HOME_READY_POLL_MS:-2000}"
ASYNC_HOME_EARLY_READY_CHECK_MS="${ASYNC_HOME_EARLY_READY_CHECK_MS:-9000}"
ASYNC_DRAIN_WAIT_SECONDS="${ASYNC_DRAIN_WAIT_SECONDS:-30}"
ASYNC_DRAIN_POLL_INTERVAL_SECONDS="${ASYNC_DRAIN_POLL_INTERVAL_SECONDS:-1}"
PPROF_PHASE_SECONDS="${PPROF_PHASE_SECONDS:-10}"

VEGETA_CREATE_USERS_WORKERS="${VEGETA_CREATE_USERS_WORKERS:-${K6_CREATE_USERS_VUS:-64}}"
VEGETA_CREATE_USERS_START_RPS="${VEGETA_CREATE_USERS_START_RPS:-0}"
VEGETA_CREATE_USERS_PEAK_RPS="${VEGETA_CREATE_USERS_PEAK_RPS:-5}"
VEGETA_CREATE_USERS_RAMP_UP_SECONDS="${VEGETA_CREATE_USERS_RAMP_UP_SECONDS:-${K6_CREATE_USERS_RAMP_UP_SECONDS:-${K6_CREATE_USERS_RAMP_SECONDS:-20}}}"
VEGETA_CREATE_USERS_HOLD_SECONDS="${VEGETA_CREATE_USERS_HOLD_SECONDS:-${K6_CREATE_USERS_HOLD_SECONDS:-20}}"
VEGETA_CREATE_USERS_RAMP_DOWN_SECONDS="${VEGETA_CREATE_USERS_RAMP_DOWN_SECONDS:-${K6_CREATE_USERS_RAMP_DOWN_SECONDS:-${K6_CREATE_USERS_RAMP_SECONDS:-20}}}"
VEGETA_CREATE_USERS_MAX_DURATION="${VEGETA_CREATE_USERS_MAX_DURATION:-${K6_CREATE_USERS_MAX_DURATION:-2m}}"

VEGETA_CREATE_HOMES_WORKERS="${VEGETA_CREATE_HOMES_WORKERS:-${K6_CREATE_HOMES_VUS:-64}}"
VEGETA_CREATE_HOMES_START_RPS="${VEGETA_CREATE_HOMES_START_RPS:-0}"
VEGETA_CREATE_HOMES_PEAK_RPS="${VEGETA_CREATE_HOMES_PEAK_RPS:-10}"
VEGETA_CREATE_HOMES_RAMP_UP_SECONDS="${VEGETA_CREATE_HOMES_RAMP_UP_SECONDS:-${K6_CREATE_HOMES_RAMP_UP_SECONDS:-${K6_CREATE_HOMES_RAMP_SECONDS:-20}}}"
VEGETA_CREATE_HOMES_HOLD_SECONDS="${VEGETA_CREATE_HOMES_HOLD_SECONDS:-${K6_CREATE_HOMES_HOLD_SECONDS:-20}}"
VEGETA_CREATE_HOMES_RAMP_DOWN_SECONDS="${VEGETA_CREATE_HOMES_RAMP_DOWN_SECONDS:-${K6_CREATE_HOMES_RAMP_DOWN_SECONDS:-${K6_CREATE_HOMES_RAMP_SECONDS:-20}}}"
VEGETA_CREATE_HOMES_MAX_DURATION="${VEGETA_CREATE_HOMES_MAX_DURATION:-${K6_CREATE_HOMES_MAX_DURATION:-4m}}"

VEGETA_ENROLL_DEVICES_WORKERS="${VEGETA_ENROLL_DEVICES_WORKERS:-${K6_ENROLL_DEVICES_VUS:-128}}"
VEGETA_ENROLL_DEVICES_START_RPS="${VEGETA_ENROLL_DEVICES_START_RPS:-0}"
VEGETA_ENROLL_DEVICES_PEAK_RPS="${VEGETA_ENROLL_DEVICES_PEAK_RPS:-100}"
VEGETA_ENROLL_DEVICES_RAMP_UP_SECONDS="${VEGETA_ENROLL_DEVICES_RAMP_UP_SECONDS:-${K6_ENROLL_DEVICES_RAMP_UP_SECONDS:-${K6_ENROLL_DEVICES_RAMP_SECONDS:-40}}}"
VEGETA_ENROLL_DEVICES_HOLD_SECONDS="${VEGETA_ENROLL_DEVICES_HOLD_SECONDS:-${K6_ENROLL_DEVICES_HOLD_SECONDS:-40}}"
VEGETA_ENROLL_DEVICES_RAMP_DOWN_SECONDS="${VEGETA_ENROLL_DEVICES_RAMP_DOWN_SECONDS:-${K6_ENROLL_DEVICES_RAMP_DOWN_SECONDS:-${K6_ENROLL_DEVICES_RAMP_SECONDS:-40}}}"
VEGETA_ENROLL_DEVICES_MAX_DURATION="${VEGETA_ENROLL_DEVICES_MAX_DURATION:-${K6_ENROLL_DEVICES_MAX_DURATION:-14m}}"

VEGETA_FULFILLMENT_MIX_WORKERS="${VEGETA_FULFILLMENT_MIX_WORKERS:-${K6_FULFILLMENT_MIX_VUS:-128}}"
VEGETA_FULFILLMENT_MIX_START_RPS="${VEGETA_FULFILLMENT_MIX_START_RPS:-0}"
VEGETA_FULFILLMENT_MIX_PEAK_RPS="${VEGETA_FULFILLMENT_MIX_PEAK_RPS:-80}"
VEGETA_FULFILLMENT_MIX_RAMP_UP_SECONDS="${VEGETA_FULFILLMENT_MIX_RAMP_UP_SECONDS:-${K6_FULFILLMENT_MIX_RAMP_UP_SECONDS:-${K6_FULFILLMENT_MIX_RAMP_SECONDS:-40}}}"
VEGETA_FULFILLMENT_MIX_HOLD_SECONDS="${VEGETA_FULFILLMENT_MIX_HOLD_SECONDS:-${K6_FULFILLMENT_MIX_HOLD_SECONDS:-40}}"
VEGETA_FULFILLMENT_MIX_RAMP_DOWN_SECONDS="${VEGETA_FULFILLMENT_MIX_RAMP_DOWN_SECONDS:-${K6_FULFILLMENT_MIX_RAMP_DOWN_SECONDS:-${K6_FULFILLMENT_MIX_RAMP_SECONDS:-40}}}"
VEGETA_FULFILLMENT_MIX_MAX_DURATION="${VEGETA_FULFILLMENT_MIX_MAX_DURATION:-${K6_FULFILLMENT_MIX_MAX_DURATION:-10m}}"

VEGETA_DELETE_DEVICES_WORKERS="${VEGETA_DELETE_DEVICES_WORKERS:-${K6_DELETE_DEVICES_VUS:-64}}"
VEGETA_DELETE_DEVICES_START_RPS="${VEGETA_DELETE_DEVICES_START_RPS:-0}"
VEGETA_DELETE_DEVICES_PEAK_RPS="${VEGETA_DELETE_DEVICES_PEAK_RPS:-50}"
VEGETA_DELETE_DEVICES_RAMP_UP_SECONDS="${VEGETA_DELETE_DEVICES_RAMP_UP_SECONDS:-${K6_DELETE_DEVICES_RAMP_UP_SECONDS:-${K6_DELETE_DEVICES_RAMP_SECONDS:-20}}}"
VEGETA_DELETE_DEVICES_HOLD_SECONDS="${VEGETA_DELETE_DEVICES_HOLD_SECONDS:-${K6_DELETE_DEVICES_HOLD_SECONDS:-20}}"
VEGETA_DELETE_DEVICES_RAMP_DOWN_SECONDS="${VEGETA_DELETE_DEVICES_RAMP_DOWN_SECONDS:-${K6_DELETE_DEVICES_RAMP_DOWN_SECONDS:-${K6_DELETE_DEVICES_RAMP_SECONDS:-20}}}"
VEGETA_DELETE_DEVICES_MAX_DURATION="${VEGETA_DELETE_DEVICES_MAX_DURATION:-${K6_DELETE_DEVICES_MAX_DURATION:-3m}}"

VEGETA_DELETE_HOMES_WORKERS="${VEGETA_DELETE_HOMES_WORKERS:-${K6_DELETE_HOMES_VUS:-32}}"
VEGETA_DELETE_HOMES_START_RPS="${VEGETA_DELETE_HOMES_START_RPS:-0}"
VEGETA_DELETE_HOMES_PEAK_RPS="${VEGETA_DELETE_HOMES_PEAK_RPS:-5}"
VEGETA_DELETE_HOMES_RAMP_UP_SECONDS="${VEGETA_DELETE_HOMES_RAMP_UP_SECONDS:-${K6_DELETE_HOMES_RAMP_UP_SECONDS:-${K6_DELETE_HOMES_RAMP_SECONDS:-20}}}"
VEGETA_DELETE_HOMES_HOLD_SECONDS="${VEGETA_DELETE_HOMES_HOLD_SECONDS:-${K6_DELETE_HOMES_HOLD_SECONDS:-20}}"
VEGETA_DELETE_HOMES_RAMP_DOWN_SECONDS="${VEGETA_DELETE_HOMES_RAMP_DOWN_SECONDS:-${K6_DELETE_HOMES_RAMP_DOWN_SECONDS:-${K6_DELETE_HOMES_RAMP_SECONDS:-20}}}"
VEGETA_DELETE_HOMES_MAX_DURATION="${VEGETA_DELETE_HOMES_MAX_DURATION:-${K6_DELETE_HOMES_MAX_DURATION:-2m}}"

VEGETA_DELETE_USERS_SELF_WORKERS="${VEGETA_DELETE_USERS_SELF_WORKERS:-${K6_DELETE_USERS_SELF_VUS:-16}}"
VEGETA_DELETE_USERS_SELF_START_RPS="${VEGETA_DELETE_USERS_SELF_START_RPS:-0}"
VEGETA_DELETE_USERS_SELF_PEAK_RPS="${VEGETA_DELETE_USERS_SELF_PEAK_RPS:-10}"
VEGETA_DELETE_USERS_SELF_RAMP_UP_SECONDS="${VEGETA_DELETE_USERS_SELF_RAMP_UP_SECONDS:-${K6_DELETE_USERS_SELF_RAMP_UP_SECONDS:-${K6_DELETE_USERS_SELF_RAMP_SECONDS:-10}}}"
VEGETA_DELETE_USERS_SELF_HOLD_SECONDS="${VEGETA_DELETE_USERS_SELF_HOLD_SECONDS:-${K6_DELETE_USERS_SELF_HOLD_SECONDS:-10}}"
VEGETA_DELETE_USERS_SELF_RAMP_DOWN_SECONDS="${VEGETA_DELETE_USERS_SELF_RAMP_DOWN_SECONDS:-${K6_DELETE_USERS_SELF_RAMP_DOWN_SECONDS:-${K6_DELETE_USERS_SELF_RAMP_SECONDS:-10}}}"
VEGETA_DELETE_USERS_SELF_MAX_DURATION="${VEGETA_DELETE_USERS_SELF_MAX_DURATION:-${K6_DELETE_USERS_SELF_MAX_DURATION:-2m}}"

VEGETA_DELETE_USERS_ADMIN_WORKERS="${VEGETA_DELETE_USERS_ADMIN_WORKERS:-${K6_DELETE_USERS_ADMIN_VUS:-16}}"
VEGETA_DELETE_USERS_ADMIN_START_RPS="${VEGETA_DELETE_USERS_ADMIN_START_RPS:-0}"
VEGETA_DELETE_USERS_ADMIN_PEAK_RPS="${VEGETA_DELETE_USERS_ADMIN_PEAK_RPS:-10}"
VEGETA_DELETE_USERS_ADMIN_RAMP_UP_SECONDS="${VEGETA_DELETE_USERS_ADMIN_RAMP_UP_SECONDS:-${K6_DELETE_USERS_ADMIN_RAMP_UP_SECONDS:-${K6_DELETE_USERS_ADMIN_RAMP_SECONDS:-10}}}"
VEGETA_DELETE_USERS_ADMIN_HOLD_SECONDS="${VEGETA_DELETE_USERS_ADMIN_HOLD_SECONDS:-${K6_DELETE_USERS_ADMIN_HOLD_SECONDS:-10}}"
VEGETA_DELETE_USERS_ADMIN_RAMP_DOWN_SECONDS="${VEGETA_DELETE_USERS_ADMIN_RAMP_DOWN_SECONDS:-${K6_DELETE_USERS_ADMIN_RAMP_DOWN_SECONDS:-${K6_DELETE_USERS_ADMIN_RAMP_SECONDS:-10}}}"
VEGETA_DELETE_USERS_ADMIN_MAX_DURATION="${VEGETA_DELETE_USERS_ADMIN_MAX_DURATION:-${K6_DELETE_USERS_ADMIN_MAX_DURATION:-2m}}"

DB_HOST="${DB_HOST:-${DATABASE_HOST:-localhost}}"
DB_PORT="${DB_PORT:-${DATABASE_PORT:-5432}}"
DB_USER="${DB_USER:-${DATABASE_USER:-}}"
DB_PASSWORD="${DB_PASSWORD:-${DATABASE_PASSWORD:-}}"
DB_NAME="${DB_NAME:-${DATABASE_NAME:-}}"
DB_SSLMODE="${DB_SSLMODE:-disable}"

: "${OAUTH_CLIENT_SECRET:?set OAUTH_CLIENT_SECRET}"
: "${DB_USER:?set DB_USER or DATABASE_USER}"
: "${DB_PASSWORD:?set DB_PASSWORD or DATABASE_PASSWORD}"
: "${DB_NAME:?set DB_NAME or DATABASE_NAME}"

results_dir="${repo_root}/tests/results"
artifacts_dir="${results_dir}/artifacts"
pprof_dir="${results_dir}/pprof"
phase_artifacts_dir="${artifacts_dir}/phases"
report_path="${results_dir}/report.html"
k6_summary_path="${artifacts_dir}/k6-summary.json"
k6_raw_path="${artifacts_dir}/k6-raw.ndjson"
metadata_path="${artifacts_dir}/run-metadata.json"
async_jobs_path="${artifacts_dir}/async-jobs.json"
phase_state_path="${artifacts_dir}/phase-state.json"
goroutine_path="${pprof_dir}/goroutine.txt"
heap_profile_path="${pprof_dir}/heap.pb.gz"
heap_top_path="${pprof_dir}/heap-top.txt"
heap_inuse_top_path="${pprof_dir}/heap-inuse-top.txt"
runner_bin="${artifacts_dir}/vegeta-phase-runner"

export BASE_URL PPROF_BASE_URL CADDY_BASE_URL WAIT_TIMEOUT_SECONDS SESSION_COOKIE_NAME OAUTH_CLIENT_ID OAUTH_CLIENT_SECRET OAUTH_REDIRECT_URI
export STRESS_HTTP_TIMEOUT STRESS_PRODUCT_PREFIX STRESS_EXPECTED_PRODUCT_COUNT STRESS_USER_COUNT STRESS_HOMES_PER_USER STRESS_DEVICES_PER_HOME
export STRESS_DELETE_DEVICES_PER_USER STRESS_DELETE_HOMES_PER_USER STRESS_DELETE_HOME_SLOT STRESS_DELETE_USERS_SELF_COUNT STRESS_DELETE_USERS_SELF_START_INDEX
export STRESS_DELETE_USERS_ADMIN_COUNT STRESS_DELETE_USERS_ADMIN_START_INDEX STRESS_FULFILLMENT_REQUESTS_PER_DEVICE STRESS_FULFILLMENT_DEVICE_LIMIT
export STRESS_PHASE_BUFFER_SECONDS STRESS_DEVICE_PUBLIC_KEY
export ASYNC_HOME_READY_TIMEOUT_MS ASYNC_HOME_READY_POLL_MS ASYNC_HOME_EARLY_READY_CHECK_MS ASYNC_DRAIN_WAIT_SECONDS ASYNC_DRAIN_POLL_INTERVAL_SECONDS
export PPROF_PHASE_SECONDS DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_SSLMODE

mkdir -p "${results_dir}" "${artifacts_dir}" "${pprof_dir}" "${phase_artifacts_dir}"
rm -f \
  "${report_path}" \
  "${k6_summary_path}" \
  "${k6_raw_path}" \
  "${metadata_path}" \
  "${async_jobs_path}" \
  "${phase_state_path}" \
  "${goroutine_path}" \
  "${heap_profile_path}" \
  "${heap_top_path}" \
  "${heap_inuse_top_path}" \
  "${runner_bin}" \
  "${pprof_dir}/"*-cpu.pb.gz \
  "${pprof_dir}/"*-cpu-top.txt \
  "${phase_artifacts_dir}/"*

python3 - "${k6_summary_path}" "${async_jobs_path}" <<'PY'
import json
import sys

summary_path = sys.argv[1]
async_jobs_path = sys.argv[2]

with open(summary_path, "w", encoding="utf-8") as handle:
    json.dump({"metrics": {}}, handle)

with open(async_jobs_path, "w", encoding="utf-8") as handle:
    json.dump({
        "all_async_jobs_completed": False,
        "error": "async audit did not run",
        "remaining_job_total": -1,
        "remaining_active_job_total": -1,
        "remaining_failed_job_total": -1,
        "remaining_home_total": -1,
        "status_counts": {},
        "operation_status_counts": [],
        "home_state_counts": {},
        "active_jobs": [],
        "failed_jobs": [],
        "global_job_total": -1,
        "global_status_counts": {},
        "timeline": [],
    }, handle, indent=2)
PY

python3 - "${metadata_path}" "${phase_state_path}" <<'PY'
import json
import sys

metadata_path = sys.argv[1]
phase_state_path = sys.argv[2]

with open(metadata_path, "w", encoding="utf-8") as handle:
    json.dump({"phases": []}, handle, indent=2)

with open(phase_state_path, "w", encoding="utf-8") as handle:
    json.dump({"products": [], "users": {}}, handle, indent=2)
PY

cat >"${report_path}" <<'EOF'
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Phase Load Dashboard</title>
  <style>
    body { font-family: Arial, sans-serif; background: #0b1020; color: #e7ecf7; padding: 32px; }
    .panel { max-width: 960px; margin: 0 auto; background: #121a30; border: 1px solid #334155; border-radius: 16px; padding: 24px; }
    code { background: #17213d; padding: 2px 6px; border-radius: 6px; }
  </style>
</head>
<body>
  <div class="panel">
    <h1>Phase Load Dashboard</h1>
    <p>The stress run did not finish rendering a final dashboard.</p>
    <p>Check the raw artifacts under <code>tests/results/artifacts/</code> and <code>tests/results/pprof/</code>.</p>
  </div>
</body>
</html>
EOF

printf 'Goroutine dump unavailable.\n' >"${goroutine_path}"
printf 'Heap profile unavailable.\n' >"${heap_top_path}"
printf 'Heap in-use profile unavailable.\n' >"${heap_inuse_top_path}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

duration_to_seconds() {
  local value="$1"
  case "${value}" in
    *h) echo $(( ${value%h} * 3600 )) ;;
    *m) echo $(( ${value%m} * 60 )) ;;
    *s) echo $(( ${value%s} )) ;;
    *)
      echo "Unsupported duration format: ${value}" >&2
      exit 1
      ;;
  esac
}

run_id="$(date +%s)_$RANDOM"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
admin_username="vegeta_admin_${run_id}"
admin_password="StressAdminPass123!"
admin_response_file="$(mktemp)"

cleanup() {
  rm -f "${admin_response_file}"
}
trap cleanup EXIT

require_cmd curl
require_cmd psql
require_cmd python3
require_cmd go

curl_flags=(-sS)
curl_ready_flags=(-fsS)
if [[ "${CURL_INSECURE:-false}" == "true" ]]; then
  curl_flags+=(-k)
  curl_ready_flags+=(-k)
fi

wait_for_http() {
  local label="$1"
  local target_url="$2"
  local started_at
  started_at="$(date +%s)"
  while true; do
    if curl "${curl_ready_flags[@]}" -o /dev/null "${target_url}"; then
      echo "${label} is ready at ${target_url}"
      return 0
    fi
    if (( "$(date +%s)" - started_at >= WAIT_TIMEOUT_SECONDS )); then
      echo "Timed out waiting for ${label} at ${target_url}" >&2
      return 1
    fi
    sleep 2
  done
}

wait_for_http_any_status() {
  local label="$1"
  local target_url="$2"
  local started_at
  started_at="$(date +%s)"
  while true; do
    if curl "${curl_flags[@]}" -o /dev/null "${target_url}"; then
      echo "${label} is reachable at ${target_url}"
      return 0
    fi
    if (( "$(date +%s)" - started_at >= WAIT_TIMEOUT_SECONDS )); then
      echo "Timed out waiting for ${label} at ${target_url}" >&2
      return 1
    fi
    sleep 2
  done
}

wait_for_postgres() {
  local started_at
  started_at="$(date +%s)"
  while true; do
    if PGPASSWORD="${DB_PASSWORD}" psql \
      "host=${DB_HOST} port=${DB_PORT} user=${DB_USER} dbname=${DB_NAME} sslmode=${DB_SSLMODE}" \
      -tAc 'SELECT 1' >/dev/null 2>&1; then
      echo "PostgreSQL is ready at ${DB_HOST}:${DB_PORT}"
      return 0
    fi
    if (( "$(date +%s)" - started_at >= WAIT_TIMEOUT_SECONDS )); then
      echo "Timed out waiting for PostgreSQL at ${DB_HOST}:${DB_PORT}" >&2
      return 1
    fi
    sleep 2
  done
}

wait_for_tcp() {
  local label="$1"
  local host="$2"
  local port="$3"
  local started_at
  started_at="$(date +%s)"
  while true; do
    if python3 - "$host" "$port" <<'PY'
import socket
import sys

host = sys.argv[1]
port = int(sys.argv[2])
with socket.create_connection((host, port), timeout=2):
    pass
PY
    then
      echo "${label} is ready at ${host}:${port}"
      return 0
    fi
    if (( "$(date +%s)" - started_at >= WAIT_TIMEOUT_SECONDS )); then
      echo "Timed out waiting for ${label} at ${host}:${port}" >&2
      return 1
    fi
    sleep 2
  done
}

run_pprof_capture() {
  local output_path="$1"
  shift
  if ! "$@" >"${output_path}" 2>&1; then
    {
      echo "Failed to generate ${output_path##*/}"
      echo
      python3 - "$output_path" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace"))
PY
    } >"${output_path}.tmp"
    mv "${output_path}.tmp" "${output_path}"
  fi
}

collect_async_jobs_audit() {
  local output_path="$1"

  python3 - "${output_path}" <<'PY'
import json
import os
import subprocess
import sys
import time
from pathlib import Path

output_path = Path(sys.argv[1])
timeout_seconds = int(os.environ["ASYNC_DRAIN_WAIT_SECONDS"])
poll_interval_seconds = max(1, int(os.environ["ASYNC_DRAIN_POLL_INTERVAL_SECONDS"]))
run_started_at = os.environ["RUN_STARTED_AT"]
connection = (
    f"host={os.environ['DB_HOST']} "
    f"port={os.environ['DB_PORT']} "
    f"user={os.environ['DB_USER']} "
    f"dbname={os.environ['DB_NAME']} "
    f"sslmode={os.environ['DB_SSLMODE']}"
)

escaped_started_at = run_started_at.replace("'", "''")
sql = """
WITH current_jobs AS (
    SELECT id, home_id, operation, status, attempts, next_run_at, claimed_at, last_error, created_at, updated_at
    FROM home_mqtt_jobs
    WHERE created_at >= '__RUN_STARTED_AT__'::timestamptz
),
current_homes AS (
    SELECT id, mqtt_provision_state, mqtt_provision_error, created_at, updated_at
    FROM homes
    WHERE created_at >= '__RUN_STARTED_AT__'::timestamptz
      AND deleted_at IS NULL
),
job_counts AS (
    SELECT operation, status, COUNT(*) AS count
    FROM current_jobs
    GROUP BY operation, status
),
status_counts AS (
    SELECT status, COUNT(*) AS count
    FROM current_jobs
    GROUP BY status
),
home_state_counts AS (
    SELECT mqtt_provision_state AS state, COUNT(*) AS count
    FROM current_homes
    GROUP BY mqtt_provision_state
),
global_status_counts AS (
    SELECT status, COUNT(*) AS count
    FROM home_mqtt_jobs
    GROUP BY status
),
active_jobs AS (
    SELECT COALESCE(
        json_agg(
            json_build_object(
                'id', id,
                'home_id', home_id,
                'operation', operation,
                'status', status,
                'attempts', attempts,
                'next_run_at', next_run_at,
                'claimed_at', claimed_at,
                'last_error', last_error,
                'created_at', created_at,
                'updated_at', updated_at
            )
            ORDER BY next_run_at ASC, id ASC
        ),
        '[]'::json
    ) AS rows
    FROM (
        SELECT *
        FROM current_jobs
        WHERE status IN ('pending', 'running')
        ORDER BY next_run_at ASC, id ASC
        LIMIT 10
    ) sample
),
failed_jobs AS (
    SELECT COALESCE(
        json_agg(
            json_build_object(
                'id', id,
                'home_id', home_id,
                'operation', operation,
                'status', status,
                'attempts', attempts,
                'next_run_at', next_run_at,
                'claimed_at', claimed_at,
                'last_error', last_error,
                'created_at', created_at,
                'updated_at', updated_at
            )
            ORDER BY updated_at DESC, id DESC
        ),
        '[]'::json
    ) AS rows
    FROM (
        SELECT *
        FROM current_jobs
        WHERE status = 'failed'
        ORDER BY updated_at DESC, id DESC
        LIMIT 10
    ) sample
)
SELECT json_build_object(
    'all_async_jobs_completed',
        NOT EXISTS (
            SELECT 1
            FROM current_jobs
            WHERE status IN ('pending', 'running', 'failed')
        )
        AND NOT EXISTS (SELECT 1 FROM current_homes),
    'remaining_job_total', COALESCE((SELECT COUNT(*) FROM current_jobs), 0),
    'remaining_active_job_total', COALESCE((SELECT SUM(count) FROM status_counts WHERE status IN ('pending', 'running')), 0),
    'remaining_failed_job_total', COALESCE((SELECT SUM(count) FROM status_counts WHERE status = 'failed'), 0),
    'remaining_home_total', COALESCE((SELECT COUNT(*) FROM current_homes), 0),
    'status_counts', COALESCE((SELECT json_object_agg(status, count) FROM status_counts), '{}'::json),
    'operation_status_counts', COALESCE((
        SELECT json_agg(
            json_build_object(
                'operation', operation,
                'status', status,
                'count', count
            )
            ORDER BY operation, status
        )
        FROM job_counts
    ), '[]'::json),
    'home_state_counts', COALESCE((SELECT json_object_agg(state, count) FROM home_state_counts), '{}'::json),
    'active_jobs', (SELECT rows FROM active_jobs),
    'failed_jobs', (SELECT rows FROM failed_jobs),
    'global_job_total', COALESCE((SELECT COUNT(*) FROM home_mqtt_jobs), 0),
    'global_status_counts', COALESCE((SELECT json_object_agg(status, count) FROM global_status_counts), '{}'::json)
);
""".replace("__RUN_STARTED_AT__", escaped_started_at)

def run_query():
    env = os.environ.copy()
    env["PGPASSWORD"] = os.environ["DB_PASSWORD"]
    result = subprocess.run(
        ["psql", connection, "-At", "-c", sql],
        check=True,
        capture_output=True,
        text=True,
        env=env,
    )
    payload = result.stdout.strip()
    return json.loads(payload) if payload else {}

def current_timestamp():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def build_timeline_point(snapshot, elapsed_seconds, captured_at):
    status_counts = snapshot.get("status_counts") or {}
    return {
        "elapsed_seconds": elapsed_seconds,
        "captured_at": captured_at,
        "remaining_job_total": int(snapshot.get("remaining_job_total", 0) or 0),
        "remaining_active_job_total": int(snapshot.get("remaining_active_job_total", 0) or 0),
        "remaining_failed_job_total": int(snapshot.get("remaining_failed_job_total", 0) or 0),
        "remaining_home_total": int(snapshot.get("remaining_home_total", 0) or 0),
        "pending_job_total": int(status_counts.get("pending", 0) or 0),
        "running_job_total": int(status_counts.get("running", 0) or 0),
        "failed_job_total": int(status_counts.get("failed", 0) or 0),
    }

def write_snapshot(snapshot, timeline, timed_out):
    snapshot["captured_at"] = current_timestamp()
    snapshot["drain_wait_seconds"] = timeout_seconds
    snapshot["drain_poll_interval_seconds"] = poll_interval_seconds
    snapshot["timed_out"] = timed_out
    snapshot["timeline"] = timeline
    snapshot["scope"] = {
        "run_started_at": run_started_at,
        "jobs_created_since_run_start": True,
        "homes_created_since_run_start": True,
    }
    output_path.write_text(json.dumps(snapshot, indent=2), encoding="utf-8")

deadline = time.time() + timeout_seconds
drain_started_at = time.time()
timeline = []

try:
    while True:
        snapshot = run_query()
        captured_at = current_timestamp()
        elapsed_seconds = max(0, int(round(time.time() - drain_started_at)))
        timeline.append(build_timeline_point(snapshot, elapsed_seconds, captured_at))
        write_snapshot(snapshot, timeline, timed_out=False)

        if snapshot.get("all_async_jobs_completed"):
            sys.exit(0)

        if time.time() >= deadline:
            write_snapshot(snapshot, timeline, timed_out=True)
            sys.exit(1)

        time.sleep(poll_interval_seconds)
except Exception as exc:
    write_snapshot(
        {
            "all_async_jobs_completed": False,
            "error": str(exc),
            "remaining_job_total": -1,
            "remaining_active_job_total": -1,
            "remaining_failed_job_total": -1,
            "remaining_home_total": -1,
            "status_counts": {},
            "operation_status_counts": [],
            "home_state_counts": {},
            "active_jobs": [],
            "failed_jobs": [],
            "global_job_total": -1,
            "global_status_counts": {},
        },
        timeline,
        timed_out=True,
    )
    sys.exit(1)
PY
}

declare -a phase_names=()
declare -a phase_start_seconds=()
declare -a phase_duration_seconds=()
declare -a phase_profile_seconds=()
declare -a phase_iterations=()
declare -a phase_workers=()
declare -a phase_start_rps=()
declare -a phase_peak_rps=()
declare -a phase_ramp_up_seconds=()
declare -a phase_hold_seconds=()
declare -a phase_ramp_down_seconds=()
declare -a phase_max_durations=()

phase_cursor_seconds=0

register_phase() {
  local name="$1"
  local iterations="$2"
  local workers="$3"
  local start_rps="$4"
  local peak_rps="$5"
  local ramp_up_seconds="$6"
  local hold_seconds="$7"
  local ramp_down_seconds="$8"
  local max_duration="$9"

  if (( iterations <= 0 )); then
    return
  fi

  local duration_seconds
  duration_seconds="$(duration_to_seconds "${max_duration}")"
  local capture_seconds="${PPROF_PHASE_SECONDS}"
  if (( capture_seconds > duration_seconds )); then
    capture_seconds="${duration_seconds}"
  fi
  if (( capture_seconds < 1 )); then
    capture_seconds=1
  fi

  phase_names+=("${name}")
  phase_start_seconds+=("${phase_cursor_seconds}")
  phase_duration_seconds+=("${duration_seconds}")
  phase_profile_seconds+=("${capture_seconds}")
  phase_iterations+=("${iterations}")
  phase_workers+=("${workers}")
  phase_start_rps+=("${start_rps}")
  phase_peak_rps+=("${peak_rps}")
  phase_ramp_up_seconds+=("${ramp_up_seconds}")
  phase_hold_seconds+=("${hold_seconds}")
  phase_ramp_down_seconds+=("${ramp_down_seconds}")
  phase_max_durations+=("${max_duration}")

  phase_cursor_seconds=$(( phase_cursor_seconds + duration_seconds + STRESS_PHASE_BUFFER_SECONDS ))
}

create_homes_total=$(( STRESS_USER_COUNT * STRESS_HOMES_PER_USER ))
enroll_devices_total=$(( create_homes_total * STRESS_DEVICES_PER_HOME ))
if (( STRESS_FULFILLMENT_DEVICE_LIMIT > 0 && STRESS_FULFILLMENT_DEVICE_LIMIT < enroll_devices_total )); then
  fulfillment_mix_total="${STRESS_FULFILLMENT_DEVICE_LIMIT}"
else
  fulfillment_mix_total="${enroll_devices_total}"
fi
delete_devices_total=$(( STRESS_USER_COUNT * STRESS_DELETE_DEVICES_PER_USER ))
delete_homes_total=$(( STRESS_USER_COUNT * STRESS_DELETE_HOMES_PER_USER ))

register_phase "create_users" "${STRESS_USER_COUNT}" "${VEGETA_CREATE_USERS_WORKERS}" "${VEGETA_CREATE_USERS_START_RPS}" "${VEGETA_CREATE_USERS_PEAK_RPS}" "${VEGETA_CREATE_USERS_RAMP_UP_SECONDS}" "${VEGETA_CREATE_USERS_HOLD_SECONDS}" "${VEGETA_CREATE_USERS_RAMP_DOWN_SECONDS}" "${VEGETA_CREATE_USERS_MAX_DURATION}"
register_phase "google_oauth_enroll" "${STRESS_USER_COUNT}" "${VEGETA_GOOGLE_OAUTH_ENROLL_WORKERS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_START_RPS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_PEAK_RPS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_RAMP_UP_SECONDS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_HOLD_SECONDS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_RAMP_DOWN_SECONDS}" "${VEGETA_GOOGLE_OAUTH_ENROLL_MAX_DURATION}"
register_phase "google_signin_enroll" "${STRESS_USER_COUNT}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_WORKERS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_START_RPS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_PEAK_RPS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_RAMP_UP_SECONDS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_HOLD_SECONDS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_RAMP_DOWN_SECONDS}" "${VEGETA_GOOGLE_SIGNIN_ENROLL_MAX_DURATION}"
register_phase "create_homes" "${create_homes_total}" "${VEGETA_CREATE_HOMES_WORKERS}" "${VEGETA_CREATE_HOMES_START_RPS}" "${VEGETA_CREATE_HOMES_PEAK_RPS}" "${VEGETA_CREATE_HOMES_RAMP_UP_SECONDS}" "${VEGETA_CREATE_HOMES_HOLD_SECONDS}" "${VEGETA_CREATE_HOMES_RAMP_DOWN_SECONDS}" "${VEGETA_CREATE_HOMES_MAX_DURATION}"
register_phase "enroll_devices" "${enroll_devices_total}" "${VEGETA_ENROLL_DEVICES_WORKERS}" "${VEGETA_ENROLL_DEVICES_START_RPS}" "${VEGETA_ENROLL_DEVICES_PEAK_RPS}" "${VEGETA_ENROLL_DEVICES_RAMP_UP_SECONDS}" "${VEGETA_ENROLL_DEVICES_HOLD_SECONDS}" "${VEGETA_ENROLL_DEVICES_RAMP_DOWN_SECONDS}" "${VEGETA_ENROLL_DEVICES_MAX_DURATION}"
register_phase "fulfillment_mix" "${fulfillment_mix_total}" "${VEGETA_FULFILLMENT_MIX_WORKERS}" "${VEGETA_FULFILLMENT_MIX_START_RPS}" "${VEGETA_FULFILLMENT_MIX_PEAK_RPS}" "${VEGETA_FULFILLMENT_MIX_RAMP_UP_SECONDS}" "${VEGETA_FULFILLMENT_MIX_HOLD_SECONDS}" "${VEGETA_FULFILLMENT_MIX_RAMP_DOWN_SECONDS}" "${VEGETA_FULFILLMENT_MIX_MAX_DURATION}"
register_phase "delete_devices" "${delete_devices_total}" "${VEGETA_DELETE_DEVICES_WORKERS}" "${VEGETA_DELETE_DEVICES_START_RPS}" "${VEGETA_DELETE_DEVICES_PEAK_RPS}" "${VEGETA_DELETE_DEVICES_RAMP_UP_SECONDS}" "${VEGETA_DELETE_DEVICES_HOLD_SECONDS}" "${VEGETA_DELETE_DEVICES_RAMP_DOWN_SECONDS}" "${VEGETA_DELETE_DEVICES_MAX_DURATION}"
register_phase "delete_homes" "${delete_homes_total}" "${VEGETA_DELETE_HOMES_WORKERS}" "${VEGETA_DELETE_HOMES_START_RPS}" "${VEGETA_DELETE_HOMES_PEAK_RPS}" "${VEGETA_DELETE_HOMES_RAMP_UP_SECONDS}" "${VEGETA_DELETE_HOMES_HOLD_SECONDS}" "${VEGETA_DELETE_HOMES_RAMP_DOWN_SECONDS}" "${VEGETA_DELETE_HOMES_MAX_DURATION}"
register_phase "delete_users_self" "${STRESS_DELETE_USERS_SELF_COUNT}" "${VEGETA_DELETE_USERS_SELF_WORKERS}" "${VEGETA_DELETE_USERS_SELF_START_RPS}" "${VEGETA_DELETE_USERS_SELF_PEAK_RPS}" "${VEGETA_DELETE_USERS_SELF_RAMP_UP_SECONDS}" "${VEGETA_DELETE_USERS_SELF_HOLD_SECONDS}" "${VEGETA_DELETE_USERS_SELF_RAMP_DOWN_SECONDS}" "${VEGETA_DELETE_USERS_SELF_MAX_DURATION}"
register_phase "delete_users_admin" "${STRESS_DELETE_USERS_ADMIN_COUNT}" "${VEGETA_DELETE_USERS_ADMIN_WORKERS}" "${VEGETA_DELETE_USERS_ADMIN_START_RPS}" "${VEGETA_DELETE_USERS_ADMIN_PEAK_RPS}" "${VEGETA_DELETE_USERS_ADMIN_RAMP_UP_SECONDS}" "${VEGETA_DELETE_USERS_ADMIN_HOLD_SECONDS}" "${VEGETA_DELETE_USERS_ADMIN_RAMP_DOWN_SECONDS}" "${VEGETA_DELETE_USERS_ADMIN_MAX_DURATION}"

initialize_phase_state() {
  local products_json
  products_json="$(
    PGPASSWORD="${DB_PASSWORD}" psql \
      "host=${DB_HOST} port=${DB_PORT} user=${DB_USER} dbname=${DB_NAME} sslmode=${DB_SSLMODE}" \
      -At \
      -c "SELECT COALESCE(json_agg(json_build_object('product_id', id, 'name', name) ORDER BY name ASC), '[]'::json) FROM products WHERE name LIKE '${STRESS_PRODUCT_PREFIX}%';"
  )"

  python3 - "${phase_state_path}" "${products_json}" <<'PY'
import json
import sys

output_path = sys.argv[1]
products = json.loads(sys.argv[2])

payload = {
    "products": products,
    "users": {},
}

with open(output_path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2)
PY
}

append_phase_raw() {
  local phase_raw_path="$1"
  if [[ -f "${phase_raw_path}" ]]; then
    python3 - "${phase_raw_path}" "${k6_raw_path}" <<'PY'
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])

with src.open("r", encoding="utf-8", errors="replace") as source, dst.open("a", encoding="utf-8") as target:
    for line in source:
        target.write(line)
PY
  fi
}

run_phase() {
  local phase_name="$1"
  local iterations_total="$2"
  local phase_workers="$3"
  local start_rps="$4"
  local peak_rps="$5"
  local ramp_up_seconds="$6"
  local hold_seconds="$7"
  local ramp_down_seconds="$8"
  local max_duration="$9"
  local capture_seconds="${10}"

  if (( iterations_total <= 0 )); then
    return 0
  fi

  local phase_raw_path="${phase_artifacts_dir}/${phase_name}-raw.ndjson"
  local phase_summary_path="${phase_artifacts_dir}/${phase_name}-summary.json"
  local phase_console_path="${phase_artifacts_dir}/${phase_name}-console.log"
  local profile_path="${pprof_dir}/${phase_name}-cpu.pb.gz"
  local top_path="${pprof_dir}/${phase_name}-cpu-top.txt"

  rm -f "${phase_raw_path}" "${phase_summary_path}" "${phase_console_path}" "${profile_path}" "${top_path}"

  echo "Running phase ${phase_name}"
  curl "${curl_flags[@]}" "${PPROF_BASE_URL}/debug/pprof/profile?seconds=${capture_seconds}" -o "${profile_path}" &
  local cpu_pid=$!

  set +e
  STRESS_ACTIVE_PHASE="${phase_name}" \
  STRESS_PHASE_STATE_PATH="${phase_state_path}" \
  STRESS_PHASE_RAW_PATH="${phase_raw_path}" \
  STRESS_PHASE_SUMMARY_PATH="${phase_summary_path}" \
  STRESS_PHASE_TOTAL_ITEMS="${iterations_total}" \
  STRESS_PHASE_WORKERS="${phase_workers}" \
  STRESS_PHASE_START_RPS="${start_rps}" \
  STRESS_PHASE_PEAK_RPS="${peak_rps}" \
  STRESS_PHASE_RAMP_UP_SECONDS="${ramp_up_seconds}" \
  STRESS_PHASE_HOLD_SECONDS="${hold_seconds}" \
  STRESS_PHASE_RAMP_DOWN_SECONDS="${ramp_down_seconds}" \
  STRESS_PHASE_MAX_DURATION="${max_duration}" \
  STRESS_ADMIN_USERNAME="${admin_username}" \
  STRESS_ADMIN_PASSWORD="${admin_password}" \
  BASE_URL="${BASE_URL}" \
  SESSION_COOKIE_NAME="${SESSION_COOKIE_NAME}" \
  OAUTH_CLIENT_ID="${OAUTH_CLIENT_ID}" \
  OAUTH_CLIENT_SECRET="${OAUTH_CLIENT_SECRET}" \
  OAUTH_REDIRECT_URI="${OAUTH_REDIRECT_URI}" \
  STRESS_RUN_ID="${run_id}" \
  STRESS_HTTP_TIMEOUT="${STRESS_HTTP_TIMEOUT}" \
  STRESS_DEVICE_PUBLIC_KEY="${STRESS_DEVICE_PUBLIC_KEY}" \
  STRESS_PRODUCT_PREFIX="${STRESS_PRODUCT_PREFIX}" \
  STRESS_EXPECTED_PRODUCT_COUNT="${STRESS_EXPECTED_PRODUCT_COUNT}" \
  STRESS_USER_COUNT="${STRESS_USER_COUNT}" \
  STRESS_HOMES_PER_USER="${STRESS_HOMES_PER_USER}" \
  STRESS_DEVICES_PER_HOME="${STRESS_DEVICES_PER_HOME}" \
  STRESS_DELETE_DEVICES_PER_USER="${STRESS_DELETE_DEVICES_PER_USER}" \
  STRESS_DELETE_HOMES_PER_USER="${STRESS_DELETE_HOMES_PER_USER}" \
  STRESS_DELETE_HOME_SLOT="${STRESS_DELETE_HOME_SLOT}" \
  STRESS_DELETE_USERS_SELF_COUNT="${STRESS_DELETE_USERS_SELF_COUNT}" \
  STRESS_DELETE_USERS_SELF_START_INDEX="${STRESS_DELETE_USERS_SELF_START_INDEX}" \
  STRESS_DELETE_USERS_ADMIN_COUNT="${STRESS_DELETE_USERS_ADMIN_COUNT}" \
  STRESS_DELETE_USERS_ADMIN_START_INDEX="${STRESS_DELETE_USERS_ADMIN_START_INDEX}" \
  STRESS_FULFILLMENT_REQUESTS_PER_DEVICE="${STRESS_FULFILLMENT_REQUESTS_PER_DEVICE}" \
  STRESS_FULFILLMENT_DEVICE_LIMIT="${STRESS_FULFILLMENT_DEVICE_LIMIT}" \
  ASYNC_HOME_READY_TIMEOUT_MS="${ASYNC_HOME_READY_TIMEOUT_MS}" \
  ASYNC_HOME_READY_POLL_MS="${ASYNC_HOME_READY_POLL_MS}" \
  ASYNC_HOME_EARLY_READY_CHECK_MS="${ASYNC_HOME_EARLY_READY_CHECK_MS}" \
  "${runner_bin}" 2>&1 | tee "${phase_console_path}"
  local phase_status=${PIPESTATUS[0]}
  wait "${cpu_pid}"
  local cpu_status=$?
  set -e

  run_pprof_capture "${top_path}" go tool pprof -top "${profile_path}"
  append_phase_raw "${phase_raw_path}"

  if [[ ${cpu_status} -ne 0 ]]; then
    echo "Warning: failed to capture CPU profile for ${phase_name}" >&2
  fi
  if [[ ${phase_status} -ne 0 ]]; then
    return "${phase_status}"
  fi
  return 0
}

wait_for_postgres
wait_for_tcp "Mosquitto" "mosquitto" "1883"
wait_for_http_any_status "Caddy" "${CADDY_BASE_URL}/"
wait_for_http "Backend" "${BASE_URL}/login"
wait_for_http "pprof" "${PPROF_BASE_URL}/debug/pprof/"

echo "Building Vegeta phase runner"
(
  cd "${repo_root}" &&
  go build -o "${runner_bin}" ./tests/vegeta/cmd/phase_runner
)

echo "Seeding firmware base URLs for stress products"
PGPASSWORD="${DB_PASSWORD}" psql \
  "host=${DB_HOST} port=${DB_PORT} user=${DB_USER} dbname=${DB_NAME} sslmode=${DB_SSLMODE}" \
  -v ON_ERROR_STOP=1 \
  -c "UPDATE products SET firmware_url = '${CADDY_BASE_URL}', firmware_md5_url = '${CADDY_BASE_URL}' WHERE name LIKE '${STRESS_PRODUCT_PREFIX}%';"

echo "Creating admin stress-test user ${admin_username}"
admin_status="$(
  curl "${curl_flags[@]}" \
    -o "${admin_response_file}" \
    -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST "${BASE_URL}/api/enroll/user" \
    -d "{\"username\":\"${admin_username}\",\"password\":\"${admin_password}\"}"
)"

if [[ "${admin_status}" != "201" ]]; then
  echo "Failed to create admin stress-test user (HTTP ${admin_status})" >&2
  python3 - "${admin_response_file}" <<'PY' >&2
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace"))
PY
  exit 1
fi

admin_user_id="$(
  python3 - "${admin_response_file}" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
print(payload["user_id"])
PY
)"

echo "Promoting user ${admin_user_id} to admin in PostgreSQL"
PGPASSWORD="${DB_PASSWORD}" psql \
  "host=${DB_HOST} port=${DB_PORT} user=${DB_USER} dbname=${DB_NAME} sslmode=${DB_SSLMODE}" \
  -v ON_ERROR_STOP=1 \
  -c "INSERT INTO admin_users (user_id) VALUES (${admin_user_id}) ON CONFLICT DO NOTHING;"

initialize_phase_state

export PHASE_NAMES="$(printf '%s\n' "${phase_names[@]}")"
export PHASE_STARTS="$(printf '%s\n' "${phase_start_seconds[@]}")"
export PHASE_ITERATIONS="$(printf '%s\n' "${phase_iterations[@]}")"
export PHASE_WORKERS="$(printf '%s\n' "${phase_workers[@]}")"
export PHASE_START_RPS="$(printf '%s\n' "${phase_start_rps[@]}")"
export PHASE_PEAK_RPS="$(printf '%s\n' "${phase_peak_rps[@]}")"
export PHASE_RAMP_UPS="$(printf '%s\n' "${phase_ramp_up_seconds[@]}")"
export PHASE_HOLDS="$(printf '%s\n' "${phase_hold_seconds[@]}")"
export PHASE_RAMP_DOWNS="$(printf '%s\n' "${phase_ramp_down_seconds[@]}")"
export PHASE_MAX_DURATIONS="$(printf '%s\n' "${phase_max_durations[@]}")"
export PHASE_PROFILE_SECONDS="$(printf '%s\n' "${phase_profile_seconds[@]}")"
export RUN_ID="${run_id}"
export RUN_STARTED_AT="${run_started_at}"
export ADMIN_USERNAME="${admin_username}"

python3 - "${metadata_path}" <<'PY'
import json
import os
import sys

def int_env(name):
    return int(float(os.environ[name]))

def float_env(name):
    return float(os.environ[name])

output_path = sys.argv[1]
phases = []
phase_names = os.environ.get("PHASE_NAMES", "").split("\n") if os.environ.get("PHASE_NAMES") else []
phase_starts = os.environ.get("PHASE_STARTS", "").split("\n") if os.environ.get("PHASE_STARTS") else []
phase_iterations = os.environ.get("PHASE_ITERATIONS", "").split("\n") if os.environ.get("PHASE_ITERATIONS") else []
phase_workers = os.environ.get("PHASE_WORKERS", "").split("\n") if os.environ.get("PHASE_WORKERS") else []
phase_start_rps = os.environ.get("PHASE_START_RPS", "").split("\n") if os.environ.get("PHASE_START_RPS") else []
phase_peak_rps = os.environ.get("PHASE_PEAK_RPS", "").split("\n") if os.environ.get("PHASE_PEAK_RPS") else []
phase_ramp_ups = os.environ.get("PHASE_RAMP_UPS", "").split("\n") if os.environ.get("PHASE_RAMP_UPS") else []
phase_holds = os.environ.get("PHASE_HOLDS", "").split("\n") if os.environ.get("PHASE_HOLDS") else []
phase_ramp_downs = os.environ.get("PHASE_RAMP_DOWNS", "").split("\n") if os.environ.get("PHASE_RAMP_DOWNS") else []
phase_max_durations = os.environ.get("PHASE_MAX_DURATIONS", "").split("\n") if os.environ.get("PHASE_MAX_DURATIONS") else []
phase_profile_seconds = os.environ.get("PHASE_PROFILE_SECONDS", "").split("\n") if os.environ.get("PHASE_PROFILE_SECONDS") else []

for idx, name in enumerate(phase_names):
    if not name:
        continue
    phases.append({
        "name": name,
        "start_offset_seconds": int(phase_starts[idx]),
        "iterations": int(phase_iterations[idx]),
        "workers": int(phase_workers[idx]),
        "start_rps": float(phase_start_rps[idx]),
        "peak_rps": float(phase_peak_rps[idx]),
        "ramp_up_seconds": int(phase_ramp_ups[idx]),
        "hold_seconds": int(phase_holds[idx]),
        "ramp_down_seconds": int(phase_ramp_downs[idx]),
        "max_duration": phase_max_durations[idx],
        "cpu_profile_seconds": int(phase_profile_seconds[idx]),
    })

payload = {
    "run_id": os.environ["RUN_ID"],
    "run_started_at": os.environ["RUN_STARTED_AT"],
    "base_url": os.environ["BASE_URL"],
    "pprof_base_url": os.environ["PPROF_BASE_URL"],
    "caddy_base_url": os.environ["CADDY_BASE_URL"],
    "admin_username": os.environ["ADMIN_USERNAME"],
    "runner_suite": "vegeta",
    "stress_product_prefix": os.environ["STRESS_PRODUCT_PREFIX"],
    "stress_expected_product_count": int_env("STRESS_EXPECTED_PRODUCT_COUNT"),
    "stress_user_count": int_env("STRESS_USER_COUNT"),
    "stress_homes_per_user": int_env("STRESS_HOMES_PER_USER"),
    "stress_devices_per_home": int_env("STRESS_DEVICES_PER_HOME"),
    "stress_delete_devices_per_user": int_env("STRESS_DELETE_DEVICES_PER_USER"),
    "stress_delete_homes_per_user": int_env("STRESS_DELETE_HOMES_PER_USER"),
    "stress_delete_home_slot": int_env("STRESS_DELETE_HOME_SLOT"),
    "stress_delete_users_self_count": int_env("STRESS_DELETE_USERS_SELF_COUNT"),
    "stress_delete_users_self_start_index": int_env("STRESS_DELETE_USERS_SELF_START_INDEX"),
    "stress_delete_users_admin_count": int_env("STRESS_DELETE_USERS_ADMIN_COUNT"),
    "stress_delete_users_admin_start_index": int_env("STRESS_DELETE_USERS_ADMIN_START_INDEX"),
    "stress_fulfillment_requests_per_device": int_env("STRESS_FULFILLMENT_REQUESTS_PER_DEVICE"),
    "stress_fulfillment_device_limit": int_env("STRESS_FULFILLMENT_DEVICE_LIMIT"),
    "stress_phase_buffer_seconds": int_env("STRESS_PHASE_BUFFER_SECONDS"),
    "pprof_phase_seconds": int_env("PPROF_PHASE_SECONDS"),
    "async_home_ready_timeout_ms": int_env("ASYNC_HOME_READY_TIMEOUT_MS"),
    "async_home_ready_poll_ms": int_env("ASYNC_HOME_READY_POLL_MS"),
    "async_home_early_ready_check_ms": int_env("ASYNC_HOME_EARLY_READY_CHECK_MS"),
    "async_drain_wait_seconds": int_env("ASYNC_DRAIN_WAIT_SECONDS"),
    "async_drain_poll_interval_seconds": int_env("ASYNC_DRAIN_POLL_INTERVAL_SECONDS"),
    "phases": phases,
    "k6_product_prefix": os.environ["STRESS_PRODUCT_PREFIX"],
    "k6_expected_product_count": int_env("STRESS_EXPECTED_PRODUCT_COUNT"),
    "k6_user_count": int_env("STRESS_USER_COUNT"),
    "k6_homes_per_user": int_env("STRESS_HOMES_PER_USER"),
    "k6_devices_per_home": int_env("STRESS_DEVICES_PER_HOME"),
    "k6_delete_devices_per_user": int_env("STRESS_DELETE_DEVICES_PER_USER"),
    "k6_delete_homes_per_user": int_env("STRESS_DELETE_HOMES_PER_USER"),
    "k6_delete_home_slot": int_env("STRESS_DELETE_HOME_SLOT"),
    "k6_delete_users_self_count": int_env("STRESS_DELETE_USERS_SELF_COUNT"),
    "k6_delete_users_self_start_index": int_env("STRESS_DELETE_USERS_SELF_START_INDEX"),
    "k6_delete_users_admin_count": int_env("STRESS_DELETE_USERS_ADMIN_COUNT"),
    "k6_delete_users_admin_start_index": int_env("STRESS_DELETE_USERS_ADMIN_START_INDEX"),
    "k6_fulfillment_requests_per_device": int_env("STRESS_FULFILLMENT_REQUESTS_PER_DEVICE"),
    "k6_fulfillment_device_limit": int_env("STRESS_FULFILLMENT_DEVICE_LIMIT"),
}

with open(output_path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2)
PY

echo -n >"${k6_raw_path}"

vegeta_status=0
last_phase_index=$(( ${#phase_names[@]} - 1 ))
for idx in "${!phase_names[@]}"; do
  phase_name="${phase_names[$idx]}"
  set +e
  run_phase \
    "${phase_name}" \
    "${phase_iterations[$idx]}" \
    "${phase_workers[$idx]}" \
    "${phase_start_rps[$idx]}" \
    "${phase_peak_rps[$idx]}" \
    "${phase_ramp_up_seconds[$idx]}" \
    "${phase_hold_seconds[$idx]}" \
    "${phase_ramp_down_seconds[$idx]}" \
    "${phase_max_durations[$idx]}" \
    "${phase_profile_seconds[$idx]}"
  phase_status=$?
  set -e
  if (( phase_status != 0 )); then
    vegeta_status=$phase_status
    break
  fi
  if (( STRESS_PHASE_BUFFER_SECONDS > 0 && idx < last_phase_index )); then
    echo "Waiting ${STRESS_PHASE_BUFFER_SECONDS}s before next phase"
    sleep "${STRESS_PHASE_BUFFER_SECONDS}"
  fi
done

set +e
curl "${curl_flags[@]}" "${PPROF_BASE_URL}/debug/pprof/heap" -o "${heap_profile_path}"
heap_status=$?
curl "${curl_flags[@]}" "${PPROF_BASE_URL}/debug/pprof/goroutine?debug=1" -o "${goroutine_path}"
goroutine_status=$?
set -e

if [[ ${heap_status} -eq 0 ]]; then
  run_pprof_capture "${heap_top_path}" go tool pprof -sample_index=alloc_space -top "${heap_profile_path}"
  run_pprof_capture "${heap_inuse_top_path}" go tool pprof -sample_index=inuse_space -top "${heap_profile_path}"
else
  printf 'Heap profile capture failed.\n' >"${heap_top_path}"
  printf 'Heap in-use profile capture failed.\n' >"${heap_inuse_top_path}"
fi

if [[ ${goroutine_status} -ne 0 ]]; then
  printf 'Goroutine dump capture failed.\n' >"${goroutine_path}"
fi

echo "Waiting up to ${ASYNC_DRAIN_WAIT_SECONDS}s for async home jobs to drain"
set +e
RUN_STARTED_AT="${run_started_at}" collect_async_jobs_audit "${async_jobs_path}"
async_status=$?
set -e

python3 "${repo_root}/tests/vegeta/phase_render_report.py" \
  --summary "${k6_summary_path}" \
  --raw "${k6_raw_path}" \
  --report "${report_path}" \
  --metadata "${metadata_path}" \
  --async-jobs "${async_jobs_path}" \
  --phase-state "${phase_state_path}" \
  --pprof-dir "${pprof_dir}" \
  --goroutine "${goroutine_path}" \
  --heap-top "${heap_top_path}" \
  --heap-inuse-top "${heap_inuse_top_path}"

echo
echo "Stress dashboard: ${report_path}"
echo "Raw summary: ${k6_summary_path}"
echo "Raw metrics: ${k6_raw_path}"
echo "Async audit: ${async_jobs_path}"
echo "Phase CPU profiles: ${pprof_dir}"

if [[ ${heap_status} -ne 0 || ${goroutine_status} -ne 0 ]]; then
  echo "Warning: one or more post-run pprof captures failed." >&2
fi
if [[ ${async_status} -ne 0 ]]; then
  echo "Warning: async queue drain verification failed. See ${async_jobs_path}." >&2
fi
if [[ ${vegeta_status} -ne 0 || ${async_status} -ne 0 ]]; then
  exit 1
fi
