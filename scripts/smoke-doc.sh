#!/usr/bin/env bash
# F3/F4/F5 文档/信息流/标签 冒烟验收脚本(幂等)
# 前置:docker compose up -d 且后端已在 :8080 运行
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"
J="$(mktemp)"

echo "==> 0. 清理冒烟数据(幂等)"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  DELETE FROM comments WHERE document_id IN (SELECT id FROM documents WHERE author_id IN (SELECT id FROM users WHERE username LIKE 'smoke_doc%'));
  DELETE FROM reactions WHERE document_id IN (SELECT id FROM documents WHERE author_id IN (SELECT id FROM users WHERE username LIKE 'smoke_doc%'));
  DELETE FROM document_tags WHERE document_id IN (SELECT id FROM documents WHERE author_id IN (SELECT id FROM users WHERE username LIKE 'smoke_doc%'));
  DELETE FROM documents WHERE author_id IN (SELECT id FROM users WHERE username LIKE 'smoke_doc%');
  DELETE FROM folders WHERE space_id IN (SELECT s.id FROM spaces s JOIN users u ON s.user_id=u.id WHERE u.username LIKE 'smoke_doc%');
  DELETE FROM spaces WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'smoke_doc%');
  DELETE FROM users WHERE username LIKE 'smoke_doc%';
  DELETE FROM invite_codes WHERE code IN ('SMOKE-003','SMOKE-004');
  DELETE FROM tags WHERE name IN ('冒烟标签');" >/dev/null
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-003', '00000000-0000-0000-0000-000000000000'), (gen_random_uuid(), 'SMOKE-004', '00000000-0000-0000-0000-000000000000');" >/dev/null

reg() { # $1=invite $2=user $3=display
  curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
    -d "{\"invite_code\":\"$1\",\"username\":\"$2\",\"display_name\":\"$3\",\"password\":\"password123\"}"
}
TOKA=$(reg SMOKE-003 smoke_doc_a "冒烟A" | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
TOKB=$(reg SMOKE-004 smoke_doc_b "冒烟B" | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
[ -n "$TOKA" ] && [ -n "$TOKB" ] || { echo "FAIL: 注册失败"; exit 1; }
AH="Authorization: Bearer $TOKA"; BH="Authorization: Bearer $TOKB"

echo "==> 1. A 创建标签「冒烟标签」"
TAG=$(curl -s -X POST "$BASE/tags" -H "$AH" -H 'Content-Type: application/json' -d '{"name":"冒烟标签"}')
TAGID=$(echo "$TAG" | python3 -c "import sys,json;print(json.load(sys.stdin)['tag']['id'])")
echo "tag=$TAGID"

echo "==> 2. A 创建公开文档(打标签)"
DOC=$(curl -s -X POST "$BASE/me/documents" -H "$AH" -H 'Content-Type: application/json' \
  -d "{\"title\":\"冒烟公开帖\",\"content\":\"## 正文\",\"visibility\":\"public\",\"tag_ids\":[\"$TAGID\"]}")
DOCID=$(echo "$DOC" | python3 -c "import sys,json;print(json.load(sys.stdin)['document']['id'])")
echo "doc=$DOCID"

echo "==> 3. A 创建私有文档"
PRIV=$(curl -s -X POST "$BASE/me/documents" -H "$AH" -H 'Content-Type: application/json' \
  -d '{"title":"冒烟私有笔记","visibility":"private"}')
PRIVID=$(echo "$PRIV" | python3 -c "import sys,json;print(json.load(sys.stdin)['document']['id'])")

echo "==> 4. B 看 A 私有文档应 404"
curl -s -H "$BH" "$BASE/documents/$PRIVID" -o /dev/null -w "status: %{http_code}\n"

echo "==> 5. B 看 A 公开文档应 200"
curl -s -H "$BH" "$BASE/documents/$DOCID" -o /dev/null -w "status: %{http_code}\n"

echo "==> 6. B 点赞 + 评论公开帖"
curl -s -X POST "$BASE/documents/$DOCID/reactions" -H "$BH" -H 'Content-Type: application/json' -d '{"emoji":"👍"}' -o /dev/null -w "reaction: %{http_code}\n"
curl -s -X POST "$BASE/documents/$DOCID/comments" -H "$BH" -H 'Content-Type: application/json' -d '{"content":"好帖!"}' -o /dev/null -w "comment: %{http_code}\n"

echo "==> 7. A 看 feed(应含公开帖 + 计数,不含私有笔记)"
curl -s -H "$AH" "$BASE/feed" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print('total:', d['pagination']['total'])
for doc in d['documents']:
    print(' -', doc['title'], '| reactions:', doc['reactions_count'], '| comments:', doc['comments_count'])
assert d['pagination']['total'] >= 1
assert all(x['title'] != '冒烟私有笔记' for x in d['documents']), '私有文档不应出现在 feed'
assert any(x['reactions_count'] >= 1 for x in d['documents']), '点赞计数缺失'
print('feed 校验 OK')"

echo "==> 8. 标签内容页(应含公开帖)"
curl -s -H "$BH" "$BASE/tags/$TAGID/contents" | python3 -c "
import sys,json
d=json.load(sys.stdin)
titles=[x['title'] for x in d['documents']]
print('contents:', titles)
assert '冒烟公开帖' in titles, '标签内容页应含公开帖'
print('tag contents OK')"

echo "==> 9. A 删除自己的文档"
curl -s -X DELETE -H "$AH" "$BASE/documents/$DOCID" -o /dev/null -w "delete: %{http_code}\n"
curl -s -H "$BH" "$BASE/documents/$DOCID" -o /dev/null -w "after delete get: %{http_code}\n"

rm -f "$J"
echo ""
echo "SMOKE F3/F4/F5 OK ✅"
