#!/usr/bin/env bash
set -euo pipefail
if [[ "${GITHUB_ACTIONS:-}" != "true" || "${RUNNER_ENVIRONMENT:-}" != "github-hosted" || "${ANDROID_SERIAL:-}" != "emulator-5556" ]]; then
  echo 'Android verification requires the explicit cloud emulator.' >&2
  exit 2
fi
if [[ $(adb -s emulator-5556 emu avd name | tr -d '\r' | head -1) != qimeng_api35 ]]; then
  echo 'The connected AVD does not match the reference capture.' >&2
  exit 2
fi
adb -s emulator-5556 shell wm size 320x640
adb -s emulator-5556 shell wm density 160
adb -s emulator-5556 shell wm user-rotation lock 0
adb -s emulator-5556 shell settings put system font_scale 1.0
adb -s emulator-5556 shell settings put secure immersive_mode_confirmations confirmed
adb -s emulator-5556 shell cmd uimode night no
export QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-android"
independent_target=$(mktemp -d /dev/shm/qimeng-android-proof-XXXXXXXX)
export QIMENG_BACKUP_TARGETS
QIMENG_BACKUP_TARGETS=$(jq -cn --arg path "$independent_target" '{proof:{path:$path,failureDomain:"ephemeral-cloud-volume"}}')
export QIMENG_DEFAULT_BACKUP_TARGET=proof
export QIMENG_LISTEN='127.0.0.1:18481'
export QIMENG_TIMEZONE='Asia/Shanghai'
export QIMENG_ADMIN_TOKEN='synthetic-cloud-only-owner-credential-18481'
chmod +x "$RUNNER_TEMP/server-artifact/build/qimeng-next"
"$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-android.log" 2>&1 &
service_pid=$!
source_token='synthetic-cloud-only-owner-credential-18482'
QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-source" QIMENG_LISTEN='127.0.0.1:18482' \
  QIMENG_ADMIN_TOKEN="$source_token" QIMENG_BACKUP_TARGETS='{}' QIMENG_DEFAULT_BACKUP_TARGET='' \
  "$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-source.log" 2>&1 &
source_pid=$!
finish() {
  result=$?
  trap - EXIT
  mkdir -p android/build/cloud-screenshots
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next.ui.test/files android/build/cloud-screenshots/core-ui > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next/files/screenshots android/build/cloud-screenshots/app > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Pictures/QimengNextCloudTests android/build/cloud-screenshots/retained > /dev/null 2>&1 || true
  kill "$service_pid" "$source_pid" 2>/dev/null || true
  wait "$service_pid" 2>/dev/null || true
  wait "$source_pid" 2>/dev/null || true
  exit "$result"
}
trap finish EXIT
mkdir -p android/build/cloud-screenshots
transport_log=android/build/cloud-screenshots/transport-health.txt
transport_status() {
  echo "$*" | tee -a "$transport_log"
}
prepare_transport() {
  local phase=$1 port pid status prior exit_code device_status
  if ! timeout 30 adb -s emulator-5556 wait-for-device ||
     [[ $(adb -s emulator-5556 emu avd name | tr -d '\r' | head -1) != qimeng_api35 ]]; then
    transport_status "$phase: designated AVD unavailable"
    return 1
  fi
  local mappings
  mappings=$(adb -s emulator-5556 reverse --list)
  for port in 18481 18482; do
    if [[ $port == 18481 ]]; then pid=$service_pid; else pid=$source_pid; fi
    if ! kill -0 "$pid" 2>/dev/null; then
      exit_code=0
      wait "$pid" || exit_code=$?
      transport_status "$phase: port=$port host process exited with status $exit_code"
      return 1
    fi
    status=$(curl --silent --output /dev/null --max-time 4 --write-out '%{http_code}' "http://127.0.0.1:$port/healthz") || status=000
    prior=absent
    if awk -v p="tcp:$port" '$2 == p && $3 == p { found=1 } END { exit !found }' <<< "$mappings"; then prior=present; fi
    transport_status "$phase: port=$port host process alive, host HTTP=$status, preceding reverse=$prior"
    [[ $status == 200 ]] || return 1
    if ! adb -s emulator-5556 reverse "tcp:$port" "tcp:$port" >/dev/null; then
      transport_status "$phase: port=$port could not register ADB reverse"
      return 1
    fi
    # Keep stdin open while the response crosses ADB. Only the production client verifies its authenticated route.
    device_status=$({ printf 'GET /healthz HTTP/1.1\r\nHost: 127.0.0.1:%s\r\nConnection: close\r\n\r\n' "$port"; sleep 1; } |
      timeout 8 adb -s emulator-5556 shell -T toybox nc -w 3 -W 3 127.0.0.1 "$port" |
      awk 'NR == 1 && $1 ~ /^HTTP\// && $2 ~ /^[0-9][0-9][0-9]$/ { print $2 }') || device_status=''
    transport_status "$phase: port=$port device HTTP=${device_status:-unobserved}"
  done
}
ready=false
for attempt in $(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:18481/healthz > /dev/null &&
     curl --fail --silent http://127.0.0.1:18482/healthz > /dev/null; then ready=true; break; fi
  if ! kill -0 "$service_pid" 2>/dev/null; then transport_status 'startup: target host process exited'; exit 1; fi
  if ! kill -0 "$source_pid" 2>/dev/null; then transport_status 'startup: source host process exited'; exit 1; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo 'Isolated Android server did not become ready'; exit 1; fi
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=320x180:rate=15' -t 12 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test.mp4"
prepare_transport core
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test.mp4" /data/local/tmp/qimeng-next-test.mp4
checks=0
gradle --no-daemon --continue -p android -Pqimeng.embedded=false :core:ui:connectedDebugAndroidTest :core:data:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceUrl=http://127.0.0.1:18482/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceToken="$source_token" \
  -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" --stacktrace || checks=1
prepare_transport app
gradle --no-daemon -p android -Pqimeng.embedded=false :app:connectedClientDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.AndroidRealServerTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" --stacktrace || checks=1
if (( checks != 0 )); then prepare_transport after-failure || true; fi
exit "$checks"
