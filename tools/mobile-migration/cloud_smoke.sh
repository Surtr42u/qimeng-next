#!/usr/bin/env bash
set -euo pipefail
[[ "${CI:-}" == true && "${GITHUB_ACTIONS:-}" == true && "${ANDROID_SERIAL:-}" == emulator-5556 ]]
device=(adb -s emulator-5556)
[[ "$("${device[@]}" emu avd name | tr -d '\r' | head -1)" == qimeng_api35 ]]
package=media.qimeng.next
runner="$package.test/.MigrationAcceptance"
"${device[@]}" install -t "$RUNNER_TEMP/cloud-host.apk"
"${device[@]}" install -t "$RUNNER_TEMP/cloud-fixture.apk"
"${device[@]}" install -t "$RUNNER_TEMP/companion/migration-acceptance.apk"
"${device[@]}" shell appops set --uid "$package" MANAGE_EXTERNAL_STORAGE allow

wake() {
  # This exported AndroidX shell receiver accepts SKIP_FILE. Its two mutating operations
  # are WRITE_SKIP_FILE / DELETE_SKIP_FILE. An unknown extra wakes the app and returns.
  "${device[@]}" shell am broadcast --user current --include-stopped-packages \
    -a androidx.profileinstaller.action.SKIP_FILE --es EXTRA_SKIP_FILE_OPERATION MIGRATION_NO_OP \
    -n "$package/androidx.profileinstaller.ProfileInstallReceiver" >/dev/null
  "${device[@]}" shell pidof -s "$package" >/dev/null
}
instrument() {
  wake
  output=$(timeout 270 "${device[@]}" shell am instrument --user current --no-restart --always-check-signature -w -r "$@" | tr -d '\r')
  echo "$output"
}
accept() {
  instrument -e command "$1" "$runner"
  [[ "$output" == *'INSTRUMENTATION_RESULT: ok=true'* && "$output" == *'INSTRUMENTATION_CODE: -1'* ]]
}
wake
instrument -e synthetic true "$package.cloudfixture/.CloudFixture"
[[ "$output" == *'INSTRUMENTATION_RESULT: seeded=true'* ]]
before=$("${device[@]}" shell pidof -s "$package" | tr -d '\r')
accept quiesce
[[ "$output" == *'stopped=true'* && "$output" == *'previouslyEnabled=true'* ]]
accept quiesce
[[ "$output" == *'previouslyEnabled=true'* ]]
"${device[@]}" shell run-as "$package" cp cache/cloud-check-bad.json cache/qimeng-migration-check.json
instrument -e command accept "$runner"
[[ "$output" == *'INSTRUMENTATION_RESULT: ok=false'* && "$output" == *'versionedOriginal=false'* ]]
"${device[@]}" shell run-as "$package" cp cache/cloud-check-good.json cache/qimeng-migration-check.json
accept accept
for field in countsMatch authorsReadable tagsMatch sharedLibrary digestMatches eventsUnchanged lifecycleRestored selectionPreserved; do
  [[ "$output" == *"$field=true"* ]]
done
[[ "$output" == *'originalsVerified=1'* ]]
after=$("${device[@]}" shell pidof -s "$package" | tr -d '\r')
[[ "$before" == "$after" ]]
services=$("${device[@]}" shell dumpsys activity services "$package")
[[ "$services" == *'EmbeddedService'* && "$services" == *'isForeground=true'* ]]
echo 'hostProcessPreserved=true'
echo 'hostForegroundServiceRunning=true'
