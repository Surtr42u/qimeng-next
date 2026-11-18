#!/usr/bin/env bash
set +x
set -euo pipefail
umask 077
app=$(cd -- "$(dirname -- "$0")" && pwd -P)
state=${QIMENG_PACKAGE_STATE:-${XDG_DATA_HOME:-$HOME/.local/share}/qimeng-next-server}
port=${QIMENG_PACKAGE_PORT:-8421}
secret_override=${QIMENG_PACKAGE_PASSWORD_FILE:-}
mode=${1:-start}
case "$mode" in start|nas|nas-media|nas-stop) ;; *) echo '用法：bash start.sh [start|nas|nas-media|nas-stop]' >&2; exit 2 ;; esac
[[ "$state" == /* && "$port" =~ ^[0-9]{1,5}$ && 10#$port -ge 1 && 10#$port -le 65535 ]] || { echo '需要绝对持久目录和有效端口。' >&2; exit 2; }
mkdir -p -- "$state"
state=$(cd -- "$state" && pwd -P)
case "$state/" in "$app/"*) echo '持久目录必须放在软件解压位置之外。' >&2; exit 1 ;; esac
marker="$state/.qimeng-next-package"
if [[ -f "$marker" ]]; then
  [[ $(cat -- "$marker") == qimeng-next-package-v1 ]] || { echo '无法确认数据目录归属。' >&2; exit 1; }
else
  [[ -z $(find "$state" -mindepth 1 -maxdepth 1 -print -quit) ]] || { echo '该位置已有其他文件，请改用空的专用数据目录。' >&2; exit 1; }
  printf '%s\n' 'qimeng-next-package-v1' > "$marker"
fi
data="$state/data"
media="$state/media"
private="$state/private"
mkdir -p -- "$data" "$media/我的影库" "$private"
secret=${secret_override:-$private/login}
if [[ $mode != nas-stop && ! -f "$secret" ]]; then
  [[ -z "$secret_override" && -t 0 ]] || { echo '请在终端首次启动，或指定已准备好的受限密码文件。' >&2; exit 1; }
  IFS= read -r -s -p '设置影库登录密码（至少 12 个字符，输入隐藏）：' chosen
  printf '\n'
  [[ ${#chosen} -ge 12 && $(printf %s "$chosen" | wc -c) -le 1024 ]] || { echo '密码长度不符合要求。' >&2; exit 1; }
  tmp=$(mktemp "$private/login.XXXXXXXX")
  printf '%s\n' "$chosen" > "$tmp"
  unset chosen
  if ! ln "$tmp" "$secret"; then rm -- "$tmp"; echo '已有另一个启动过程正在设置密码，请重新启动。' >&2; exit 1; fi
  rm -- "$tmp"
fi
if [[ ! -f "$secret" || -L "$secret" || ! -O "$secret" ]] || (( (8#$(stat -c '%a' -- "$secret") & 077) != 0 )); then
  echo '密码文件必须为当前用户持有的私有普通文件（权限 600 或 400）。' >&2; exit 1
fi
if [[ "$mode" == nas || "$mode" == nas-media || "$mode" == nas-stop ]]; then
  export QIMENG_PACKAGE_APP="$app" QIMENG_PACKAGE_DATA="$data" QIMENG_PACKAGE_MEDIA="$media" QIMENG_PACKAGE_SECRET="$secret"
  export QIMENG_PACKAGE_UID=$(id -u) QIMENG_PACKAGE_GID=$(id -g) QIMENG_PACKAGE_PORT="$port"
  project="qimeng-next-$(printf %s "$state" | cksum | cut -d' ' -f1)"
  unset QIMENG_PACKAGE_IMAGE
  if [[ "$mode" == nas-stop ]]; then exec docker compose --project-name "$project" --file "$app/compose.yaml" stop; fi
  if [[ "$mode" == nas-media ]]; then
    export QIMENG_PACKAGE_IMAGE=qimeng-next-local-media:alpine3.24.4-ffmpeg8.1.3-r0
    # Empty build context; no media, database, or credentials are sent to Docker.
    docker build --tag "$QIMENG_PACKAGE_IMAGE" - < "$app/Dockerfile.media"
  fi
  docker compose --project-name "$project" --file "$app/compose.yaml" up -d
  echo "NAS 主机回环地址为 http://127.0.0.1:$port/，远程浏览请使用快速开始中的隧道。"
  exit 0
fi
# Use only this package's dedicated roots. Service credentials are environment
# entries of the child and never command-line arguments.
for key in ${!QIMENG_@}; do unset "$key"; done
if [[ "$media" =~ [[:cntrl:]] ]]; then echo '持久目录中不能有控制字符。' >&2; exit 1; fi
json_path=${media//\\/\\\\}
json_path=${json_path//\"/\\\"}
QIMENG_ADMIN_PASSWORD=$(< "$secret")
QIMENG_ADMIN_TOKEN=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
export QIMENG_ADMIN_PASSWORD QIMENG_ADMIN_TOKEN
export QIMENG_LISTEN="127.0.0.1:$port" QIMENG_DATA_DIR="$data" QIMENG_WEB_DIR="$app/web"
export QIMENG_STORAGE_ROOTS="{\"media\":\"$json_path\"}"
echo "正在启动影库。成功后请在浏览器打开 http://127.0.0.1:$port/ ，此终端按 Ctrl+C 停止。"
exec "$app/qimeng-next"
