#!/usr/bin/env bash
# RiceGuard deterministic smoke test.
#
# Builds the server, starts it on a local port with a fresh SQLite database,
# probes its health endpoint, creates a real inspection task through the public
# JSON API, and then tears everything down. The script never touches the
# network, never calls `go test`, and asserts responses by capturing curl
# output into variables (never piping curl into grep, which can close the pipe
# early and produce SIGPIPE).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

PORT="${RICE_SMOKE_PORT:-18080}"
WORK="$(mktemp -d)"
DB="$WORK/smoke.db"
BIN="$WORK/riceguard"
PID=""

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "== building server =="
go build -o "$BIN" ./cmd/riceguard

echo "== starting server on :$PORT =="
RICE_ADDR=":$PORT" RICE_DB="$DB" RICE_STATIC_DIR="$HERE/frontend/dist" "$BIN" &
PID=$!

# Wait for the health endpoint to become ready (bounded, deterministic).
ready=""
for _ in $(seq 1 100); do
  if resp="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" 2>/dev/null || true)"; then
    if [ "$resp" = "200" ]; then ready="1"; break; fi
  fi
  sleep 0.05
done
if [ -z "$ready" ]; then
  echo "server did not become ready" >&2
  exit 1
fi

echo "== probing health =="
health="$(curl -s "http://127.0.0.1:$PORT/api/health")"
case "$health" in
  *'"status":"ok"'*) echo "health ok" ;;
  *) echo "unexpected health response: $health" >&2; exit 1 ;;
esac

echo "== creating task =="
create_payload='{
  "operation_id":"smoke-1",
  "seed_lot":"lot-smoke",
  "field":"field-01",
  "variety":"xiangliangyou-900",
  "female_cert_revision":3,
  "male_cert_revision":3,
  "blind_allocations":[{"code":"b1","germination":100,"pathogen":50,"moisture":30}],
  "chamber":"ch-smoke",
  "chamber_start":100,
  "chamber_end":200,
  "plate":"p-smoke",
  "wells":["w1"],
  "reviewer_roster":["reviewer-f","reviewer-g"]
}'
create_resp="$(curl -s -X POST "http://127.0.0.1:$PORT/api/tasks" \
  -H 'Content-Type: application/json' -d "$create_payload")"

task_id="$(printf '%s' "$create_resp" | sed -n 's/.*"task_id":"\([^"]*\)".*/\1/p')"
if [ -z "$task_id" ]; then
  echo "task creation failed: $create_resp" >&2
  exit 1
fi
echo "created task $task_id"

echo "== reading task aggregate =="
view="$(curl -s "http://127.0.0.1:$PORT/api/tasks/$task_id")"
case "$view" in
  *'"status":"pending_sampling"'*) echo "task view ok" ;;
  *) echo "unexpected task view: $view" >&2; exit 1 ;;
esac

echo "== reading summary =="
summary="$(curl -s "http://127.0.0.1:$PORT/api/tasks/$task_id/summary")"
case "$summary" in
  *'"status":"pending_sampling"'*) echo "summary ok" ;;
  *) echo "unexpected summary: $summary" >&2; exit 1 ;;
esac

echo "== restart recovery (reopen same DB) =="
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
PID=""
RICE_ADDR=":$PORT" RICE_DB="$DB" RICE_STATIC_DIR="$HERE/frontend/dist" "$BIN" &
PID=$!
ready=""
for _ in $(seq 1 100); do
  if resp="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" 2>/dev/null || true)"; then
    if [ "$resp" = "200" ]; then ready="1"; break; fi
  fi
  sleep 0.05
done
if [ -z "$ready" ]; then
  echo "server did not recover" >&2
  exit 1
fi
recovered="$(curl -s "http://127.0.0.1:$PORT/api/tasks/$task_id")"
case "$recovered" in
  *'"ID":"'"$task_id"'"'*) echo "recovery ok" ;;
  *) echo "task not recovered: $recovered" >&2; exit 1 ;;
esac

echo "== smoke test passed =="
