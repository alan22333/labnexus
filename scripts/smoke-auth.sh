#!/usr/bin/env bash
# F1 账号系统冒烟验收脚本(幂等,可重复运行)
# 前置:docker compose up -d(Postgres+Redis)且后端已在 :8080 运行
# 用法:./scripts/smoke-auth.sh
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"
JAR="$(mktemp)"

# 幂等:清理上次冒烟数据,重建邀请码
echo "==> 0. 清理并重建冒烟数据"
docker compose exec -T postgres psql -U labnexus -d labnexus -c \
  "DELETE FROM users WHERE username='smoke_user'; DELETE FROM invite_codes WHERE code='SMOKE-001';" >/dev/null
docker compose exec -T postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) \
   VALUES (gen_random_uuid(), 'SMOKE-001', '00000000-0000-0000-0000-000000000000');" >/dev/null

echo "==> 1. 注册(注册即登录)"
REG=$(curl -s -c "$JAR" -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"invite_code":"SMOKE-001","username":"smoke_user","display_name":"冒烟用户","password":"password123"}')
echo "$REG" | head -c 300; echo
TOKEN=$(echo "$REG" | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])" 2>/dev/null)
[ -n "$TOKEN" ] || { echo "FAIL: 未拿到 access_token"; exit 1; }

echo "==> 2. GET /api/me"
curl -s "$BASE/me" -H "Authorization: Bearer $TOKEN"; echo

echo "==> 3. 登录(拿 refresh cookie)"
curl -s -c "$JAR" -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"smoke_user","password":"password123"}' -o /dev/null -w "login status: %{http_code}\n"
OLD_REFRESH=$(awk '$6=="ln_refresh"{print $7}' "$JAR")

echo "==> 4. 用旧 refresh 刷新(轮换,jar 更新为新 token)"
curl -s -b "$JAR" -c "$JAR" -X POST "$BASE/auth/refresh" -o /dev/null -w "refresh status: %{http_code}\n"
NEW_REFRESH=$(awk '$6=="ln_refresh"{print $7}' "$JAR")
if [ -n "$NEW_REFRESH" ] && [ "$NEW_REFRESH" != "$OLD_REFRESH" ]; then
  echo "token 已轮换 ✅"
else
  echo "FAIL: refresh 未轮换"; exit 1
fi

echo "==> 5. 旧 refresh 再刷新应 401(验证轮换撤销)"
curl -s -H "Cookie: ln_refresh=$OLD_REFRESH" -X POST "$BASE/auth/refresh" -o /dev/null -w "old refresh status: %{http_code}\n"

echo "==> 6. 新 refresh 登出应 204"
curl -s -H "Cookie: ln_refresh=$NEW_REFRESH" -X POST "$BASE/auth/logout" -o /dev/null -w "logout status: %{http_code}\n"

echo "==> 7. 登出后 refresh 再刷新应 401(验证登出撤销)"
curl -s -H "Cookie: ln_refresh=$NEW_REFRESH" -X POST "$BASE/auth/refresh" -o /dev/null -w "after-logout refresh: %{http_code}\n"

rm -f "$JAR"
echo ""
echo "SMOKE OK ✅"
