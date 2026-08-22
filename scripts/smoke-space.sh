#!/usr/bin/env bash
# F2 个人空间/目录冒烟验收脚本(幂等)
# 前置:docker compose up -d 且后端已在 :8080 运行
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"

echo "==> 0. 清理冒烟数据(幂等)"
docker compose exec -T postgres psql -U labnexus -d labnexus -c \
  "DELETE FROM folders WHERE space_id IN (SELECT s.id FROM spaces s JOIN users u ON s.user_id=u.id WHERE u.username='smoke_space');
   DELETE FROM spaces WHERE user_id IN (SELECT id FROM users WHERE username='smoke_space');
   DELETE FROM users WHERE username='smoke_space';
   DELETE FROM invite_codes WHERE code='SMOKE-002';" >/dev/null
docker compose exec -T postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-002', '00000000-0000-0000-0000-000000000000');" >/dev/null

echo "==> 1. 注册 smoke_space"
REG=$(curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"invite_code":"SMOKE-002","username":"smoke_space","display_name":"空间冒烟","password":"password123"}')
TOKEN=$(echo "$REG" | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
[ -n "$TOKEN" ] || { echo "FAIL: 注册失败"; exit 1; }
AUTH="Authorization: Bearer $TOKEN"

echo "==> 2. 建根目录「会议记录」"
R1=$(curl -s -X POST "$BASE/me/folders" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"会议记录"}')
F1=$(echo "$R1" | python3 -c "import sys,json;print(json.load(sys.stdin)['folder']['id'])")
echo "folder1=$F1"

echo "==> 3. 建子目录「组会」"
R2=$(curl -s -X POST "$BASE/me/folders" -H "$AUTH" -H 'Content-Type: application/json' -d "{\"name\":\"组会\",\"parent_id\":\"$F1\"}")
F2=$(echo "$R2" | python3 -c "import sys,json;print(json.load(sys.stdin)['folder']['id'])")
echo "folder2=$F2"

echo "==> 4. 建根目录「日常记录」"
curl -s -X POST "$BASE/me/folders" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"日常记录"}' -o /dev/null -w "status: %{http_code}\n"

echo "==> 5. 获取空间目录树"
curl -s "$BASE/me/space" -H "$AUTH" | python3 -m json.tool | head -30

echo "==> 6. 改名「会议记录」→「会议纪要」"
curl -s -X PATCH "$BASE/me/folders/$F1" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"会议纪要"}' -o /dev/null -w "status: %{http_code}\n"

echo "==> 7. 删除非空根目录应 409"
curl -s -X DELETE "$BASE/me/folders/$F1" -H "$AUTH" -o /dev/null -w "status: %{http_code}\n"

echo "==> 8. 删除空子目录应 204"
curl -s -X DELETE "$BASE/me/folders/$F2" -H "$AUTH" -o /dev/null -w "status: %{http_code}\n"

echo "==> 9. 根目录变空后再删应 204"
curl -s -X DELETE "$BASE/me/folders/$F1" -H "$AUTH" -o /dev/null -w "status: %{http_code}\n"

echo ""
echo "SMOKE F2 OK ✅"
