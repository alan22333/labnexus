# 功能规格:项目与任务(F9)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.3 F9 |
| 关联契约 | api-contract.md §F9 |
| 涉及表 | projects, project_members, milestones, tasks, task_links(新增) |

## 1. 背景与动机

**解决"拖延"的监督模块**:项目/任务看板 + 状态机 + 里程碑,进度共享与自我监督。负责人(owner)派任务、全员可见进度。

## 2. 行为需求

- **项目**:任意登录用户创建(创建者即 owner,自动成为成员);列表仅返回本人参与的项目;详情(成员可见)返回 project + members + milestones + tasks;修改仅 owner;
- **成员管理**(owner):添加/移除成员;**不可移除 owner 自己**;
- **里程碑**(owner 创建/修改):name + due_date + completed_at;项目成员可见;
- **任务**(项目成员创建):title/description/assignee/priority(high|medium|low)/due_date(YYYY-MM-DD)/milestone;**assignee 必须是项目成员**;可关联文档/资源(task_links);
- **任务列表**(成员):按 status / assignee_id / milestone_id 筛选;
- **任务修改**(owner 或 assignee):字段更新;
- **状态机**(owner 或 assignee 发起,service 校验):
  `todo → in_progress`;`in_progress → blocked | done`;`blocked → todo | in_progress`;`done` 为终态;
- **任务删除**:仅 owner。

## 3. 接口

契约 §F9:projects CRUD、members、milestones、tasks 全部端点。

## 4. 验收标准(可测清单)

- [ ] 创建项目:201;创建者自动成为 owner 成员;name 空 → 400
- [ ] 列表:仅本人参与的项目(他人项目不出现)
- [ ] 详情:成员 200(含 project/members/milestones/tasks);非成员 → 403
- [ ] 修改项目:owner 200;非 owner 成员 → 403
- [ ] 添加成员:owner 201;重复添加 → 400(或幂等,规格选:重复添加返回 400);非 owner → 403
- [ ] 移除成员:owner 204;移除 owner 自己 → 400;非 owner → 403
- [ ] 里程碑:owner 创建 201 / 修改 200;成员可见;非 owner 创建 → 403
- [ ] 任务创建:成员 201;assignee 非项目成员 → 400;非法 priority → 400;非法 due_date 格式 → 400;非法 status 初始值 → 400
- [ ] 任务创建:link_document_id / link_resource_id 写入 task_links(详情返回 links)
- [ ] 任务列表:status / assignee / milestone 筛选正确
- [ ] 任务修改:owner 或 assignee 200;其他成员 → 403
- [ ] 状态机:todo→in_progress ✓;in_progress→blocked ✓;blocked→todo ✓;in_progress→done ✓;done→in_progress → 400;todo→done → 400;非法状态值 → 400
- [ ] 任务删除:owner 204;assignee/其他成员 → 403;软删除后列表不出现
- [ ] 未登录 → 401

## 5. 边界与异常

- 状态机校验在 service 层(单一权威),handler 只传参;
- 任务软删除(DeletedAt);项目/成员/里程碑物理删除;
- due_date 统一 YYYY-MM-DD,非法 → 400;
- 关联目标(document/resource)仅存 ID,存在性由引用方保证(MVP 不强制外键)。

## 6. 测试计划

- 单元(service):权限矩阵(owner/成员/非成员)、状态机全迁移表、筛选、校验;
- handler:错误码断言;
- 集成:项目全流程 + 越权(真实 DB)。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
- [x] 已实现并验收:单元/handler 测试全绿,集成测试(project_test.go)+ 端到端冒烟通过(scripts/smoke-project.sh),`make check` 全绿(2026-08-22)
- [x] 深度接口测试:数据隔离/越权矩阵/状态机全表/边界/软删除/契约结构(project_deep_test.go)

## 8. v2 体验优化(2026-09,对应正式 React 前端)

> 动因:项目模块被反馈"太粗糙"。React 正式前端接管后,补齐三块能力:
> **任务类型字典**(可后台管理,管理界面随管理后台轮次落地)、**项目进度统计**、**加成员候选下拉**。

### 8.1 行为需求

1. **任务类型字典**:
   - 新增 `task_types` 表(课题组级全局字典):name 唯一、color、sort、builtin、active;
   - 内置种子类型(实验/文献/写作/代码/其他,builtin=true),幂等写入(app.Build 后与集成测试重建时);
   - `GET /api/task-types`(🔐 登录即可)返回 active 类型,用于任务表单下拉;
   - 任务新增 `type_id`(可空,指向 task_types);创建/编辑任务可带 `type_id`,须存在且 active,否则 400;
   - 任务 JSON 携带嵌套 `type:{id,name,color}`,看板卡片显示类型标签;
   - 类型的增删改(后台管理)不在本规格范围 —— 归入「管理后台」功能轮次。
2. **项目进度统计**:
   - 项目对象(列表 + 详情)新增 `stats`:`{total, todo, in_progress, blocked, done, overdue}`;
   - 统计口径:未删除任务;`overdue` = 状态非 done 且 due_date < 今天(SQL CURRENT_DATE);
   - 列表接口一次聚合查询,不 N+1。
3. **加成员候选**:
   - `GET /projects/:id/candidates`(仅 owner):返回**未入组**用户列表(按 display_name),前端加成员弹窗改成下拉选择,不再手填 UUID;
   - 非 owner(成员/局外人)→ 403。

### 8.2 接口变化

契约 §F9 追加:
`GET /api/task-types`、`GET /projects/:id/candidates`;任务创建/编辑请求与响应增加 type 相关字段;
项目响应增加 `stats`。schema.sql 增加 task_types 表与 tasks.type_id。

### 8.3 验收标准(可测清单)

- [ ] GET /api/task-types:登录 200 含内置类型;未登录 401
- [ ] 建任务带合法 type_id:201 且响应含 type 对象;带不存在/非 active type_id → 400
- [ ] 任务详情/列表含 type 对象(设置了 type_id 时)
- [ ] 项目列表与详情均含 stats;计数与 DB 一致;过期任务计入 overdue
- [ ] candidates:owner 200 且不含已有成员/自己;成员、局外人 → 403
- [ ] 上述全部有集成测试覆盖;`make check` 全绿
