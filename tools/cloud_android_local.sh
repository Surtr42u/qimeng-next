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
