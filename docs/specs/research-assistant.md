# 功能规格:Research Scout 调研助手(独立服务集成)

| 项 | 内容 |
|---|---|
| 状态 | 已实现并完成真实服务链路验收(2026-10-27);真实 Zotero OAuth 浏览器授权待部署方人工验收 |
| 关联 PRD | 课题组成员个人文献调研与带证据研究简报需求;独立服务产品边界以 Research Scout 仓库 PRD v1.4 为准 |
| 关联契约 | Research Scout `openapi/research-scout.yaml`;LabNexus 不新增 `/api/scout/*` 后端接口 |
| 涉及表 | LabNexus 无新增表;Research Scout 使用独立 PostgreSQL(schema/migration 由该仓库管理) |

## 1. 背景与动机

LabNexus 已承载课题组社区、资源、项目和经费管理,但成员仍需在个人 Zotero 文库、外部学术来源和笔记之间手工切换。Research Scout 作为**独立服务**,以当前 LabNexus 登录身份访问该成员的个人文库与显式研究范围,生成带 claim-level 证据、引用、来源状态和缺口清单的研究简报。

本规格只定义 LabNexus 侧集成边界与验收口径,不把 Research Scout 并入 LabNexus 模块化单体,也不改变 LabNexus 既有数据模型或 API 契约。

## 2. 行为需求

### 2.1 服务与身份

- Research Scout 独立部署,默认 API 地址为 `http://localhost:8081`;LabNexus 正式前端通过 Vite `/api/scout` 代理访问,页面入口为 `/scout`。
- Research Scout 使用 `IDENTITY_PROVIDER=labnexus`,校验 LabNexus HS256 Bearer access JWT;只接受 `Authorization: Bearer <token>`,不接受开发头切换身份。
- LabNexus 与 Research Scout 的 `JWT_SECRET` / `LAB_NEXUS_JWT_SECRET` 必须一致;access token 过期时,前端使用 refresh Cookie 刷新一次并重放原请求,包括普通 API、SSE replay 和 Brief 导出。
- `uid` 映射为 `actor_id=labnexus:<uid>` 和 `workspace_id=labnexus:<uid>`;每个用户独占私有 workspace,禁止共享 workspace。
- 跨 workspace 的项目、会话、Brief、证据和导出必须返回 `404 not_found`,不得泄漏资源存在性。

### 2.2 Zotero 与检索范围

- Zotero 连接只读;支持 OAuth 1.0a 浏览器授权作为产品主路径,API Key 仅作兼容/本地开发入口。
- 只支持个人 My Library,不支持 group libraries、Connector、Zotero 写回、批注/笔记索引、PDF 上传或 PDF 保存/解析。
- 用户必须在创建会话前显式选择项目与 collection scope;scope v2 的保存与恢复由 Research Scout API 负责。
- 连接列表、coverage、同步运行和来源状态必须来自 Research Scout 真实响应;前端不得自行推断可用来源或补造覆盖数量。

### 2.3 证据与简报

- 证据来源分层固定为 `personal_fulltext`、`structured_abstract`、`external_discovery`,并保留 document/version/chunk/epoch/locator 等可追溯字段。
- `metadata_only` 只能说明“只有元数据”,不得作为科学结论证据或伪造为 `personal_fulltext`。
- Brief 至少提供摘要、findings、citations、来源状态与 gaps;证据不足时允许弃答,前端不得把弃答渲染为空成功。
- 首轮查询和跟进轮次均通过 session/turn 承载;SSE 过程事件必须覆盖受理、阶段、证据和 `brief_ready` 等状态。
- JSON、BibTeX、CSV 导出与 evidence detail 必须受当前用户 workspace 隔离。

### 2.4 前端体验

- `/scout` 展示真实连接状态与 coverage;`/scout/connections` 管理 Zotero 连接、同步与来源状态;`/scout/research` 承载项目、scope、历史会话、提问和 Brief。
- 历史会话加载失败必须显示明确错误,不得伪装成“暂无历史会话”。
- 连接 API Key 只存在于表单内存与提交请求中,不得写入 localStorage/sessionStorage、URL 或 console。
- OAuth 回调成功/失败必须有可见结果;失败时保留可重试入口。

## 3. 接口

- LabNexus 不新增后端 Scout 路由;正式前端以 `/api/scout` 为代理前缀,由 Vite/部署网关转发到 Research Scout `/api/v1/*`。
- Research Scout 端点、请求/响应 schema、SSE 事件和错误格式以该仓库 `openapi/research-scout.yaml` 为唯一契约来源,包括:
  - 身份:`GET /api/v1/me`
  - Zotero 连接与 OAuth:`/api/v1/zotero/*`
  - 项目与 scope:`/api/v1/projects*`
  - 会话、turn、SSE、Brief 与导出:`/api/v1/sessions*`
  - 生命周期与审计:Research Scout 契约中对应 `/lifecycle/*`、`/audit-events` 等路由
- 任何 Scout 契约变更先改 `research-scout/openapi/research-scout.yaml` 并遵循该仓库 Change Spec;不修改 LabNexus 受保护的 `docs/api-contract.md`。

