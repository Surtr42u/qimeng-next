#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" ]]; then echo 'MCP integration is cloud-only.' >&2; exit 2; fi
export QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-mcp"
independent_target=$(mktemp -d /dev/shm/qimeng-cloud-mcp-XXXXXXXX)
export QIMENG_BACKUP_TARGETS
QIMENG_BACKUP_TARGETS=$(jq -cn --arg path "$independent_target" '{proof:{path:$path,failureDomain:"temporary-cloud-volume"}}')
export QIMENG_DEFAULT_BACKUP_TARGET=proof
export QIMENG_LISTEN='127.0.0.1:18481'
export QIMENG_TIMEZONE='Asia/Shanghai'
export QIMENG_AI_CONFIGURED='true'
export QIMENG_ADMIN_TOKEN='synthetic-cloud-only-owner-credential-18481'
export QIMENG_AI_TOKEN='synthetic-cloud-only-readonly-credential-18481'
export QIMENG_E2E_TOKEN="$QIMENG_ADMIN_TOKEN"
export QIMENG_TEST_BASE='http://127.0.0.1:18481'
export PYTHONPATH="$GITHUB_WORKSPACE/ai/src"
chmod +x "$RUNNER_TEMP/server-artifact/build/qimeng-next"
"$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-mcp.log" 2>&1 &
service_pid=$!
trap 'kill "$service_pid" 2>/dev/null || true; wait "$service_pid" 2>/dev/null || true' EXIT
ready=false
for attempt in $(seq 1 30); do
  if curl --fail --silent "$QIMENG_TEST_BASE/healthz" > /dev/null; then ready=true; break; fi
  if ! kill -0 "$service_pid" 2>/dev/null; then cat "$RUNNER_TEMP/qimeng-next-mcp.log"; exit 1; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo 'Isolated MCP server did not become ready'; exit 1; fi
python -m unittest discover -s ai/tests -p test_live.py -v
npm --prefix tools/mcp-compat test
