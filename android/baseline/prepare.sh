#!/usr/bin/env bash
set -euo pipefail

[[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_ENVIRONMENT:-} == github-hosted && ${RUNNER_OS:-} == Linux ]]
work="${RUNNER_TEMP:?}/qimeng-installed-ui"
revision=745974081716a9d69a95002a41220e35e743d764
archive_sha=5337e94e60a582b27cb25a888363fa1d1c58c7490f50f00b00c4b80e0d111add
apk_sha=72cd0c695e888877a8f07f27486c308b1b0bf8179e57e90dabf19adb384dc84a
mkdir -p "$work"
git init --quiet "$work/source"
git -C "$work/source" fetch --quiet --depth 1 https://github.com/Surtr42u/qimeng-media.git "$revision"
git -C "$work/source" checkout --quiet --detach FETCH_HEAD
[[ $(git -C "$work/source" rev-parse HEAD) == "$revision" ]]
printf '%s\n' "$apk_sha" > "$work/reference-apk.sha256"
printf '%s\n' "$archive_sha" > "$work/reference-archive.sha256"
make -C "$work/source" sdk-go
(cd "$work/source/server" && go build -mod=readonly -o "$work/server" ./cmd/qimeng)

# The installed 0.2.3/code38 APK was retained by the original release workflow for seven days.
# When GitHub no longer serves it, the emulator step builds the original debug source at the same commit.
if gh api repos/Surtr42u/qimeng-media/actions/artifacts/14228034776/zip > "$work/release.zip"; then
  printf '%s  %s\n' "$archive_sha" "$work/release.zip" | sha256sum --check --status
  unzip -p "$work/release.zip" app-release.apk > "$work/release.apk"
  printf '%s  %s\n' "$apk_sha" "$work/release.apk" | sha256sum --check --status
else
  echo 'Original Actions artifact is unavailable to this run. The pinned source build will be identified in the screenshot manifest.'
fi
