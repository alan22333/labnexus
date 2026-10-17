# 功能规格:开发/演示种子数据(dev-demo-data)

| 项 | 内容 |
|---|---|
| 状态 | 已实现 |
| 关联 PRD | 无(开发体验/人工验收支撑,非产品功能) |
| 关联契约 | 不改契约(纯数据写入,不新增/变更 HTTP 端点) |
| 涉及表 | users, spaces, folders, documents, comments, reactions, tags, document_tags, resources, resource_tags, projects, project_members, milestones, task_types, tasks, task_links, turnover_batches, participants, turnover_items, turnover_submissions, accounts, transactions, invite_codes |

## 1. 背景与动机

集成测试(`test/integration`)在真实库上 `TRUNCATE` 全部业务表,导致本地开发库被清空;
清空后只剩零散账号,前端/人工验收缺少可用数据,重启后也看不到高保真内容。

本规格定义**一键恢复高保真演示数据**的开发工具:
- `make seed-demo`:确定性重建整套演示数据(可反复执行,结果一致);
- 开发环境启动(`make run`)时,若库中**无任何业务用户**,自动补种演示数据,保证重启后即可登录体验。

这不是产品功能,不新增对外接口,不改 schema/契约,不改动 `docs/standards.md`。

## 2. 行为需求

- **确定性**:相同版本代码重复执行,产生的账号、内容、数量、关系一致(时间类字段按"当前日期 ± 偏移"生成,保证任务/里程碑始终有过去/未来样例)。
- **覆盖全功能**:一次种子覆盖 F1~F10 与 admin:
  - 账号:admin / supervisor / student ×4,固定密码;
  - 社区:公开帖子(含置顶)、私密笔记、目录树、标签、评论(含回复)、点赞;
  - 资源:link 类型多条 + file 类型(落盘 `data/uploads/`,可下载/预览)、资源标签;
  - 演示图片:运行时用标准库生成 2 张 PNG(采样对比曲线、CIFAR-10 样例子图网格)落盘 `data/uploads/`,
    作为图片类 file 资源入库(可预览/下载),并嵌入对应帖子的正文(信息流中直接可见图片);
  - 项目:≥2 个项目、成员(演示管理员为「扩散模型采样加速」成员,保证 admin 登录后 /projects 有内容)、里程碑、任务(覆盖 todo/in_progress/blocked/done 与 high/medium/low,含逾期任务)、任务关联(task_links);
  - 经费:资金账户、≥2 个周转批次、参与同学、发放明细、上交记录、资金流水;
  - 管理:内置任务类型字典。
- **幂等/可重置**:执行时先清空全部业务表再写入固定数据;重复执行结果一致,不产生重复行。
- **开发环境空库自动补种**:`APP_ENV != production` 且 `users` 表为空时启动自动补种;`production` 或库非空时不触发。
- **不影响测试**:自动补种只发生在 `cmd/server` 启动路径,不影响单元/handler 测试;集成测试使用独立 `labnexus_test` 数据库并清空该库,不会碰开发库 `labnexus`。

## 3. 接口

- 不新增 HTTP 端点;
- 开发者入口:`make seed-demo`(等价 `go run ./cmd/seed-demo`);
- 自动补种开关:环境变量 `DEMO_SEED`(默认 `true`;`production` 恒不启用)。

## 4. 验收标准(可测清单)

- [ ] `make seed-demo` 在干净库执行成功,退出码 0,并打印账号与数据量摘要;
- [ ] 重复执行两次,关键表行数一致(users/documents/projects/tasks/resources/finance);
- [ ] 种子后 `test_admin / Test@123456` 与 `supervisor / Demo@123456` 均可登录(200);
- [ ] 种子后至少存在 1 个公开帖、1 个私密笔记、1 个 link 资源、1 个 file 资源、2 个项目、覆盖 4 种任务状态;
- [ ] file 资源在 `data/uploads/` 有对应文件,可通过资源下载端点取回;
- [ ] 至少存在 1 个图片类 file 资源(`preview.type = "image"`),预览端点返回 200 且 `Content-Type` 为图片;
- [ ] 至少 1 篇公开帖正文内嵌图片(`![...](/api/resources/<id>/preview)`),信息流卡片可直接看到图片;
- [ ] 演示管理员 `test_admin` 至少属于 1 个项目,登录后 `/projects` 非空;
- [ ] `users` 为空时,开发环境启动自动补种(集成/单元测试不受影响);
- [ ] `production` 环境即使库为空也不自动补种。

## 5. 边界与异常

- 数据库不可用:命令退出非 0 并给出提示(复用 config 默认连接参数);
- `data/uploads/` 不存在:自动创建(file 资源需要);
- 破坏性:命令会清空业务表(设计如此,仅用于开发/演示)。文档需显著标注;
- 并发/重复执行:通过"先清空、后写入 + 固定用户名/唯一键"保证幂等。

## 6. 测试计划

- 单元:数据集定义纯函数(账号/标签/任务状态覆盖)可断言,不依赖 DB;
- 集成(需 `make up`):对测试库执行 `Seed`,断言关键表行数、登录成功、重复执行行数不变;
- 手工:清库 → `make seed-demo` → 浏览器登录体验。

## 7. 评审记录

- 2026-10-16:由本地开发体验问题(库被集成测试清空、缺少高保真数据)驱动新增;纯开发工具,不改契约/schema/规范。
- 2026-10-17:补充演示图片(信息流 + 资源库图片展示效果),图片由标准库运行时生成,不新增依赖。
- 2026-10-17(二):演示管理员加入「扩散模型采样加速」成员,修复 admin 登录 `/projects` 空态导致无法演示项目页的问题(纯演示数据调整,不改契约)。
