# 功能规格:Casbin 统一权限决策(authz)

| 项 | 内容 |
|---|---|
| 状态 | ✅ 已实现并验收(2026-09-27;用户工单明确批准 Casbin 改造与测试后门范围) |
| 关联 PRD | §3.3 权限矩阵 |
| 关联契约 | 不改变 F1~F10 / Admin Console 对外接口与状态码 |
| 涉及表 | 不改 schema;策略在进程内加载,项目成员关系来自现有表 |

## 1. 背景与动机

现有权限语义分散在 `admin`、`finance`、`project`、`resource`、`document` 五个领域服务中,虽然都在 service 层执行,但新增动作容易出现规则漂移。本次引入 Casbin,把角色继承、资源动作和对象属性匹配收敛到一个 `Can(actorID, action, target)` 决策入口;同时保留仅供自动化测试使用的显式后门,避免生产环境存在隐式放行。

## 2. 行为需求

- 所有领域权限判断必须调用统一 `authz.Authorizer.Can(ctx, actorID, action, target)`,目标对象以属性传入(负责人、项目成员角色、任务指派、文档可见性等),禁止在 handler 中重新实现权限分支。
- Casbin 使用 RBAC 角色继承 + ABAC 对象属性匹配:
  - `admin`、`supervisor`、`student` 均继承 `authenticated`;
  - `admin` 独占管理后台;
  - `admin`/`supervisor` 可访问经费管理;
  - 资源上传者本人或 `admin` 可修改/删除;
  - 文档/评论仅作者可修改/删除;文档阅读遵循“公开或作者”,他人私有文档仍返回 404;
  - 项目 owner/member、任务 owner/assignee 的现有权限矩阵保持不变。
- 测试后门由环境变量 `AUTHZ_TEST_BYPASS=true` 显式开启,默认关闭;开启后 `Can` 对所有已认证 actor 返回允许。
- `APP_ENV=production` 与 `AUTHZ_TEST_BYPASS=true` 同时出现时,应用装配必须返回错误,禁止服务启动。
- 不新增权限管理 HTTP API,不新增数据库表;策略属于代码资产,评审时随代码 review。
- 新增依赖 `github.com/casbin/casbin/v2`,理由:成熟 RBAC + ABAC 策略引擎,避免继续维护自研权限分支;不引入 GORM adapter,本次策略是静态代码策略,无需持久化适配器。

## 3. 接口

- 对外 HTTP 接口、请求/响应字段、错误码均不变,因此不修改 `docs/api-contract.md`。
- 新增内部接口:`Can(ctx context.Context, actorID, action string, target authz.Target) (bool, error)`。

## 4. 验收标准(可测清单)

- [x] admin/supervisor/student 角色继承矩阵由 Casbin enforceable policy 决定;
- [x] 现有 admin、finance、project、resource、document/comment 权限行为不回归;
- [x] 项目 owner/member、任务 assignee/owner 的允许与拒绝矩阵有单元测试;
- [x] 文档公开/私有/作者读取矩阵有单元测试,他人私有继续返回 404;
- [x] 默认配置下测试后门关闭;
- [x] `AUTHZ_TEST_BYPASS=true` 时权限放行,可用于测试;
- [x] `APP_ENV=production` + `AUTHZ_TEST_BYPASS=true` 时启动装配失败;
- [x] 全量 `make check` 通过(2026-09-27,含 vet/fmt/test/lint/build)。

## 5. 边界与异常

- 用户不存在或角色查询失败时 `Can` 返回错误或拒绝,不泄露内部错误细节;
- 对象属性为空(如非项目成员、无 visibility)时默认拒绝,除非策略明确允许;
- 后门只影响授权判断,不绕过 JWT 身份认证;未登录请求仍为 401;
- 测试后门不得通过代码默认值或测试辅助函数在生产装配中隐式开启。

## 6. 测试计划

- 单元(authz):角色继承、资源/动作、owner/member/assignee/visibility 矩阵、后门开关、生产环境拒绝;
- 领域 service 回归:保留现有权限测试并补充 Casbin 决策路径;
- 集成:复用现有权限测试,验证对外状态码不变;增加后门开启场景的显式测试(使用独立配置)。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-09-27,依据用户明确工单范围)
- [x] 依赖新增理由已记录:casbin/v2 提供统一策略模型;不引入 adapter 与 schema 变更
- [x] 实现与全量门禁完成后复核(2026-09-27:`make check` 全绿,集成测试 16.5s 通过)
