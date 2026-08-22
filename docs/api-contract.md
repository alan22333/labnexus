# LabNexus API 契约 v0.1

> 前后端分离的地基:**改接口先改本文档**。前端(阶段 1 纯 HTML → 正式 React)按此契约开发,不因前端框架切换而返工。
> 依据:PRD v0.4 功能清单 F1~F9。

## 通用约定

- Base URL:`/api`;JSON 请求/响应;UTF-8
- 认证:JWT,`Authorization: Bearer <access_token>`;refresh 走 httpOnly cookie `ln_refresh`
- 权限标注:🔓 无需登录 / 🔐 需登录 / 👑 管理员 / ✍️ 作者本人 / 🧑💼 项目负责人
- 分页:统一 `?page=1&page_size=20`,响应 `{..., "pagination": {"page":1,"page_size":20,"total":N}}`
- 错误格式:统一 `{"error": {"code": "AUTH_REQUIRED", "message": "..."}}`
- 常用错误码:`AUTH_REQUIRED` / `FORBIDDEN` / `NOT_FOUND` / `VALIDATION` / `CONFLICT` / `INTERNAL`

## 阶段 1:F1~F6(社区 MVP)

### F1 账号系统

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| POST | `/auth/register` | 🔓 | `{invite_code, username, display_name, password}` | `201 {user}` |
| POST | `/auth/login` | 🔓 | `{username, password}` | `{access_token, user}`(refresh 写 cookie) |
| POST | `/auth/refresh` | 🔓 | —(读 cookie) | `{access_token}`(刷新并轮换 refresh) |
| POST | `/auth/logout` | 🔐 | — | `204`(撤销 refresh,Redis 删除) |
| GET | `/me` | 🔐 | — | `{user}` |
| PATCH | `/me` | 🔐 | `{display_name?, password?}` | `{user}` |

`user` 结构:`{id, username, display_name, role, avatar_url, created_at}`

### F2 个人空间与目录

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/me/space` | 🔐 | — | `{space, folders: []}`(树形) |
| POST | `/me/folders` | 🔐 | `{name, parent_id?}` | `201 {folder}` |
| PATCH | `/me/folders/:id` | 🔐 | `{name?, sort_order?}` | `{folder}` |
| DELETE | `/me/folders/:id` | 🔐 | — | `204`(空目录才可删) |

`folder` 结构:`{id, name, parent_id, sort_order, children: []}`

### F3 文档(笔记 = 帖子)

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/me/documents` | 🔐 | query:`folder_id?, visibility?` | `{documents: []}` |
| POST | `/me/documents` | 🔐 | `{folder_id?, title, content, visibility, tag_ids?}` | `201 {document}` |
| GET | `/documents/:id` | 🔐(作者或公开) | — | `{document}` |
| PATCH | `/documents/:id` | ✍️ | `{title?, content?, visibility?, folder_id?, pinned?, tag_ids?}` | `{document}` |
| DELETE | `/documents/:id` | ✍️ | — | `204`(软删除) |

`document` 结构:`{id, title, content, visibility, pinned, folder_id, author:{id,display_name}, tags:[], reactions_count, comments_count, created_at, updated_at}`
可见性切换:`private → public` = 发帖;`public → private` = 撤回(评论/点赞保留)。

### F4 社区信息流

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/feed` | 🔐 | query:`sort=latest\|hot, page, page_size` | `{documents: [], pagination}` |
| POST | `/documents/:id/reactions` | 🔐 | `{emoji?}`(默认👍) | `204`(toggle:已有则取消) |
| GET | `/documents/:id/comments` | 🔐 | — | `{comments: []}` |
| POST | `/documents/:id/comments` | 🔐 | `{content, reply_to_id?}` | `201 {comment}` |
| DELETE | `/comments/:id` | ✍️ | — | `204` |

`comment` 结构:`{id, content, reply_to_id, author:{id,display_name}, created_at}`

### F5 标签

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/tags` | 🔐 | — | `{tags: []}` |
| POST | `/tags` | 🔐 | `{name, color?}` | `201 {tag}` |
| GET | `/tags/:id/contents` | 🔐 | — | `{documents: [], resources: []}` |

`tag` 结构:`{id, name, color}`

