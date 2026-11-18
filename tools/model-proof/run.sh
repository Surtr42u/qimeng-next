#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
test "$(ls -1 /sys/class/net)" = lo
engine=$(find "$MODEL_WORK/engine" -type f -name llama-server -print -quit)
test -x "$engine"
export LD_LIBRARY_PATH="$(dirname "$engine")"
"$engine" --model "$MODEL_WORK/Qwen3-4B-Q4_K_M.gguf" --alias qimeng-cpu-fixture --host 127.0.0.1 --port 18580 \
  --ctx-size 32768 --parallel 1 --threads 4 --threads-batch 4 --n-gpu-layers 0 --jinja --reasoning off --no-webui \
  > "$MODEL_WORK/llama.log" 2>&1 &
engine_pid=$!
trap 'kill "$engine_pid" 2>/dev/null || true; wait "$engine_pid" 2>/dev/null || true' EXIT
for _ in $(seq 1 90); do
  if curl --silent --fail http://127.0.0.1:18580/health >/dev/null; then break; fi
  if ! kill -0 "$engine_pid" 2>/dev/null; then tail -30 "$MODEL_WORK/llama.log"; exit 1; fi
  sleep 1
done
curl --silent --fail http://127.0.0.1:18580/health >/dev/null
node tools/mcp-compat/model-proof.mjs
