#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" || "${ANDROID_SERIAL:-}" != "emulator-5556" ]]; then
  echo 'Local runtime verification requires the explicit cloud emulator.' >&2
  exit 2
fi
gradle --no-daemon -p android :app:connectedEmbeddedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=media.qimeng.next.test.EmbeddedRuntimeTest \
  -Pandroid.testInstrumentationRunnerArguments.qimengEmbedded=true --stacktrace
