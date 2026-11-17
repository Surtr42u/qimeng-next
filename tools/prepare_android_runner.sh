#!/usr/bin/env bash
set -euo pipefail
if [[ "${CI:-}" != "true" || "${RUNNER_OS:-}" != "Linux" || -z "${RUNNER_TEMP:-}" ]]; then
  echo 'Android runner preparation is restricted to the ephemeral Linux CI host.' >&2
  exit 2
fi
# Emulator 37 first-time API >= 24 setup raises userdata to at least 6 GiB,
# regardless of disk-size=2048M, and checks another 20% free-space allowance.
# Remove only these unused preinstalled toolchains. The workspace, Android SDK,
# Java, Gradle caches, downloaded native artifacts and test data are untouched.
sudo rm -rf -- /usr/share/dotnet /opt/ghc /usr/local/.ghcup /usr/local/swift /opt/hostedtoolcache/CodeQL
available=$(df --output=avail -B1 "$RUNNER_TEMP" | tail -n 1 | tr -d ' ')
if (( available < 12 * 1024 * 1024 * 1024 )); then
  echo 'Android CI needs at least 12 GiB free before system-image initialization and Gradle execution.' >&2
  df -h "$RUNNER_TEMP"
  exit 1
fi
df -h "$RUNNER_TEMP"
