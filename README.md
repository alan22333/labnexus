# LabNexus

课题组内部社区平台:科研朋友圈 + 知识库 + 进度监督。

- **产品定位**:见 [`docs/product-context.md`](docs/product-context.md)(自包含摘要;完整 PRD 在上级目录 `research-group-app/PRD.md`)
- **AI 接手入口**:见 [`AGENTS.md`](AGENTS.md)(Codex/Claude Code 等工具会自动读取;新接手方先读它)
- **技术栈**:Go + Gin + GORM + Postgres + Redis / React + Vite + Tailwind(阶段 1 前端为纯 HTML)
- **架构**:模块化单体 + MVC 轻量分层(handler → service → repository)

## 目录结构

```
labnexus/
├── cmd/
│   └── server/            # 服务入口
├── internal/
│   ├── config/            # 配置加载(环境变量)
│   ├── database/          # Postgres 连接
│   ├── middleware/        # 认证/权限中间件(阶段 1 引入)
│   ├── user/              # 用户/认证模块(阶段 1)
│   ├── space/             # 个人空间/目录模块(阶段 1)
│   ├── document/          # 文档(笔记/帖子)模块(阶段 1)
│   ├── tag/               # 标签模块(阶段 1)
│   ├── resource/          # 资源库模块(阶段 2)
│   ├── project/           # 项目/任务模块(阶段 2)
│   └── admin/             # 管理端(阶段 1)
├── web/                   # 前端(阶段 1 纯 HTML;正式版 React)
├── docs/
│   ├── schema.sql         # 数据模型定稿(建表 SQL)
│   └── api-contract.md    # API 契约(前后端分离的地基)
└── docker-compose.yml     # 本地开发:Postgres + Redis
```

## 开发规范(必读)

> **开发前必读 [`docs/standards.md`](docs/standards.md)——SDD + TDD 为核心的全套规范。**

- **SDD**:每个功能先写规格(`docs/specs/<feature>.md`,模板见 `_template.md`)→ 评审 → 契约先行 → 编码;
- **TDD**:先写测试(红)→ 最小实现(绿)→ 重构;没有规格不写代码,没有测试不提交;
- **AI Coding**:按规范 §7 的循环(规格 → AI 生成测试 → AI 生成实现 → review),AI 产物红线见 §7.3;
- **提交前**:`make check` 必须全绿。

## 快速开始

```bash
# 1. 启动依赖(Postgres + Redis)
docker compose up -d

# 2. 启动后端(默认 :8080)
make run

# 3. 健康检查
curl http://localhost:8080/api/health
```

环境变量见 `internal/config/config.go`(均有本地开发默认值)。

## 开发约定

- **API 契约先行**:改接口先改 `docs/api-contract.md`,前后端按契约开发;
- **模块化单体 + MVC**:每个领域模块内部分 handler → service → repository 三层;
- **一次一功能**:git 分支管理(`feature/<slug>`),一个功能一个分支;
- **质量门禁**:`make check`(vet + fmt + test + lint + build);
- **数据库**:开发期 GORM AutoMigrate;正式部署前切换 goose 版本化迁移(schema 权威定义见 `docs/schema.sql`)。

## 里程碑

| 阶段 | 内容 | 状态 |
|---|---|---|
| 阶段 0 | PRD 定稿 + 数据模型 + API 契约 + 骨架 | ✅ 当前 |
| 阶段 1 | MVP 社区:F1~F6 | ⏳ |
| 阶段 2 | 资源库 + 任务:F7~F9 | ⏳ |
