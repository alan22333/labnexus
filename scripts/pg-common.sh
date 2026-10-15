#!/usr/bin/env bash
# 共享辅助(被 backup.sh / restore.sh source,不直接执行):
#   - Postgres 连接配置默认值(与 docker-compose / internal/config 一致)
#   - SHA-256 工具封装(GNU coreutils / BSD 兼容)
#   - 容器/主机 pg 工具选择,避免主机客户端版本与服务器不匹配

DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5433}"
DB_USER="${DB_USER:-labnexus}"
DB_PASSWORD="${DB_PASSWORD:-labnexus}"
DB_NAME="${DB_NAME:-labnexus}"
PG_CONTAINER="${PG_CONTAINER:-labnexus-postgres}"
PG_CONTAINER_PORT="${PG_CONTAINER_PORT:-5432}"
PG_USE_CONTAINER="${PG_USE_CONTAINER:-auto}"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "错误:缺少命令 $1。" >&2
    exit 1
  fi
}

# 是否在 DB 容器内执行 pg 工具:
#   always 强制容器;never 强制主机;auto(默认)在本机且容器运行时用容器。
pg_in_container() {
  case "$PG_USE_CONTAINER" in
    always) return 0 ;;
    never)  return 1 ;;
  esac
  case "$DB_HOST" in
    localhost|127.0.0.1|::1) ;;
    *) return 1 ;;
  esac
  command -v docker >/dev/null 2>&1 || return 1
  [ "$(docker inspect -f '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null || echo false)" = "true" ]
}

pg_dump_custom() {
  local out="$1"
  if pg_in_container; then
    docker exec -e PGPASSWORD="$DB_PASSWORD" "$PG_CONTAINER" \
      pg_dump -h 127.0.0.1 -p "$PG_CONTAINER_PORT" -U "$DB_USER" -d "$DB_NAME" \
      -Fc --no-owner --no-privileges > "$out"
  else
    PGPASSWORD="$DB_PASSWORD" pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
      -Fc --no-owner --no-privileges -f "$out"
  fi
}

pg_restore_custom() {
  local dump="$1"
  if pg_in_container; then
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$PG_CONTAINER" \
      pg_restore -h 127.0.0.1 -p "$PG_CONTAINER_PORT" -U "$DB_USER" -d "$DB_NAME" \
      --no-owner --no-privileges --exit-on-error < "$dump"
  else
    PGPASSWORD="$DB_PASSWORD" pg_restore -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
      --no-owner --no-privileges --exit-on-error < "$dump"
  fi
}

pg_psql() {
  local sql="$1"
  if pg_in_container; then
    docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$PG_CONTAINER" \
      psql -h 127.0.0.1 -p "$PG_CONTAINER_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -c "$sql"
  else
    PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -c "$sql"
  fi
}

# 校验 pg 工具可用性(容器模式检查 docker,主机模式检查 pg_dump/pg_restore/psql)。
require_pg_tools() {
  if pg_in_container; then
    require_cmd docker
  else
    require_cmd pg_dump
    require_cmd pg_restore
    require_cmd psql
  fi
  require_cmd tar
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    echo "错误:缺少 sha256sum / shasum,无法校验备份。" >&2
    exit 1
  fi
}
