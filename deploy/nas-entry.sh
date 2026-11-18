#!/bin/sh
set +x
set -eu
umask 077
QIMENG_ADMIN_PASSWORD=$(cat /run/secrets/admin_password)
QIMENG_ADMIN_TOKEN=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
export QIMENG_ADMIN_PASSWORD QIMENG_ADMIN_TOKEN
export QIMENG_LISTEN=0.0.0.0:8421 QIMENG_DATA_DIR=/var/lib/qimeng QIMENG_WEB_DIR=/opt/qimeng/web
export QIMENG_STORAGE_ROOTS='{"media":"/media/qimeng"}'
exec /opt/qimeng/qimeng-next
