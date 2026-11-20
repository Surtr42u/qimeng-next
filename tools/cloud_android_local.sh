#!/usr/bin/env bash
set -euo pipefail
if [[ "${GITHUB_ACTIONS:-}" != "true" || "${RUNNER_ENVIRONMENT:-}" != "github-hosted" || "${ANDROID_SERIAL:-}" != "emulator-5556" ]]; then
  echo 'Local runtime verification requires the explicit cloud emulator.' >&2
  exit 2
fi
if [[ $(adb -s emulator-5556 emu avd name | tr -d '\r' | head -1) != qimeng_api35 ]]; then
  echo 'The connected AVD does not match the embedded test target.' >&2
  exit 2
fi
ffmpeg -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=320x180:rate=15' -t 4 -c:v libx264 -pix_fmt yuv420p "$RUNNER_TEMP/qimeng-next-test.mp4"
adb -s emulator-5556 push "$RUNNER_TEMP/qimeng-next-test.mp4" /data/local/tmp/qimeng-next-test.mp4
collect_screenshots() {
  result=$?
  trap - EXIT
  mkdir -p android/build/cloud-screenshots
  adb -s emulator-5556 pull /sdcard/Pictures/QimengNextCloudTests android/build/cloud-screenshots/retained > /dev/null 2>&1 || true
  exit "$result"
}
trap collect_screenshots EXIT
gradle --no-daemon -p android :app:connectedEmbeddedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.EmbeddedRuntimeTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengEmbedded=true --stacktrace

# Independent instrumentation invocations ensure Android cold-starts the target process.
# This uses fresh synthetic data on the guarded emulator, never an attached device.
apk=android/app/build/outputs/apk/embedded/debug/app-embedded-debug.apk
test_apk=android/app/build/outputs/apk/androidTest/embedded/debug/app-embedded-debug-androidTest.apk
adb -s emulator-5556 install -r "$apk"
adb -s emulator-5556 install -r "$test_apk"
[[ $(adb -s emulator-5556 shell pm clear media.qimeng.next | tr -d '\r') == Success ]]

cold_test=media.qimeng.next.test.EmbeddedColdStartTest
run_cold_phase() {
  local phase=$1
  adb -s emulator-5556 shell am instrument -w -r \
    -e class "${cold_test}#${phase}" -e qimengColdStart true \
    media.qimeng.next.test/androidx.test.runner.AndroidJUnitRunner \
    | tee "android/build/cloud-screenshots/cold-start-${phase}.log"
  grep -Fq 'OK (1 test)' "android/build/cloud-screenshots/cold-start-${phase}.log"
}
mkdir -p android/build/cloud-screenshots
run_cold_phase seed
# Instrumentation has returned and the fixture is durably sealed before any process stop.
adb -s emulator-5556 shell am force-stop media.qimeng.next
run_cold_phase recover
