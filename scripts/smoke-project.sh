#!/usr/bin/env bash
# F9 项目/任务 冒烟验收脚本(幂等)
# 前置:docker compose up -d 且后端已在 :8080 运行
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"

echo "==> 0. 清理冒烟数据(幂等)"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  DELETE FROM task_links WHERE task_id IN (SELECT id FROM tasks WHERE project_id IN (SELECT id FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%')));
  DELETE FROM tasks WHERE project_id IN (SELECT id FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%'));
  DELETE FROM milestones WHERE project_id IN (SELECT id FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%'));
  DELETE FROM project_members WHERE project_id IN (SELECT id FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%'));
  DELETE FROM projects WHERE owner_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%');
  DELETE FROM spaces WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'smoke_pj%');
  DELETE FROM users WHERE username LIKE 'smoke_pj%';
  DELETE FROM invite_codes WHERE code IN ('SMOKE-006','SMOKE-007');" >/dev/null
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-006', '00000000-0000-0000-0000-000000000000'), (gen_random_uuid(), 'SMOKE-007', '00000000-0000-0000-0000-000000000000');" >/dev/null

reg() { # $1=invite $2=user
  curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
    -d "{\"invite_code\":\"$1\",\"username\":\"$2\",\"display_name\":\"$2\",\"password\":\"password123\"}" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])"
}
TO=$(reg SMOKE-006 smoke_pj_owner)
TM=$(reg SMOKE-007 smoke_pj_member)
AH="Authorization: Bearer $TO"; MH="Authorization: Bearer $TM"

echo "==> 1. 创建项目"
PID=$(curl -s -X POST "$BASE/projects" -H "$AH" -H 'Content-Type: application/json' \
  -d '{"name":"冒烟论文项目","description":"演示"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['project']['id'])")
echo "project=$PID"

echo "==> 2. 添加成员"
MID=$(curl -s "$BASE/me" -H "$MH" | python3 -c "import sys,json;print(json.load(sys.stdin)['user']['id'])")
curl -s -X POST "$BASE/projects/$PID/members" -H "$AH" -H 'Content-Type: application/json' \
  -d "{\"user_id\":\"$MID\"}" -o /dev/null -w "add member: %{http_code}\n"

echo "==> 3. 创建里程碑"
curl -s -X POST "$BASE/projects/$PID/milestones" -H "$AH" -H 'Content-Type: application/json' \
  -d '{"name":"初稿完成","due_date":"2026-09-01"}' -o /dev/null -w "milestone: %{http_code}\n"

echo "==> 4. 创建任务(指派给成员)"
TID=$(curl -s -X POST "$BASE/projects/$PID/tasks" -H "$AH" -H 'Content-Type: application/json' \
  -d "{\"title\":\"写综述\",\"assignee_id\":\"$MID\",\"priority\":\"high\"}" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['task']['id'])")
echo "task=$TID"

echo "==> 5. 状态机流转:todo→in_progress→done"
curl -s -X POST "$BASE/tasks/$TID/transition" -H "$MH" -H 'Content-Type: application/json' \
  -d '{"status":"in_progress"}' -o /dev/null -w "→in_progress: %{http_code}\n"
curl -s -X POST "$BASE/tasks/$TID/transition" -H "$AH" -H 'Content-Type: application/json' \
  -d '{"status":"done"}' -o /dev/null -w "→done: %{http_code}\n"

echo "==> 6. done 后非法迁移应 400"
curl -s -X POST "$BASE/tasks/$TID/transition" -H "$AH" -H 'Content-Type: application/json' \
  -d '{"status":"todo"}' -o /dev/null -w "illegal transition: %{http_code}\n"

echo "==> 7. 任务列表筛选 status=done"
curl -s "$BASE/projects/$PID/tasks?status=done" -H "$MH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
assert len(d['tasks'])==1 and d['tasks'][0]['status']=='done'; print('筛选 OK')"

echo "==> 8. 项目详情(成员可见)"
curl -s "$BASE/projects/$PID" -H "$MH" | python3 -c "
import sys,json; d=json.load(sys.stdin)['project']
print('members:', len(d['members']), '| milestones:', len(d['milestones']), '| tasks:', len(d['tasks']))
assert len(d['tasks'])==1; print('详情 OK')"

echo "==> 9. 删除任务(owner)"
curl -s -X DELETE "$BASE/tasks/$TID" -H "$AH" -o /dev/null -w "delete task: %{http_code}\n"

echo ""
echo "SMOKE F9 OK ✅"
