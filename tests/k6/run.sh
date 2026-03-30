#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

BASE_URL="${BASE_URL:-http://backend:8080}"
PPROF_BASE_URL="${PPROF_BASE_URL:-http://backend:6060}"
CADDY_BASE_URL="${CADDY_BASE_URL:-http://caddy}"
PPROF_CPU_SECONDS="${PPROF_CPU_SECONDS:-30}"
WAIT_TIMEOUT_SECONDS="${WAIT_TIMEOUT_SECONDS:-180}"
SESSION_COOKIE_NAME="${SESSION_COOKIE_NAME:-user_session}"
OAUTH_CLIENT_ID="${OAUTH_CLIENT_ID:-google-client}"
OAUTH_REDIRECT_URI="${OAUTH_REDIRECT_URI:-http://127.0.0.1/oauth/callback}"
K6_PRODUCT_ID="${K6_PRODUCT_ID:-1}"
K6_PRODUCT_NAME="${K6_PRODUCT_NAME:-ml-smart-sensor-v1}"
K6_USER_FLOW_VUS="${K6_USER_FLOW_VUS:-${K6_VUS:-100}}"
K6_USER_FLOW_ITERATIONS="${K6_USER_FLOW_ITERATIONS:-100}"
K6_USER_FLOW_MAX_DURATION="${K6_USER_FLOW_MAX_DURATION:-20m}"
K6_HOME_BURST_VUS="${K6_HOME_BURST_VUS:-100}"
K6_HOME_BURST_START="${K6_HOME_BURST_START:-10s}"
K6_HOME_BURST_ITERATIONS="${K6_HOME_BURST_ITERATIONS:-1}"
K6_HOME_BURST_MAX_DURATION="${K6_HOME_BURST_MAX_DURATION:-20m}"
K6_FULFILLMENT_PARALLEL_VUS="${K6_FULFILLMENT_PARALLEL_VUS:-100}"
K6_FULFILLMENT_START="${K6_FULFILLMENT_START:-10s}"
K6_FULFILLMENT_PARALLEL_ITERATIONS="${K6_FULFILLMENT_PARALLEL_ITERATIONS:-1000}"
K6_FULFILLMENT_MAX_DURATION="${K6_FULFILLMENT_MAX_DURATION:-10m}"
K6_DEVICE_STREAM_VUS="${K6_DEVICE_STREAM_VUS:-10}"
K6_DEVICE_STREAM_ITERATIONS="${K6_DEVICE_STREAM_ITERATIONS:-20}"
K6_DEVICE_STREAM_START="${K6_DEVICE_STREAM_START:-10s}"
K6_DEVICE_STREAM_DELETE_START="${K6_DEVICE_STREAM_DELETE_START:-30s}"
K6_DEVICE_STREAM_INTERVAL_MS="${K6_DEVICE_STREAM_INTERVAL_MS:-1000}"
K6_DEVICE_STREAM_MAX_DURATION="${K6_DEVICE_STREAM_MAX_DURATION:-10m}"
ASYNC_HOME_READY_TIMEOUT_MS="${ASYNC_HOME_READY_TIMEOUT_MS:-90000}"
ASYNC_HOME_READY_POLL_MS="${ASYNC_HOME_READY_POLL_MS:-2000}"
ASYNC_HOME_EARLY_READY_CHECK_MS="${ASYNC_HOME_EARLY_READY_CHECK_MS:-9000}"
ASYNC_DRAIN_WAIT_SECONDS="${ASYNC_DRAIN_WAIT_SECONDS:-120}"
ASYNC_DRAIN_POLL_INTERVAL_SECONDS="${ASYNC_DRAIN_POLL_INTERVAL_SECONDS:-5}"
DB_HOST="${DB_HOST:-${DATABASE_HOST:-localhost}}"
DB_PORT="${DB_PORT:-${DATABASE_PORT:-5432}}"
DB_USER="${DB_USER:-${DATABASE_USER:-}}"
DB_PASSWORD="${DB_PASSWORD:-${DATABASE_PASSWORD:-}}"
DB_NAME="${DB_NAME:-${DATABASE_NAME:-}}"
DB_SSLMODE="${DB_SSLMODE:-disable}"
K6_DEVICE_PUBLIC_KEY="${K6_DEVICE_PUBLIC_KEY:-AQbnaqQshSiDwqVRxeH8lTij1x49dJjzhQqAwtbW4EI=}"

: "${OAUTH_CLIENT_SECRET:?set OAUTH_CLIENT_SECRET}"
: "${DB_USER:?set DB_USER or DATABASE_USER}"
: "${DB_PASSWORD:?set DB_PASSWORD or DATABASE_PASSWORD}"
: "${DB_NAME:?set DB_NAME or DATABASE_NAME}"

