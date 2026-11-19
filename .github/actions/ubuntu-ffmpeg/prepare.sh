#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

if [[ $1 == plan ]]; then
  if ffmpeg -version >/dev/null 2>&1 && ffprobe -version >/dev/null 2>&1; then
    echo 'FFmpeg and FFprobe already work on this runner.'
    exit 0
  fi
  APT_WORK=$(mktemp -d /var/tmp/ubuntu-ffmpeg.XXXXXXXX)
  # APT's unprivileged downloader must be able to traverse this directory.
  chmod 755 "$APT_WORK"
  mkdir "$APT_WORK/archives"
fi
archives="$APT_WORK/archives"
checksums="$APT_WORK/SHA256SUMS"
ubuntu_sources=/etc/apt/sources.list.d/ubuntu.sources
apt=(sudo apt-get -o "Dir::Etc::sourcelist=$ubuntu_sources" -o Dir::Etc::sourceparts=-
     -o "Dir::Cache::archives=$archives")
install=(--no-install-recommends --no-remove --reinstall install ffmpeg)

if [[ $1 == plan ]]; then
  test -s "$ubuntu_sources"
  time "${apt[@]}" --error-on=any -o APT::Get::List-Cleanup=false update
  # The archives directory must be empty here. APT omits archives it finds there by size.
  time "${apt[@]}" -qq --print-uris -o Acquire::ForceHash=SHA256 "${install[@]}" > "$APT_WORK/uris"
  while read -r uri name size hash; do
    if [[ ! $hash =~ ^SHA256:[0-9a-f]{64}$ || $name == */* || $name != *.deb ]]; then
      echo 'Expected an Ubuntu package archive and its SHA-256 from APT.' >&2
      exit 1
    fi
    printf '%s  %s\n' "${hash#SHA256:}" "$name"
  done < "$APT_WORK/uris" | sort > "$checksums"
  test -s "$checksums"
  awk '{bytes += $3} END {printf "APT selected %d archives, %.0f bytes\n", NR, bytes}' "$APT_WORK/uris"
  source /etc/os-release
  digest=$(sha256sum "$checksums" | cut -d' ' -f1)
  echo "work=$APT_WORK" >> "$GITHUB_OUTPUT"
  echo "key=ubuntu-ffmpeg-v1-$VERSION_ID-$(dpkg --print-architecture)-$digest" >> "$GITHUB_OUTPUT"
  exit 0
fi

cache="$RUNNER_TEMP/ubuntu-ffmpeg-debs"
reused=0
if [[ -d $cache && ! -L $cache ]]; then
  while read -r digest name; do
    if [[ -f $cache/$name && ! -L $cache/$name ]]; then
      cp -- "$cache/$name" "$archives/$name"
      if (cd "$archives" && printf '%s  %s\n' "$digest" "$name" | sha256sum -c --status); then
        ((reused+=1))
      else
        echo "Discarding mismatched cached archive $name"
        rm -- "$archives/$name"
      fi
    fi
  done < "$checksums"
fi
echo "Actions cache hit=${CACHE_HIT:-false}; reused $reused verified archives."
time "${apt[@]}" -y --download-only "${install[@]}"
(cd "$archives" && sha256sum --check --quiet "$checksums")

if [[ $CACHE_HIT != true ]]; then
  # A failed restore can leave an incomplete directory. Save only this transaction's archives.
  if [[ -e $cache || -L $cache ]]; then
    mv -T -- "$cache" "$(mktemp -d "$RUNNER_TEMP/ffmpeg-restore.XXXXXXXX")/previous"
  fi
  mkdir "$cache"
  while read -r digest name; do
    cp -- "$archives/$name" "$cache/$name"
  done < "$checksums"
fi
time "${apt[@]}" -y --no-download "${install[@]}"
ffmpeg -version | sed -n '1p'
ffprobe -version | sed -n '1p'