### F6 搜索

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/search` | 🔐 | query:`q, type?(document\|resource\|task)` | `{documents: [], resources: [], tasks: []}` |

MVP 为数据库 LIKE;全文搜索升级见灵感库 F14。

## 阶段 2:F7~F9(资源库 + 项目任务)

### F7 资源库

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/resources` | 🔐 | query:`type?, tag_id?, keyword?, page, page_size` | `{resources: [], pagination}` |
| POST | `/resources` | 🔐 | `{type: link\|paper, title, url?, doi?, arxiv_id?, description?, tag_ids?}` | `201 {resource}` |
| POST | `/resources/upload` | 🔐 | multipart:`file, title?, tag_ids?` | `201 {resource}` |
| GET | `/resources/:id` | 🔐 | — | `{resource}` |
| PATCH | `/resources/:id` | ✍️或👑 | `{title?, description?, tag_ids?}` | `{resource}` |
| DELETE | `/resources/:id` | ✍️或👑 | — | `204` |

`resource` 结构:`{id, type, title, url, doi, arxiv_id, metadata:{authors,journal,year}, uploader:{id,display_name}, tags:[], created_at}`

### F8 文献元数据抓取

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/resources/paper/meta` | 🔐 | query:`doi=? 或 arxiv_id=?` | `{title, authors:[], journal?, year?, doi?, arxiv_id?}` |

服务端调 Crossref / arXiv API,失败返回 `{error}` 由前端提示手动填写。

### F9 项目与任务

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/projects` | 🔐 | — | `{projects: []}`(含本人成员项目) |
| POST | `/projects` | 🔐 | `{name, description?}` | `201 {project}`(创建者即 owner) |
| GET | `/projects/:id` | 🔐(成员) | — | `{project, members, milestones, tasks}` |
| PATCH | `/projects/:id` | 🧑💼 | `{name?, description?, status?}` | `{project}` |
| POST | `/projects/:id/members` | 🧑💼 | `{user_id, role?}` | `201 {member}` |
| DELETE | `/projects/:id/members/:user_id` | 🧑💼 | — | `204` |
| POST | `/projects/:id/milestones` | 🧑💼 | `{name, due_date?}` | `201 {milestone}` |
| PATCH | `/milestones/:id` | 🧑💼 | `{name?, due_date?, completed_at?}` | `{milestone}` |
| GET | `/projects/:id/tasks` | 🔐(成员) | query:`status?, assignee_id?, milestone_id?` | `{tasks: []}` |
| POST | `/projects/:id/tasks` | 🔐(成员) | `{title, description?, assignee_id?, priority?, due_date?, milestone_id?, link_document_id?, link_resource_id?}` | `201 {task}` |
| PATCH | `/tasks/:id` | 🔐(负责人/指派者) | `{title?, description?, priority?, due_date?, assignee_id?, milestone_id?}` | `{task}` |
| POST | `/tasks/:id/transition` | 🔐(负责人/指派者) | `{status}` | `{task}` |
| DELETE | `/tasks/:id` | 🔐(负责人) | — | `204`(软删除) |

- `project` 结构:`{id, name, description, status, owner:{id,display_name}, created_at}`
- `task` 结构:`{id, title, description, status, priority, due_date, milestone_id, assignee:{id,display_name}, created_at, updated_at}`
- **状态机**(service 层校验合法迁移):`todo → in_progress → blocked → todo`(受阻后可回进行中)、`in_progress → done`;done 为终态;其余迁移返回 `VALIDATION` 错误。

## 管理端(阶段 1 起)

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/admin/users` | 👑 | — | `{users: []}` |
| PATCH | `/admin/users/:id/role` | 👑 | `{role}` | `{user}` |
| POST | `/admin/invites` | 👑 | `{expires_at?}` | `201 {code, url}` |
| GET | `/admin/invites` | 👑 | — | `{invites: []}` |
| DELETE | `/admin/invites/:id` | 👑 | — | `204` |

## 待定/备注

- 排序"热门"= 点赞数(阶段 1 按点赞数倒序,无需额外字段);
- 任务关联文档/资源阶段 2 通过 `link_document_id` / `link_resource_id` 一次性写入 `task_links`;
- 权限语义的最终解释权在 `internal/*/service.go` 与 `can()` 接口(PRD §3.3)。
