#!/usr/bin/env bash
set -euo pipefail

[[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_ENVIRONMENT:-} == github-hosted && ${RUNNER_OS:-} == Linux ]]
[[ ${ANDROID_SERIAL:-} == emulator-5556 ]]
device=(adb -s emulator-5556)
[[ $("${device[@]}" emu avd name | tr -d '\r' | head -1) == qimeng_api35 ]]
[[ $("${device[@]}" shell getprop ro.build.version.sdk | tr -d '\r') == 35 ]]
work="${RUNNER_TEMP:?}/qimeng-installed-ui"
output="${GITHUB_WORKSPACE:?}/android/build/installed-ui-baseline"
mkdir -p "$output" "$work/media" "$work/data"
mode=installed-release
apk="$work/release.apk"
abis=$("${device[@]}" shell getprop ro.product.cpu.abilist | tr -d '\r')
if [[ ! -s $apk || ,$abis, != *,arm64-v8a,* ]]; then
  mode=pinned-source-debug
  make -C "$work/source" sdk-kotlin
  (cd "$work/source/android" && bash gradlew --no-daemon :app:assembleDebug)
  apk="$work/source/android/app/build/outputs/apk/debug/app-debug.apk"
fi
# A present package means this is not the empty independent baseline device.
if [[ -n $("${device[@]}" shell pm list packages media.qimeng.app | tr -d '\r') ]]; then
  echo 'The legacy baseline requires a fresh cloud AVD.' >&2
  exit 1
fi
"${device[@]}" install "$apk"
echo "Installed baseline package ($mode)."
installed=$("${device[@]}" shell pm path media.qimeng.app | tr -d '\r' | sed -n 's/^package://p' | head -1)
if [[ -z $installed ]]; then echo 'Package manager returned no installed APK path.' >&2; exit 1; fi
# Use the file-sync protocol for binary APKs, as in the original installation receipt.
if ! "${device[@]}" pull "$installed" "$work/readback.apk" > "$work/pull.log" 2>&1; then
  echo 'ADB could not read back the installed APK over its file-sync protocol.' >&2
  exit 1
fi
sha256sum "$work/readback.apk" | cut -d ' ' -f 1 > "$work/installed.sha256"
jq -n --arg source "$(git -C "$work/source" rev-parse HEAD)" --arg mode "$mode" \
  --arg installed "$(cat "$work/installed.sha256")" --arg reference "$(cat "$work/reference-apk.sha256")" \
  --arg archive "$(cat "$work/reference-archive.sha256")" --arg run "$GITHUB_RUN_ID" \
  '{sourceRevision:$source,installedBuild:$mode,installedApkSha256:$installed,referenceApkSha256:$reference,
    referenceArchiveSha256:$archive,referenceArtifactId:14228034776,referenceRunId:43873579739,captureRunId:$run}' \
  > "$output/provenance.json"
if [[ $(cat "$work/installed.sha256") != "$(sha256sum "$apk" | cut -d ' ' -f 1)" ]]; then
  echo "Installed APK digest differs from the candidate. Byte counts: candidate=$(stat -c %s "$apk"), readback=$(stat -c %s "$work/readback.apk")." >&2
  exit 1
fi

export QIMENG_LISTEN=127.0.0.1:18483 QIMENG_AUTH_DEV_MODE=1 QIMENG_TIMEZONE=Asia/Shanghai
export QIMENG_DATA_DIR="$work/data" QIMENG_DB_PATH="$work/data/qimeng.db"
(cd "$work" && exec "$work/server") > "$work/server.log" 2>&1 &
pid=$!
finish() {
  "${device[@]}" reverse --remove tcp:18483 >/dev/null 2>&1 || true
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
trap finish EXIT
"${device[@]}" reverse tcp:18483 tcp:18483
for attempt in {1..60}; do
  if curl --fail --silent http://127.0.0.1:18483/healthz >/dev/null; then break; fi
  kill -0 "$pid"
  sleep 1
done
curl --fail --silent http://127.0.0.1:18483/healthz >/dev/null
ffmpeg -nostdin -y -hide_banner -loglevel error -f lavfi -i 'testsrc2=size=320x180:rate=15' -t 12 -c:v libx264 -pix_fmt yuv420p "$work/media/android-synthetic.mp4"
/usr/bin/python3 android/baseline/capture.py "$work" "$output" "$mode"
