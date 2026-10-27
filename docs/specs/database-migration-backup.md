# 功能规格:版本化数据库迁移与备份恢复(A2)

| 项 | 内容 |
|---|---|
| 状态 | 已实现并验收(2026-10-15) |
| 关联 PRD | 无(后端稳定化) |
| 关联契约 | 不涉及 HTTP API |
| 涉及表 | `goose_db_version`(迁移元数据);基线包含现有全部业务表 |

## 1. 背景与动机

开发期一直使用 GORM `AutoMigrate`。它没有版本记录、无法可靠回滚,也不能证明某个部署环境的 schema 与 `docs/schema.sql` 一致。与此同时,PostgreSQL 数据已开始承载真实试用数据,上传文件与数据库之间也需要一套可校验、可恢复的备份流程,否则演示环境和服务器部署无法安全升级。

## 2. 行为需求

- 使用 `goose` 管理 PostgreSQL 版本化迁移,迁移文件放在仓库 `migrations/`。
- 首次迁移基线严格采用 `docs/schema.sql` 中阶段 1~3 的现有结构,不新增业务字段。
- 新空库执行 `make migrate-up` 后得到完整 schema,并写入内置任务类型种子。
- 已由 `AutoMigrate` 建表的开发库可以通过一次显式 `make migrate-baseline` 标记基线,随后继续执行增量迁移;命令不得清空业务数据。
- 支持 `make migrate-up`、`make migrate-down`、`make migrate-status` 和 `make migrate-baseline`。
- 生产环境启动服务时**不自动迁移**,部署流程必须先执行 `make migrate-up`;development/test 可在服务启动前自动执行迁移,保持本地开发便利。
- `make backup` 生成一个带时间戳的 `.tar.gz` 备份包,同时包含 PostgreSQL custom-format dump、`data/uploads/` 和校验清单。
- `make restore FILE=<备份包> CONFIRM=yes` 在校验备份完整后恢复数据库与上传目录;未显式确认时拒绝执行。
- 恢复前保存当前上传目录;数据库恢复失败时回滚上传目录,避免只恢复一半。
- 备份包包含格式版本、创建时间、数据库名、两个内容文件的 SHA-256 校验值。

## 3. 接口

不修改 HTTP API 契约。新增运维命令:

| 命令 | 行为 |
|---|---|
| `make migrate-up` | 应用全部待执行迁移 |
| `make migrate-down` | 回滚最近一个迁移版本 |
| `make migrate-status` | 显示每个迁移的执行状态 |
| `make migrate-baseline` | 仅对已有 AutoMigrate 库标记基线到版本 1,再执行后续迁移;空库直接执行全部迁移 |
| `make backup` | 生成 `backups/labnexus-YYYYmmdd-HHMMSS.tar.gz` |
| `make restore FILE=... CONFIRM=yes` | 恢复指定备份包 |

## 4. 验收标准(可测清单)

- [x] `make migrate-up` 可在全新数据库上创建全部业务表,并写入 5 个内置任务类型。
- [x] 重复执行 `make migrate-up` 不改数据且成功退出。
- [x] `make migrate-status` 能显示基线版本和种子版本均为 applied。
- [x] 对已有表但没有 goose 版本表的数据库,`make migrate-baseline` 保留数据并标记版本 1,再补齐版本 2。
- [x] 对空数据库,`make migrate-baseline` 等价于 `make migrate-up`。
- [x] `make migrate-down` 可回滚最近一个迁移;再次 `make migrate-up` 恢复。
- [x] production 配置下 `cmd/server` 不执行迁移(代码路径:仅打印提示,不调用迁移)。
- [x] 备份包可重复生成且包含 `database.dump`、`uploads.tar.gz`、`MANIFEST.sha256`。
- [x] 备份包被修改或文件缺失时,恢复命令在写入数据库前失败。
- [x] 使用备份恢复临时数据库和上传目录后,标记数据、表结构与文件内容均与备份一致。
- [x] `make check`、`make test-integration` 全绿(2026-10-15,含 `TestBackupRestoreLifecycle`)。

## 5. 边界与异常

- Docker/PostgreSQL 未启动、`pg_dump`/`pg_restore` 不存在时给出明确错误,不创建半成品备份包。
- `migrate-baseline` 仅允许在 goose 版本表为空时使用;如果已有迁移记录,返回错误,避免误跳过迁移。
- 已有库基线标记前检查核心业务表是否存在;完全空库走正常 `up`。
- `restore` 默认拒绝执行,必须 `CONFIRM=yes`;备份校验失败或数据库恢复失败均返回非 0。
- 上传目录不存在时备份空目录,恢复时创建空目录。
- 数据库名、容器名和目录允许通过环境变量覆盖,便于隔离集成测试。
- 不在 production 中自动执行迁移,避免多实例同时迁移和隐式升级。

## 6. 测试计划

- 单元:迁移文件注册、版本解析、已有库/空库判断、production 自动迁移策略。
- 集成(真实 PostgreSQL):全新库 `up → up → status → down → up`;已有 AutoMigrate 结构 `baseline`;备份 → 修改数据与文件 → 恢复 → 校验。
- 脚本级 smoke:校验命令参数、损坏备份拒绝、未确认恢复拒绝。
- 回归:`make check`;`make test-integration`。

## 7. 依赖决策

- 新增 `github.com/pressly/goose/v3`:项目规范和 roadmap 已明确选择 goose,用于 SQL 版本、状态与回滚,不自行实现易错的迁移框架。
- 备份恢复使用 PostgreSQL 官方 `pg_dump`/`pg_restore` custom format,不引入额外备份服务。

## 8. 评审记录

- [x] §1.3 checklist 通过(用户 2026-10-15 明确要求完成 A2)
- [x] 不修改 `docs/schema.sql` 权威结构;基线迁移直接复制定稿 schema。
