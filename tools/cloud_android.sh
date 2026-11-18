#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" || "${ANDROID_SERIAL:-}" != "emulator-5556" ]]; then
  echo 'Android verification requires the explicit cloud emulator.' >&2
  exit 2
fi
export QIMENG_DATA_DIR="$RUNNER_TEMP/qimeng-next-android"
export QIMENG_LISTEN='127.0.0.1:18481'
export QIMENG_TIMEZONE='Asia/Shanghai'
export QIMENG_ADMIN_TOKEN='synthetic-cloud-only-owner-credential-18481'
chmod +x "$RUNNER_TEMP/server-artifact/build/qimeng-next"
"$RUNNER_TEMP/server-artifact/build/qimeng-next" > "$RUNNER_TEMP/qimeng-next-android.log" 2>&1 &
service_pid=$!
finish() {
  result=$?
  trap - EXIT
  mkdir -p android/build/cloud-screenshots
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next.ui.test/files android/build/cloud-screenshots/core-ui > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Android/data/media.qimeng.next/files/screenshots android/build/cloud-screenshots/app > /dev/null 2>&1 || true
  adb -s emulator-5556 pull /sdcard/Pictures/QimengNextCloudTests android/build/cloud-screenshots/retained > /dev/null 2>&1 || true
  kill "$service_pid" 2>/dev/null || true
  wait "$service_pid" 2>/dev/null || true
  exit "$result"
}
trap finish EXIT
ready=false
for attempt in $(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:18481/healthz > /dev/null; then ready=true; break; fi
  if ! kill -0 "$service_pid" 2>/dev/null; then cat "$RUNNER_TEMP/qimeng-next-android.log"; exit 1; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo 'Isolated Android server did not become ready'; exit 1; fi
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=160x240:rate=15' -t 2 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test.mp4"
adb -s emulator-5556 reverse tcp:18481 tcp:18481
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test.mp4" /data/local/tmp/qimeng-next-test.mp4
gradle --no-daemon -p android -Pqimeng.embedded=false :core:ui:connectedDebugAndroidTest :core:data:connectedDebugAndroidTest --stacktrace
gradle --no-daemon -p android -Pqimeng.embedded=false :app:connectedClientDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.AndroidRealServerTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengServerUrl=http://127.0.0.1:18481/ \
  -Pandroid.testInstrumentationRunnerArguments.qimengToken="$QIMENG_ADMIN_TOKEN" --stacktrace
