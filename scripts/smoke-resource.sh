#!/usr/bin/env bash
# F7/F8 资源库/文献元数据 冒烟验收脚本(幂等)
# 前置:docker compose up -d 且后端已在 :8080 运行
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"

echo "==> 0. 清理冒烟数据(幂等)"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  DELETE FROM resource_tags;
  DELETE FROM resources;
  DELETE FROM spaces WHERE user_id IN (SELECT id FROM users WHERE username='smoke_res');
  DELETE FROM users WHERE username='smoke_res';
  DELETE FROM invite_codes WHERE code='SMOKE-005';
  DELETE FROM tags WHERE name IN ('冒烟资源标签');" >/dev/null
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-005', '00000000-0000-0000-0000-000000000000');" >/dev/null
rm -rf data/uploads/*

echo "==> 1. 注册 + 建标签"
TOK=$(curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"invite_code":"SMOKE-005","username":"smoke_res","display_name":"资源冒烟","password":"password123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
AUTH="Authorization: Bearer $TOK"
TAG=$(curl -s -X POST "$BASE/tags" -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"冒烟资源标签"}')
TAGID=$(echo "$TAG" | python3 -c "import sys,json;print(json.load(sys.stdin)['tag']['id'])")

echo "==> 2. 建 link 资源(带标签)"
curl -s -X POST "$BASE/resources" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"type\":\"link\",\"title\":\"冒烟链接\",\"url\":\"https://example.com\",\"tag_ids\":[\"$TAGID\"]}" \
  -o /dev/null -w "create link: %{http_code}\n"

echo "==> 3. 建 paper 资源"
curl -s -X POST "$BASE/resources" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"type":"paper","title":"冒烟文献","doi":"10.1000/smoke"}' -o /dev/null -w "create paper: %{http_code}\n"

echo "==> 4. 上传文件"
curl -s -X POST "$BASE/resources/upload" -H "$AUTH" \
  -F "file=@README.md;filename=README.md" -o /dev/null -w "upload: %{http_code}\n"
ls data/uploads/ | head -2

echo "==> 5. 列表(共享可见,含上传者/标签)"
curl -s "$BASE/resources" -H "$AUTH" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print('total:', d['pagination']['total'])
for r in d['resources']:
    print(' -', r['type'], '|', r['title'], '| uploader:', r['uploader']['display_name'], '| tags:', [t['name'] for t in r['tags']])
assert d['pagination']['total'] == 3, '应有 3 条资源'
print('列表 OK')"

echo "==> 6. 筛选:type=link / keyword=文献"
curl -s "$BASE/resources?type=link" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==1 and d['resources'][0]['type']=='link'; print('type 筛选 OK')"
curl -s "$BASE/resources?keyword=%E6%96%87%E7%8C%AE" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==1; print('keyword 筛选 OK')"

echo "==> 7. 标签筛选"
curl -s "$BASE/resources?tag_id=$TAGID" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==1; print('tag 筛选 OK')"

echo "==> 8. 上传非法扩展名应 400"
echo "MZ" > /tmp/evil.exe
curl -s -X POST "$BASE/resources/upload" -H "$AUTH" -F "file=@/tmp/evil.exe;filename=evil.exe" -o /dev/null -w "bad ext: %{http_code}\n"

rm -f /tmp/evil.exe
echo ""
echo "SMOKE F7/F8 OK ✅"
