#!/usr/bin/env bash
# F7 资源库(link + file)冒烟验收脚本(幂等)
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

echo "==> 2. 建 link 资源(带标签/描述)"
curl -s -X POST "$BASE/resources" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"type\":\"link\",\"title\":\"冒烟链接\",\"url\":\"https://example.com\",\"description\":\"冒烟描述\",\"tag_ids\":[\"$TAGID\"]}" \
  -o /dev/null -w "create link: %{http_code}\n"

echo "==> 3. 非法协议应 400"
curl -s -X POST "$BASE/resources" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"type":"link","title":"x","url":"javascript:alert(1)"}' -o /dev/null -w "bad url: %{http_code}\n"

echo "==> 4. 上传 PDF 文件"
printf '%%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%%%EOF\n' > /tmp/smoke.pdf
UP=$(curl -s -X POST "$BASE/resources/upload" -H "$AUTH" \
  -F "file=@/tmp/smoke.pdf;filename=smoke.pdf" \
  -F "title=冒烟文档" -F "description=上传测试")
echo "$UP" | python3 -c "
import sys,json; d=json.load(sys.stdin)['resource']
print('upload:', d['type'], '|', d['title'], '| mime:', d['mime_type'], '| preview:', d['preview']['supported'])
assert d['type']=='file' and d['preview']['supported'], '文件上传/预览元信息异常'"
ls data/uploads/ | head -2
RID=$(echo "$UP" | python3 -c "import sys,json;print(json.load(sys.stdin)['resource']['id'])")
rm -f /tmp/smoke.pdf

echo "==> 5. 下载与预览"
curl -s -o /dev/null -w "download: %{http_code}\n" "$BASE/resources/$RID/download" -H "$AUTH"
curl -s -o /dev/null -w "preview: %{http_code}\n" "$BASE/resources/$RID/preview" -H "$AUTH"

echo "==> 6. 列表(共享可见,含上传者/标签/预览)"
curl -s "$BASE/resources" -H "$AUTH" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print('total:', d['pagination']['total'])
for r in d['resources']:
    print(' -', r['type'], '|', r['title'], '| uploader:', r['uploader']['display_name'], '| tags:', [t['name'] for t in r['tags']])
assert d['pagination']['total'] == 2, '应有 2 条资源(link + file)'
print('列表 OK')"

echo "==> 7. 筛选:type=link / keyword=冒烟"
curl -s "$BASE/resources?type=link" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==1 and d['resources'][0]['type']=='link'; print('type 筛选 OK')"
curl -s "$BASE/resources?keyword=%E5%86%92%E7%83%9F" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==2; print('keyword 筛选 OK')"

echo "==> 8. 标签筛选"
curl -s "$BASE/resources?tag_id=$TAGID" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert d['pagination']['total']==1; print('tag 筛选 OK')"

echo "==> 9. 上传边界:非法扩展名 / 内容不符 应 400"
echo "MZ" > /tmp/evil.exe
curl -s -X POST "$BASE/resources/upload" -H "$AUTH" -F "file=@/tmp/evil.exe;filename=evil.exe" -o /dev/null -w "bad ext: %{http_code}\n"
printf 'MZ\x90\x00' > /tmp/fake.pdf
curl -s -X POST "$BASE/resources/upload" -H "$AUTH" -F "file=@/tmp/fake.pdf;filename=fake.pdf" -o /dev/null -w "mime mismatch: %{http_code}\n"
rm -f /tmp/evil.exe /tmp/fake.pdf

echo ""
echo "SMOKE F7 OK ✅"