artifacts_dir="${script_dir}/artifacts"
pprof_dir="${script_dir}/pprof"
report_path="${script_dir}/report.html"
k6_summary_path="./artifacts/k6-summary.json"
k6_summary_abs="${artifacts_dir}/k6-summary.json"
k6_raw_path="${artifacts_dir}/k6-raw.ndjson"
metadata_path="${artifacts_dir}/run-metadata.json"
async_jobs_path="${artifacts_dir}/async-jobs.json"

cpu_profile_path="${pprof_dir}/cpu.pb.gz"
heap_profile_path="${pprof_dir}/heap.pb.gz"
goroutine_path="${pprof_dir}/goroutine.txt"
cpu_top_path="${pprof_dir}/cpu-top.txt"
cpu_cum_path="${pprof_dir}/cpu-top-cum.txt"
heap_top_path="${pprof_dir}/heap-top.txt"
heap_inuse_top_path="${pprof_dir}/heap-inuse-top.txt"
cpu_svg_path="${pprof_dir}/cpu.svg"
heap_svg_path="${pprof_dir}/heap.svg"

mkdir -p "${artifacts_dir}" "${pprof_dir}"
rm -f \
  "${report_path}" \
  "${k6_summary_abs}" \
  "${k6_raw_path}" \
  "${metadata_path}" \
  "${async_jobs_path}" \
  "${cpu_profile_path}" \
  "${heap_profile_path}" \
  "${goroutine_path}" \
  "${cpu_top_path}" \
  "${cpu_cum_path}" \
  "${heap_top_path}" \
  "${heap_inuse_top_path}" \
  "${cpu_svg_path}" \
  "${heap_svg_path}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

run_id="$(date +%s)_$RANDOM"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
admin_username="k6_admin_${run_id}"
admin_password="StressAdminPass123!"
admin_response_file="$(mktemp)"

cleanup() {
  rm -f "${admin_response_file}"
}
trap cleanup EXIT

require_cmd curl
require_cmd k6
require_cmd psql
require_cmd python3
require_cmd go
require_cmd dot

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
      cat "${output_path}"
    } >"${output_path}.tmp"
    mv "${output_path}.tmp" "${output_path}"
  fi
}

run_pprof_svg() {
  local output_path="$1"
  shift

  if ! "$@" >"${output_path}" 2>"${output_path}.err"; then
    cat "${output_path}.err" >"${output_path}"
  fi
  rm -f "${output_path}.err"
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


def run_query() -> dict:
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


def write_snapshot(snapshot: dict, timed_out: bool) -> None:
    snapshot["captured_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    snapshot["drain_wait_seconds"] = timeout_seconds
    snapshot["drain_poll_interval_seconds"] = poll_interval_seconds
    snapshot["timed_out"] = timed_out
    snapshot["scope"] = {
        "run_started_at": run_started_at,
        "jobs_created_since_run_start": True,
        "homes_created_since_run_start": True,
    }
    output_path.write_text(json.dumps(snapshot, indent=2), encoding="utf-8")


deadline = time.time() + timeout_seconds

try:
    while True:
        snapshot = run_query()
        write_snapshot(snapshot, timed_out=False)

        if snapshot.get("all_async_jobs_completed"):
            sys.exit(0)

        if time.time() >= deadline:
            write_snapshot(snapshot, timed_out=True)
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
        timed_out=True,
    )
    sys.exit(1)
PY
}

wait_for_postgres
wait_for_tcp "Mosquitto" "mosquitto" "1883"
wait_for_http_any_status "Caddy" "${CADDY_BASE_URL}/"
wait_for_http "Backend" "${BASE_URL}/login"
wait_for_http "pprof" "${PPROF_BASE_URL}/debug/pprof/"

echo "Seeding product firmware base URLs for ${K6_PRODUCT_NAME}"
PGPASSWORD="${DB_PASSWORD}" psql \
  "host=${DB_HOST} port=${DB_PORT} user=${DB_USER} dbname=${DB_NAME} sslmode=${DB_SSLMODE}" \
  -v ON_ERROR_STOP=1 \
  -c "UPDATE products SET firmware_url = '${CADDY_BASE_URL}', firmware_md5_url = '${CADDY_BASE_URL}' WHERE name = '${K6_PRODUCT_NAME}';"

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
  cat "${admin_response_file}" >&2
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

