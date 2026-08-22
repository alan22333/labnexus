# LabNexus 开发规范 v1.0

> 本文件是 LabNexus 开发的**强制规范**(harness)。开发前必读;与 PRD、API 契约、schema 冲突时,以本文件的流程为准,以 PRD 的产品决策为准。
> 适用:单人 + 全程 AI Coding + Go/Gin/GORM/Postgres/Redis,前端阶段 1 为纯 HTML。

---

## 0. 范式总览:SDD + TDD

两个范式组合,各管一段:

| 范式 | 回答的问题 | 产物 |
|---|---|---|
| **SDD**(Spec-Driven Development,规格驱动) | 做什么、怎么验收 | `docs/specs/<feature>.md` 规格 + API 契约 |
| **TDD**(Test-Driven Development,测试驱动) | 实现是否正确 | 测试代码(红 → 绿 → 重构) |

**关系**:SDD 定义"行为契约",TDD 保证"实现满足契约"。AI 生成代码时,规格是 AI 的"需求输入",测试是 AI 的"正确性反馈"。

**每个功能的完整生命周期(必须按序)**:

```
1. Spec     写规格(docs/specs/xx.md)→ 人工评审通过
2. Contract 涉及接口 → 先改 docs/api-contract.md(契约先行)
3. RED      按规格写测试 → go test 失败(证明测试有效)
4. GREEN    写最小实现 → go test 全绿
5. REFACTOR 重构 → 测试保持全绿
6. Review   人工 review + make check
7. Merge    合并到 main
```

> **没有规格不写代码;没有测试不提交。** 这是唯一一条铁律。

---

## 1. SDD:规格驱动

### 1.1 规格文件

- 每个功能一个规格:`docs/specs/<feature>.md`,模板见 `docs/specs/_template.md`;
- 规格必含:背景与动机、行为需求(条目式/GWT)、接口(引用契约)、**验收标准(可测清单)**、边界与异常、测试计划;
- 状态机:`草稿 → 已评审 → 已实现`(评审通过前禁止进入编码)。

### 1.2 契约先行

- 任何对外接口(HTTP 端点、错误码、数据字段)变更,**必须先改 `docs/api-contract.md`**,再改代码;
- 契约是公共资产:AI 生成代码不得绕过契约直接造接口;前端按契约开发。

### 1.3 规格评审(checklist)

- [ ] 验收标准是否**可测**(每条能对应一个测试或一次手工验证)?
- [ ] 权限语义是否明确(谁可以做什么,引用 PRD §3.3)?
- [ ] 边界与异常是否覆盖(空态、越权、非法输入、并发)?
- [ ] 是否引用了正确的契约端点与 schema 字段?

---

## 2. TDD:测试先行

### 2.1 测试层级(金字塔)

| 层级 | 对象 | 依赖 | 占比 |
|---|---|---|---|
| 单元测试 | service 层(业务规则、状态机、权限判定) | 无外部服务(repository 用替身或内存实现) | 大头 |
| handler 测试 | handler 层(参数解析、状态码、错误格式) | `net/http/httptest` + mock service | 中 |
| 集成测试 | repository 层(真实 SQL 语义) | 容器 Postgres(可选,阶段 2 引入) | 少 |

### 2.2 编写约定

- 文件:`<name>_test.go` 与被测文件同包;
- 表驱动测试(Table-driven),子场景用 `t.Run`;
- 断言库:**testify**(`require` 用于前置断言,`assert` 用于结果断言);
- 测试数据:包内 `testdata` 工厂函数,禁止依赖真实用户数据;
- **覆盖率**:service 层 ≥ 80%;新增代码不允许降低整体覆盖率(CI 检查)。

### 2.3 红绿纪律

- 先写测试并**看到它失败**(红),再写实现(绿)——防止"测试永远通过"的假测试;
- 重构后必须全绿;禁止 `t.Skip` 掩饰失败(临时调试除外,提交前必须删除)。

---

## 3. 目录与分层

```
internal/
├── <domain>/               # 一个领域一个包:user / space / document / tag / resource / project / admin
│   ├── handler.go          # HTTP 层:参数解析、调用 service、组装响应
│   ├── service.go          # 业务逻辑(状态机、权限、事务边界)
│   ├── repository.go       # 数据访问(GORM)
│   └── model.go            # 该域的数据结构(可选)
├── middleware/             # 认证、can() 权限中间件
├── config/                 # 配置
└── database/               # 连接与迁移
```

**依赖方向(强制)**:
- `handler → service → repository`,禁止反向;
- 跨模块调用**只允许在 service 层**(如 document.service 调用 tag.service);handler 与 repository 禁止跨模块;
- 模块间通过构造函数注入依赖(`NewDocumentService(tagSvc, db)`),禁止全局变量与 `init()` 副作用;
- 权限判断**只允许**通过 `can(user, action, target)`(PRD §3.3),禁止 handler 里散落硬编码判断。

---

## 4. Go 代码规范

