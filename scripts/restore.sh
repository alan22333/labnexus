#!/usr/bin/env bash
# LabNexus 恢复:校验备份包后恢复 PostgreSQL 与 data/uploads。
# 用法:make restore FILE=backups/xxx.tar.gz CONFIRM=yes
# 安全:未显式 CONFIRM=yes 时拒绝执行;校验失败或数据库恢复失败均返回非 0。
# 可覆盖环境变量:DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME UPLOADS_DIR
#                 PG_CONTAINER PG_USE_CONTAINER(auto|always|never)
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/pg-common.sh
source scripts/pg-common.sh

UPLOADS_DIR="${UPLOADS_DIR:-data/uploads}"
FILE="${FILE:-${1:-}}"

# ---- 参数与确认校验(先于任何写操作)----
if [ -z "$FILE" ]; then
  echo "用法:make restore FILE=<备份包> CONFIRM=yes" >&2
  exit 2
fi
if [ ! -f "$FILE" ]; then
  echo "错误:备份包不存在: $FILE" >&2
  exit 1
fi
if [ "${CONFIRM:-}" != "yes" ]; then
  echo "拒绝执行:恢复会覆盖当前数据库与上传目录。请显式确认:CONFIRM=yes" >&2
  exit 1
fi

require_pg_tools

FILE_ABS="$(cd "$(dirname "$FILE")" && pwd)/$(basename "$FILE")"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> 1. 解包并校验备份"
if ! tar -xzf "$FILE_ABS" -C "$WORK" 2>/dev/null; then
  echo "错误:备份包无法解压或已损坏: $FILE" >&2
  exit 1
fi
if [ ! -f "$WORK/MANIFEST.sha256" ]; then
  echo "错误:备份包缺少 MANIFEST.sha256。" >&2
  exit 1
fi

while IFS= read -r line; do
  case "$line" in
    ""|\#*) continue ;;
  esac
  expected="$(printf '%s' "$line" | awk '{print $1}')"
  name="$(printf '%s' "$line" | awk '{print $2}')"
  if [ -z "$name" ] || [ ! -f "$WORK/$name" ]; then
    echo "错误:备份缺少文件 $name。" >&2
    exit 1
  fi
  actual="$(sha256_of "$WORK/$name")"
  if [ "$actual" != "$expected" ]; then
    echo "错误:校验失败,文件内容与清单不符: $name" >&2
    exit 1
  fi
done < "$WORK/MANIFEST.sha256"
echo "    校验通过: $FILE_ABS"

# ---- 恢复前保存当前上传目录,便于数据库恢复失败时回滚 ----
PREV_UPLOADS=""
if [ -d "$UPLOADS_DIR" ]; then
  PREV_UPLOADS="$(mktemp -d)"
  cp -R "$UPLOADS_DIR/." "$PREV_UPLOADS/"
fi

rollback_uploads() {
  rm -rf "$UPLOADS_DIR"
  if [ -n "$PREV_UPLOADS" ]; then
    mkdir -p "$UPLOADS_DIR"
    cp -R "$PREV_UPLOADS/." "$UPLOADS_DIR/"
  fi
}

echo "==> 2. 恢复上传目录(${UPLOADS_DIR})"
mkdir -p "$WORK/extracted"
tar -xzf "$WORK/uploads.tar.gz" -C "$WORK/extracted"
rm -rf "$UPLOADS_DIR"
mkdir -p "$(dirname "$UPLOADS_DIR")"
if [ -d "$WORK/extracted/uploads" ]; then
  mv "$WORK/extracted/uploads" "$UPLOADS_DIR"
else
  mkdir -p "$UPLOADS_DIR"
fi

echo "==> 3. 恢复数据库(${DB_NAME}@${DB_HOST}:${DB_PORT})"
if ! pg_psql "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;" >/dev/null; then
  echo "错误:清理数据库失败,回滚上传目录。" >&2
  rollback_uploads
  exit 1
fi
if ! pg_restore_custom "$WORK/database.dump"; then
  echo "错误:数据库恢复失败,回滚上传目录。" >&2
  rollback_uploads
  exit 1
fi

echo ""
echo "✅ 恢复完成: ${FILE_ABS}"