cat >"${metadata_path}" <<EOF
{
  "run_id": "${run_id}",
  "run_started_at": "${run_started_at}",
  "base_url": "${BASE_URL}",
  "pprof_base_url": "${PPROF_BASE_URL}",
  "caddy_base_url": "${CADDY_BASE_URL}",
  "admin_username": "${admin_username}",
  "k6_product_name": "${K6_PRODUCT_NAME}",
  "k6_product_id": ${K6_PRODUCT_ID},
  "k6_user_flow_vus": "${K6_USER_FLOW_VUS}",
  "k6_user_flow_iterations": "${K6_USER_FLOW_ITERATIONS}",
  "k6_user_flow_max_duration": "${K6_USER_FLOW_MAX_DURATION}",
  "k6_home_burst_vus": "${K6_HOME_BURST_VUS}",
  "k6_home_burst_start": "${K6_HOME_BURST_START}",
  "k6_home_burst_iterations": "${K6_HOME_BURST_ITERATIONS}",
  "k6_home_burst_max_duration": "${K6_HOME_BURST_MAX_DURATION}",
  "k6_fulfillment_parallel_vus": "${K6_FULFILLMENT_PARALLEL_VUS}",
  "k6_fulfillment_start": "${K6_FULFILLMENT_START}",
  "k6_fulfillment_parallel_iterations": "${K6_FULFILLMENT_PARALLEL_ITERATIONS}",
  "k6_fulfillment_max_duration": "${K6_FULFILLMENT_MAX_DURATION}",
  "k6_device_stream_vus": "${K6_DEVICE_STREAM_VUS}",
  "k6_device_stream_iterations": "${K6_DEVICE_STREAM_ITERATIONS}",
  "k6_device_stream_start": "${K6_DEVICE_STREAM_START}",
  "k6_device_stream_delete_start": "${K6_DEVICE_STREAM_DELETE_START}",
  "k6_device_stream_interval_ms": "${K6_DEVICE_STREAM_INTERVAL_MS}",
  "k6_device_stream_max_duration": "${K6_DEVICE_STREAM_MAX_DURATION}",
  "pprof_cpu_seconds": ${PPROF_CPU_SECONDS},
  "async_home_ready_timeout_ms": ${ASYNC_HOME_READY_TIMEOUT_MS},
  "async_home_ready_poll_ms": ${ASYNC_HOME_READY_POLL_MS},
  "async_home_early_ready_check_ms": ${ASYNC_HOME_EARLY_READY_CHECK_MS},
  "async_drain_wait_seconds": ${ASYNC_DRAIN_WAIT_SECONDS},
  "async_drain_poll_interval_seconds": ${ASYNC_DRAIN_POLL_INTERVAL_SECONDS}
}
EOF

echo "Starting CPU profile capture from ${PPROF_BASE_URL} for ${PPROF_CPU_SECONDS}s"
curl "${curl_flags[@]}" \
  "${PPROF_BASE_URL}/debug/pprof/profile?seconds=${PPROF_CPU_SECONDS}" \
  -o "${cpu_profile_path}" &
pprof_cpu_pid=$!

cd "${script_dir}"

