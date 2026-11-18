#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" ]]; then
  echo 'Live integration is cloud-only.' >&2
  exit 2
fi
export QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-live"
export QIMENG_E2E_LIBRARY_ROOT="$RUNNER_TEMP/qimeng-next-live-media"
mkdir -p "$QIMENG_E2E_LIBRARY_ROOT"
export QIMENG_STORAGE_ROOTS
QIMENG_STORAGE_ROOTS=$(jq -cn --arg root "$QIMENG_E2E_LIBRARY_ROOT" '{media:$root}')
export QIMENG_LISTEN='127.0.0.1:18481'
export QIMENG_WEB_DIR="$GITHUB_WORKSPACE/web/dist"
export QIMENG_TIMEZONE='Asia/Shanghai'
export QIMENG_AI_CONFIGURED='true'
export QIMENG_ADMIN_TOKEN='synthetic-cloud-only-owner-credential-18481'
export QIMENG_E2E_TOKEN="$QIMENG_ADMIN_TOKEN"
export QIMENG_TEST_BASE='http://127.0.0.1:18481'
export QIMENG_E2E_BASE_URL="$QIMENG_TEST_BASE"
export PYTHONPATH="$GITHUB_WORKSPACE/ai/src"
chmod +x "$RUNNER_TEMP/server-artifact/build/qimeng-next"
"$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-live.log" 2>&1 &
service_pid=$!
trap 'kill "$service_pid" 2>/dev/null || true; wait "$service_pid" 2>/dev/null || true' EXIT
ready=false
for attempt in $(seq 1 30); do
  if curl --fail --silent "$QIMENG_TEST_BASE/healthz" > /dev/null; then ready=true; break; fi
  if ! kill -0 "$service_pid" 2>/dev/null; then cat "$RUNNER_TEMP/qimeng-next-live.log"; exit 1; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo 'Isolated server did not become ready'; exit 1; fi
npm --prefix web run test:live
