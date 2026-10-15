#!/usr/bin/env bash
# LabNexus 备份:PostgreSQL custom-format dump + data/uploads + SHA-256 校验清单。
# 产物:backups/labnexus-YYYYmmdd-HHMMSS.tar.gz
# 用法:bash scripts/backup.sh [输出包路径]   或   make backup
# 可覆盖环境变量:DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME UPLOADS_DIR BACKUP_DIR
#                 PG_CONTAINER PG_USE_CONTAINER(auto|always|never)
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/pg-common.sh
source scripts/pg-common.sh

UPLOADS_DIR="${UPLOADS_DIR:-data/uploads}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
FORMAT_VERSION="1"

require_pg_tools

TS="$(date +%Y%m%d-%H%M%S)"
OUT="${1:-${BACKUP_DIR}/labnexus-${TS}.tar.gz}"
mkdir -p "$(dirname "$OUT")"
OUT_ABS="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> 1. 导出 PostgreSQL(${DB_NAME}@${DB_HOST}:${DB_PORT})"
if ! pg_dump_custom "$WORK/database.dump"; then
  echo "错误:pg_dump 失败(数据库是否已启动?)。未生成备份包。" >&2
  exit 1
fi
if [ ! -s "$WORK/database.dump" ]; then
  echo "错误:导出的 dump 为空。未生成备份包。" >&2
  exit 1
fi

echo "==> 2. 打包上传目录(${UPLOADS_DIR})"
mkdir -p "$WORK/stage/uploads"
if [ -d "$UPLOADS_DIR" ]; then
  cp -R "$UPLOADS_DIR/." "$WORK/stage/uploads/"
fi
tar -czf "$WORK/uploads.tar.gz" -C "$WORK/stage" uploads

echo "==> 3. 生成校验清单"
{
  echo "# LabNexus backup manifest"
  echo "# format_version=${FORMAT_VERSION}"
  echo "# created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# database=${DB_NAME}"
  echo "$(sha256_of "$WORK/database.dump")  database.dump"
  echo "$(sha256_of "$WORK/uploads.tar.gz")  uploads.tar.gz"
} > "$WORK/MANIFEST.sha256"

echo "==> 4. 组装备份包"
tar -czf "$OUT_ABS" -C "$WORK" MANIFEST.sha256 database.dump uploads.tar.gz
if ! tar -tzf "$OUT_ABS" >/dev/null 2>&1; then
  echo "错误:备份包完整性校验失败,已删除。" >&2
  rm -f "$OUT_ABS"
  exit 1
fi

SIZE="$(du -h "$OUT_ABS" | awk '{print $1}')"
echo ""
echo "✅ 备份完成: ${OUT_ABS} (${SIZE})"