- **格式化**:`gofmt`(提交前必须 `gofmt -l .` 为空),import 分组:标准库 / 第三方 / 本模块;
- **错误处理**:
  - 定义哨兵错误:`var ErrNotFound = errors.New("not found")`,service 层返回域错误;
  - handler 层统一映射 HTTP(见 API 契约错误码),禁止 handler 直接返回裸 error 文本;
  - 错误必须被处理或显式传播,禁止 `_ =` 吞错;
- **日志**:标准库 `log/slog`;业务日志在 service 层,访问日志由 Gin 中间件;禁止 `fmt.Println`;
- **禁止**:`panic`(main 之外)、`os.Exit`(main 之外)、未使用的全局可变状态;
- **依赖注入**:构造函数 + 显式参数,不搞反射容器(本项目不需要)。

---

## 5. 数据库规范

- **权威定义**:`docs/schema.sql`;改表必须同时改 schema.sql 与 GORM 模型;
- **开发期**:GORM `AutoMigrate`;**部署前**:切 goose 版本化迁移(基线 = schema.sql);
- **GORM 约束**:
  - 关联查询一律 `Preload`,禁止 N+1;
  - 软删除统一 `DeletedAt`(用户、文档、任务);
  - 时间字段 `TIMESTAMPTZ`;JSON 字段用 `datatypes.JSON`(JSONB);
  - 写操作必须在事务中执行(service 层 `db.Transaction`);
- **索引**:每个查询路径(契约中的列表/检索端点)必须有对应索引,新增查询需评估索引。

---

## 6. Git 与分支

- **分支**:`main` 为主干;功能分支 `feature/<slug>`(如 `feature/f1-auth`);修复分支 `fix/<slug>`;
- **提交信息**:Conventional Commits:`feat|fix|docs|test|refactor|chore(<scope>): 描述`(如 `feat(auth): implement invite-code registration`);
- **提交粒度**:一个逻辑变更一个提交;测试与实现同提交(提交时必须是绿的);
- **合并**:功能完成 + `make check` 全绿后合并;合并前 `git rebase main` 保持线性历史;
- **禁止**:直接向 main 提交功能代码(骨架基线等一次性初始化除外)。

---

## 7. AI Coding 工作流(全程 AI Coding,强制)

### 7.1 每个功能的 AI 循环

```
① 人工/AI 起草规格 → ② 人工评审规格(过 §1.3 checklist)
→ ③ AI 生成测试(读规格)→ 人工确认测试覆盖验收标准 → 跑出 RED
→ ④ AI 生成实现(读规格 + 测试 + 契约 + 相关模块)→ 跑出 GREEN
→ ⑤ 人工 review + make check → ⑥ 提交
```

### 7.2 Prompt 规范(给 AI 的输入必须包含)

- 规格文件路径(必读);
- 相关契约端点、schema 表、相关模块代码路径;
- 明确边界:"只修改/新增这些文件,不改动 xxx";
- 要求:实现后运行 `go test ./...` 并汇报结果。

### 7.3 AI 产物红线(禁止项)

- ❌ 跳过测试直接写实现;
- ❌ 修改 `docs/schema.sql` / `docs/api-contract.md` / `docs/standards.md` 而不经人工明确 review;
- ❌ 引入新依赖而不说明理由(新增依赖需写入规格或提交信息);
- ❌ 生成重复/死代码、绕过分层(如 handler 直连 repository);
- ❌ 假装测试通过(不运行、不报告真实结果)。

### 7.4 完成定义(Definition of Done)

- [ ] 规格验收清单全部通过(可测项对应测试或手工验证记录)
- [ ] `make check` 全绿(vet + fmt + test + lint + build)
- [ ] 覆盖率要求满足(service ≥ 80%)
- [ ] 提交信息符合 Conventional Commits
- [ ] 契约与 schema 文档与实际实现一致

---

## 8. 工具链(Makefile)

```bash
make up        # docker compose up -d(Postgres + Redis)
make down      # docker compose down
make run       # go run ./cmd/server
make build     # go build ./...
make test      # go test ./... -cover
make lint      # golangci-lint run ./...
make check     # 全量检查:vet + fmt + test + lint + build(提交前必跑)
```

lint 配置见 `.golangci.yml`;`make check` 等价于 `scripts/check.sh`。

---

## 9. 安全基线(贯穿所有功能)

- 密码:**bcrypt**(`golang.org/x/crypto/bcrypt`),禁止明文/自研哈希;
- JWT:`JWT_SECRET` 生产环境必须由环境变量注入,禁止默认密钥上线;
- SQL 注入:全部走 GORM 参数化,禁止字符串拼接 SQL;
- 越权:任何资源访问(文档/任务/项目/资源)必须过 `can()`;
- 私有内容:公开接口不得泄露私有文档内容/字段;列表接口对私有内容一律过滤;
- 文件上传:校验类型与大小(白名单 + 上限),存储路径使用随机文件名。

---

## 10. 规范变更

- 本规范允许演进:变更通过修改本文件 + 提交信息注明 `docs(standards)` 实现;
- 与 AI 产物冲突时:**先改规范/规格,再让 AI 改代码**,禁止让 AI 绕过规范"偷跑"。
