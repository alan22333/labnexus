# 功能规格:个人空间与目录(F2)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.2 F2 |
| 关联契约 | api-contract.md §F2 |
| 涉及表 | spaces(已有), folders(新增) |

## 1. 背景与动机

每个成员拥有个人空间(注册时自动创建),空间下可建多级目录(如「会议记录 / 近期工作 / 日常记录」),文档(F3)将挂载到目录下。目录树是个人空间的组织骨架。

## 2. 行为需求

- **获取空间**:登录用户获取自己的空间与目录树(按层级返回,sort_order 排序);
- **创建目录**:在空间下创建根目录,或指定 `parent_id` 创建子目录;**父目录必须属于当前用户的空间**(防越权);
- **修改目录**:改名、调整排序(sort_order);
- **删除目录**:仅当目录**为空**(无子目录)时可删;非空返回冲突。

## 3. 接口

契约 §F2:
- `GET /api/me/space` → `{space, folders: [树形]}`
- `POST /api/me/folders` `{name, parent_id?}` → `201 {folder}`
- `PATCH /api/me/folders/:id` `{name?, sort_order?}` → `{folder}`
- `DELETE /api/me/folders/:id` → `204`

## 4. 验收标准(可测清单)

- [ ] 获取空间:返回 space + 目录树,同级按 sort_order 排序
- [ ] 获取空间:用户无 space(异常)→ 404 `NOT_FOUND`
- [ ] 创建根目录:201 + name 非空
- [ ] 创建子目录:指定 parent_id → 挂到该父目录下
- [ ] 创建目录:parent 不属于当前用户 → 403 `FORBIDDEN`
- [ ] 创建目录:parent 不存在 → 404 `NOT_FOUND`
- [ ] 创建目录:name 为空 → 400 `VALIDATION`
- [ ] 修改目录:改名/排序成功 → 200
- [ ] 修改目录:目录不属于当前用户 → 403 `FORBIDDEN`
- [ ] 删除目录:空目录 → 204
- [ ] 删除目录:有子目录 → 409 `CONFLICT`
- [ ] 删除目录:目录不属于当前用户 → 403 `FORBIDDEN`
- [ ] 所有端点未登录 → 401 `AUTH_REQUIRED`

## 5. 边界与异常

- 权限:一切目录操作基于"当前用户的空间",目录归属校验在 service 层(错误语义见上);
- 删除规则:仅检查子目录(文档占用检查在 F3 引入,届时扩展);
- 树形组装:后端返回嵌套 children(递归按 parent_id 分组),MVP 层级不设上限。

## 6. 测试计划

- 单元(service):space 与 folder 用内存替身,覆盖上述验收标准;
- handler:httptest + 真实 service + 内存替身;token 用 token.GenerateAccessToken 直接签发。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
- [x] 已实现并验收:单元/handler 测试全绿(space 70.9%),端到端冒烟通过(scripts/smoke-space.sh),`make check` 全绿(2026-08-22)
