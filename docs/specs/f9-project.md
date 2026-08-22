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