## 4. 验收标准(可测清单)

- [x] LabNexus 真实登录后,`GET /api/me` 与 Research Scout `GET /api/v1/me` 均返回 200,且 `identity_provider=labnexus`、actor/workspace 都映射到同一 LabNexus uid。
- [x] 同一 JWT secret 下,前端可在 access token 过期后完成一次 refresh-and-replay,并继续创建会话。
- [x] 项目创建/获取、scope 获取/保存、fixture 索引导入和会话创建闭环成功。
- [x] 首轮会话 `completed`,SSE 共 13 个事件,覆盖 `session_accepted`、阶段、证据和 `brief_ready`。
- [x] 首轮 Brief 含 3 条 `personal_fulltext` 证据、3 citations、3 findings、1 summary claim、0 gaps;每条证据带 document/version/chunk/epoch/locator。
- [x] JSON/BibTeX/CSV 导出和 evidence detail 均返回 200;反馈提交返回 204;跟进轮次返回 202 并完成。
- [x] 第二个 LabNexus 账号无法读取第一个账号的项目、会话、Brief 和导出,均返回 404。
- [x] `/scout`、`/scout/connections`、`/scout/research` 使用真实服务渲染连接、coverage、历史会话、证据、来源状态和过程事件;UI 新建会话成功。
- [x] 前端历史会话加载失败显示错误态;普通 API、SSE replay、Brief 导出共用 401 refresh-and-replay。
- [x] Research Scout 自动化门禁包含 Go 单元/集成/HTTP contract、OpenAPI 解析、Web 测试和生产构建;LabNexus 前端门禁包含测试、lint 和 build。
- [ ] 使用部署方真实 Zotero OAuth client key/secret,在浏览器完成一次真实授权、同步和首轮检索。当前只完成 OAuth 契约/回调测试和 fixture 驱动的真实服务链路验收。

## 5. 边界与异常

- Research Scout 不写 LabNexus 业务库;身份桥只校验 JWT,不复制用户资料、不共享 LabNexus 的文档/资源/项目数据。
- 身份校验失败返回 401;workspace 越权一律 404;索引未就绪返回 `409 index_not_ready`;scope 为空返回对应校验错误。
- Zotero 凭据失效返回可操作的重新连接状态;429、5xx 和网络故障标记为可重试来源不可用,不得伪装成凭据失效。
- 外部检索失败必须体现在 source status 与 gaps 中,不得用模型常识冒充文献证据。
- `metadata_only`、未索引全文和 fixture 数据不得被描述为真实 Zotero 全文同步结果。
- 真实 OAuth 缺少 client key/secret 时,OAuth 路由可禁用,API Key 兼容路径仍可用于本地开发;不得用假凭据宣称端到端授权完成。

## 6. 测试计划

- Research Scout 单元:身份解析、JWT 算法/claim 校验、OAuth 签名与回调校验、凭据加密、证据策略、生命周期清理。
- Research Scout 集成:真实 PostgreSQL/pgvector 下的 workspace 隔离、控制面、sync、会话/Brief、生命周期与 OAuth 回调。
- Research Scout HTTP contract:OpenAPI 路由、状态码、Problem 格式、SSE 事件和导出。
- Research Scout Web:连接页、coverage、历史会话错误态、Brief/证据渲染、SSE replay 与 401 refresh-and-replay。
- LabNexus 前端:scout API 客户端、概览/连接/研究工作台组件测试;`npm test -- --run`、`npm run lint`、`npm run build`。
- 真实联调:两端共享临时 JWT secret,用 LabNexus 演示账号登录,经真实 API 和浏览器验证完整链路;另用第二个账号验证隔离。
- 人工验收:部署方提供真实 Zotero OAuth 应用凭据后,在浏览器完成授权、同步、索引和带证据 Brief。

## 7. 验收记录

- 2026-10-27:真实 LabNexus JWT + Research Scout API + LabNexus 前端链路验收通过。首轮 session `session-b723a2e24694a07f2a4f28ab`,Brief `brief-451b8f77d69f395dd02a8aa7`;fixture 覆盖 8 visible、7 indexed、4 fulltext、3 abstract、1 metadata-only。验收使用临时 secret/账号,敏感值未入库。
- 2026-10-27:跨账号隔离、refresh-and-replay、历史会话错误态与 UI 新会话渲染通过。
- 待人工:真实 Zotero OAuth 浏览器授权、真实 Zotero 文库同步、生产部署网关/CORS/TLS、ReauthVerifier 与备份恢复演练。以上未完成前,不得宣称生产验收完成。

## 8. 决策记录

- Research Scout 保持独立服务;LabNexus 只承载入口和身份来源,不合并代码库、不共享数据库。
- 身份桥使用 LabNexus HS256 JWT,不引入 OIDC;密钥轮换需两端同步发布。
- 每个 LabNexus 用户对应独立私有 Scout workspace,宁可重复创建也不共享私有语料。
- fixture 仅用于可重复的自动化与联调验收;真实 Zotero 授权/同步必须由部署方凭据完成。