set +e
K6_ADMIN_USERNAME="${admin_username}" \
K6_ADMIN_PASSWORD="${admin_password}" \
K6_SUMMARY_PATH="${k6_summary_path}" \
BASE_URL="${BASE_URL}" \
SESSION_COOKIE_NAME="${SESSION_COOKIE_NAME}" \
OAUTH_CLIENT_ID="${OAUTH_CLIENT_ID}" \
OAUTH_CLIENT_SECRET="${OAUTH_CLIENT_SECRET}" \
OAUTH_REDIRECT_URI="${OAUTH_REDIRECT_URI}" \
K6_PRODUCT_ID="${K6_PRODUCT_ID}" \
K6_PRODUCT_NAME="${K6_PRODUCT_NAME}" \
K6_RUN_ID="${run_id}" \
K6_DEVICE_PUBLIC_KEY="${K6_DEVICE_PUBLIC_KEY}" \
K6_USER_FLOW_VUS="${K6_USER_FLOW_VUS}" \
K6_USER_FLOW_ITERATIONS="${K6_USER_FLOW_ITERATIONS}" \
K6_USER_FLOW_MAX_DURATION="${K6_USER_FLOW_MAX_DURATION}" \
K6_HOME_BURST_VUS="${K6_HOME_BURST_VUS}" \
K6_HOME_BURST_START="${K6_HOME_BURST_START}" \
K6_HOME_BURST_ITERATIONS="${K6_HOME_BURST_ITERATIONS}" \
K6_HOME_BURST_MAX_DURATION="${K6_HOME_BURST_MAX_DURATION}" \
K6_FULFILLMENT_PARALLEL_VUS="${K6_FULFILLMENT_PARALLEL_VUS}" \
K6_FULFILLMENT_START="${K6_FULFILLMENT_START}" \
K6_FULFILLMENT_PARALLEL_ITERATIONS="${K6_FULFILLMENT_PARALLEL_ITERATIONS}" \
K6_FULFILLMENT_MAX_DURATION="${K6_FULFILLMENT_MAX_DURATION}" \
K6_DEVICE_STREAM_VUS="${K6_DEVICE_STREAM_VUS}" \
K6_DEVICE_STREAM_ITERATIONS="${K6_DEVICE_STREAM_ITERATIONS}" \
K6_DEVICE_STREAM_START="${K6_DEVICE_STREAM_START}" \
K6_DEVICE_STREAM_DELETE_START="${K6_DEVICE_STREAM_DELETE_START}" \
K6_DEVICE_STREAM_INTERVAL_MS="${K6_DEVICE_STREAM_INTERVAL_MS}" \
K6_DEVICE_STREAM_MAX_DURATION="${K6_DEVICE_STREAM_MAX_DURATION}" \
ASYNC_HOME_READY_TIMEOUT_MS="${ASYNC_HOME_READY_TIMEOUT_MS}" \
ASYNC_HOME_READY_POLL_MS="${ASYNC_HOME_READY_POLL_MS}" \
ASYNC_HOME_EARLY_READY_CHECK_MS="${ASYNC_HOME_EARLY_READY_CHECK_MS}" \
env -u K6_VUS -u K6_DURATION -u K6_RAMP_UP -u K6_RAMP_DOWN \
  k6 run --out "json=${k6_raw_path}" ./stress.js
k6_status=$?

wait "${pprof_cpu_pid}"
cpu_status=$?

curl "${curl_flags[@]}" "${PPROF_BASE_URL}/debug/pprof/heap" -o "${heap_profile_path}"
heap_status=$?

curl "${curl_flags[@]}" "${PPROF_BASE_URL}/debug/pprof/goroutine?debug=1" -o "${goroutine_path}"
goroutine_status=$?
set -e

run_pprof_capture "${cpu_top_path}" go tool pprof -top "${cpu_profile_path}"
run_pprof_capture "${cpu_cum_path}" go tool pprof -top -cum "${cpu_profile_path}"
run_pprof_capture "${heap_top_path}" go tool pprof -sample_index=alloc_space -top "${heap_profile_path}"
run_pprof_capture "${heap_inuse_top_path}" go tool pprof -sample_index=inuse_space -top "${heap_profile_path}"
run_pprof_svg "${cpu_svg_path}" go tool pprof -svg "${cpu_profile_path}"
run_pprof_svg "${heap_svg_path}" go tool pprof -sample_index=alloc_space -svg "${heap_profile_path}"

echo "Waiting up to ${ASYNC_DRAIN_WAIT_SECONDS}s for async home jobs to drain"
set +e
RUN_STARTED_AT="${run_started_at}" collect_async_jobs_audit "${async_jobs_path}"
async_status=$?
set -e

python3 "${script_dir}/render_report.py" \
  --summary "${k6_summary_abs}" \
  --raw "${k6_raw_path}" \
  --report "${report_path}" \
  --metadata "${metadata_path}" \
  --async-jobs "${async_jobs_path}" \
  --cpu-top "${cpu_top_path}" \
  --cpu-cum "${cpu_cum_path}" \
  --heap-top "${heap_top_path}" \
  --heap-inuse-top "${heap_inuse_top_path}" \
  --cpu-svg "${cpu_svg_path}" \
  --heap-svg "${heap_svg_path}" \
  --goroutine "${goroutine_path}"

echo
echo "Stress dashboard: ${report_path}"
echo "Raw k6 summary: ${k6_summary_abs}"
echo "Raw k6 metrics: ${k6_raw_path}"
echo "Async audit: ${async_jobs_path}"
echo "pprof CPU profile: ${cpu_profile_path}"
echo "pprof heap profile: ${heap_profile_path}"
echo "pprof goroutines: ${goroutine_path}"

if [[ ${cpu_status} -ne 0 || ${heap_status} -ne 0 || ${goroutine_status} -ne 0 ]]; then
  echo "Warning: one or more pprof captures failed. Verify ${PPROF_BASE_URL} is reachable." >&2
fi

if [[ ${async_status} -ne 0 ]]; then
  echo "Warning: async queue drain verification failed. See ${async_jobs_path}." >&2
fi

if [[ ${k6_status} -ne 0 || ${async_status} -ne 0 ]]; then
  exit 1
fi

exit 0
