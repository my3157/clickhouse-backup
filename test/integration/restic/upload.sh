#!/usr/bin/env bash
set -xeuo pipefail
CUR_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
source "${CUR_DIR}/init.sh"
BACKUP_NAME=$1
DIFF_FROM_REMOTE=${2:-}
DIFF_FROM_REMOTE_CMD=""
LOCAL_PATHS=$(clickhouse-client -q "SELECT concat(trim(TRAILING '/' FROM path),'/backup/','${BACKUP_NAME}') FROM system.disks FORMAT TSVRaw" | awk '{printf("%s ",$0)} END { printf "\n" }')
if [[ "" != "${DIFF_FROM_REMOTE}" ]]; then
  DIFF_FROM_REMOTE_CMD="--parent ${DIFF_FROM_REMOTE}"
fi
restic backup $DIFF_FROM_REMOTE_CMD --verbose --tag "${BACKUP_NAME}"  $LOCAL_PATHS
restic forget --keep-last ${RESTIC_KEEP_LAST} --prune