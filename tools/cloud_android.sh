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
QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-statistics" QIMENG_LISTEN='127.0.0.1:18483' \
  QIMENG_BACKUP_TARGETS='{}' QIMENG_DEFAULT_BACKUP_TARGET='' \
  "$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-statistics.log" 2>&1 &
statistics_pid=$!
QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-browse" QIMENG_LISTEN='127.0.0.1:18485' \
  QIMENG_BACKUP_TARGETS='{}' QIMENG_DEFAULT_BACKUP_TARGET='' \
  "$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-browse.log" 2>&1 &
browse_pid=$!
author_gate_pid=''
finish() {
  result=$?
  trap - EXIT
  mkdir -p android/build/cloud-screenshots
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next.ui.test/files android/build/cloud-screenshots/core-ui > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next/files/screenshots android/build/cloud-screenshots/app > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Pictures/QimengNextCloudTests android/build/cloud-screenshots/retained > /dev/null 2>&1 || true
  if [[ -n "$author_gate_pid" ]]; then kill "$author_gate_pid" 2>/dev/null || true; wait "$author_gate_pid" 2>/dev/null || true; fi
  kill "$service_pid" "$source_pid" "$statistics_pid" "$browse_pid" 2>/dev/null || true
  wait "$service_pid" 2>/dev/null || true
  wait "$source_pid" 2>/dev/null || true
  wait "$statistics_pid" 2>/dev/null || true
  wait "$browse_pid" 2>/dev/null || true
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
  for port in 18481 18482 18483 18485; do
    case "$port" in
      18481) pid=$service_pid ;;
      18482) pid=$source_pid ;;
      18483) pid=$statistics_pid ;;
      18485) pid=$browse_pid ;;
    esac
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
     curl --fail --silent http://127.0.0.1:18482/healthz > /dev/null &&
     curl --fail --silent http://127.0.0.1:18483/healthz > /dev/null &&
     curl --fail --silent http://127.0.0.1:18485/healthz > /dev/null; then ready=true; break; fi
  if ! kill -0 "$service_pid" 2>/dev/null; then transport_status 'startup: target host process exited'; exit 1; fi
  if ! kill -0 "$source_pid" 2>/dev/null; then transport_status 'startup: source host process exited'; exit 1; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo 'Isolated Android server did not become ready'; exit 1; fi
python tools/cloud_android_statistics_fixture.py
python tools/cloud_android_browse_fixture.py
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=320x180:rate=15' -t 12 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test.mp4"
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=320x180:rate=15' -vf hue=h=90 -t 12 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test-landscape.mp4"
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=180x320:rate=15' -t 12 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test-portrait.mp4"
prepare_transport core
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test.mp4" /data/local/tmp/qimeng-next-test.mp4
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test-landscape.mp4" /data/local/tmp/qimeng-next-test-landscape.mp4
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test-portrait.mp4" /data/local/tmp/qimeng-next-test-portrait.mp4
checks=0
gradle --no-daemon --continue -p android -Pqimeng.embedded=false :core:ui:connectedDebugAndroidTest :core:data:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceUrl=http://127.0.0.1:18482/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceToken="$source_token" \
  -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" --stacktrace || checks=1
prepare_transport app
gradle --no-daemon -p android -Pqimeng.embedded=false :app:connectedClientDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.AndroidRealServerTest,media.qimeng.next.test.StatisticsAndHomeTest,media.qimeng.next.test.ThumbnailCacheScreenTest,media.qimeng.next.test.GalleryBehaviorTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengBrowseUrl=http://127.0.0.1:18485/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengStatisticsUrl=http://127.0.0.1:18483/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceUrl=http://127.0.0.1:18482/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengSourceToken="$source_token" \
  -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" --stacktrace || checks=1

capture_album_cold_open() {
  adb -s emulator-5556 install -r android/app/build/outputs/apk/client/debug/app-client-debug.apk &&
    adb -s emulator-5556 install -r android/app/build/outputs/apk/androidTest/client/debug/app-client-debug-androidTest.apk || return 1
  local phase
  for phase in saveAndLeaveSelectionOpen readGridAfterForceStop; do
    adb -s emulator-5556 shell am force-stop media.qimeng.next || return 1
    local report="android/build/cloud-screenshots/album-$phase.txt"
    adb -s emulator-5556 shell am instrument -w \
      -e class "media.qimeng.next.test.AlbumColumnsColdOpenTest#$phase" \
      -e qimengBrowseUrl http://127.0.0.1:18485/ \
      -e qimengToken "$QIMENG_ADMIN_TOKEN" \
      media.qimeng.next.test/androidx.test.runner.AndroidJUnitRunner | tr -d '\r' | tee "$report" || return 1
    if ! grep -Fqx 'OK (1 test)' "$report"; then return 1; fi
  done
}
capture_album_cold_open || checks=1

python3 tools/cloud_android_author_gate.py &
author_gate_pid=$!
author_gate_ready=false
for attempt in $(seq 1 10); do
  if curl --fail --silent http://127.0.0.1:18484/healthz > /dev/null; then author_gate_ready=true; break; fi
  sleep 1
done
if [[ "$author_gate_ready" != true ]]; then echo 'Author UI fixture gate did not become ready'; exit 1; fi
adb -s emulator-5556 shell settings put secure show_ime_with_hard_keyboard 1
for layout in '360 1.0' '320 1.3'; do
  read -r author_width author_font <<< "$layout"
  prepare_transport "author-ui-$author_width"
  adb -s emulator-5556 reverse tcp:18481 tcp:18484
  adb -s emulator-5556 shell wm size "${author_width}x800"
  adb -s emulator-5556 shell settings put system font_scale "$author_font"
  gradle --no-daemon -p android -Pqimeng.embedded=false :app:connectedClientDebugAndroidTest \
    -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.AuthorPresentationTest \
    -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
    -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" \
    -Pandroid.testInstrumentationRunnerArguments.qimengAuthorWidth="$author_width" \
    -Pandroid.testInstrumentationRunnerArguments.qimengAuthorFont="$author_font" --stacktrace || checks=1
  results="android/app/build/outputs/androidTest-results/connected"
  if [[ -d "$results" ]]; then cp -r "$results" "android/build/cloud-screenshots/author-${author_width}dp-results"; fi
done
adb -s emulator-5556 reverse tcp:18481 tcp:18481
if (( checks != 0 )); then prepare_transport after-failure || true; fi
exit "$checks"
