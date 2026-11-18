#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" || "${ANDROID_SERIAL:-}" != "emulator-5556" ]]; then
  echo 'Local runtime verification requires the explicit cloud emulator.' >&2
  exit 2
fi
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
