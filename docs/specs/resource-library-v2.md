# 功能规格:资源库 v2(检索/组织/协作增强 + 分类)

| 项 | 内容 |
|---|---|
| 状态 | 开发中(2026-10-17) |
| 关联 PRD | §4.3 F7(资源库) |
| 关联契约 | api-contract.md §F7(本规格随契约同步更新) |
| 涉及表 | resources(改), resource_tags(不变), **resource_categories(新增)**, **resource_favorites(新增)** |
| 修订记录 | 2026-10-17 v1:在 f7-resource.md 基础上补充检索/排序/分类/置顶/计数/收藏/预览增强 |

## 1. 背景与动机

F7 已实现「链接 + 文件 + 标签 + 基础筛选」,解决了"存得进去"。实测后发现新的核心痛点:

1. **找不到**:关键词只匹配标题,描述/文件名/URL/标签搜不到;页面没有标签筛选入口;只有"最新"一种排序;
2. **用不上**:无法判断资料是否被使用(无浏览/下载数据),无法置顶组内必读,没有个人收藏;
3. **没有分类**:标签是自由的多对多,缺少管理员维护的**唯一分类**维度,资料一多就散。

本规格保持"资源为全组共享库"的既定决策不变,只做增强,不引入私有资源。

## 2. 关键设计决策

- **分类(category)与标签(tag)并存,职责不同**:
  - 分类 = 管理员维护的课题组级**字典**,资源**最多归属一个**分类(可空),取值稳定、可停用(沿用 `task_types` 的后台管理模式);
  - 标签 = 成员自由创建的**多对多**,不设限;
- **置顶(pinned)**:组内必读资料浮到列表最前;置顶权限 = 上传者或 admin(与"修改"权限一致);
- **浏览/下载计数(view_count/download_count)**:服务端计数,作为"最有用"排序依据;不做去重(同一人多次浏览累加);
- **收藏(favorite)**:**个人**维度,不影响全组可见性;可筛"我的收藏";
- **链接去重**:收藏 link 时若 URL 已存在 → 409,前端二次确认后可强制收藏;
- **兼容性**:新增字段对旧客户端向后兼容(resource 响应只增字段);列表默认排序保持"最新在前"。

## 3. 行为需求

### 3.1 检索与筛选

`GET /api/resources` 查询参数(全部可选,可组合):

| 参数 | 说明 |
|---|---|
| `type` | `link` / `file` |
| `keyword` | **跨字段**模糊匹配:`title` / `description` / `original_name` / `url` / **标签名** |
| `tag_ids` | 逗号分隔;多标签为 **AND**(需同时具备所有标签) |
| `category_id` | 分类筛选;`uncategorized=true` 筛"未分类" |
| `uploader_id` | 按上传者筛 |
| `mine` | `true` → 只看我上传的(等价 `uploader_id=<当前用户>`) |
| `favorited` | `true` → 只看我收藏的 |
| `pinned` | `true` → 只看置顶 |
| `sort` | `latest`(默认) / `oldest` / `title` / `size` / `useful` |
| `page` / `page_size` | 分页(page_size 上限 50) |

- **排序**:列表**始终**置顶优先(`pinned DESC`),再按 `sort`;`useful` = `(view_count + download_count) DESC`,末位 `created_at DESC` 保证稳定;
- 筛选栏与排序状态反映在 URL?否(前端本地 state 即可,本期不做深链)。

### 3.2 分类管理

- `GET /api/resource-categories`(登录可见):返回 **active** 分类,按 `sort` 升序,供创建弹窗/筛选下拉;
- `/api/admin/resource-categories`(仅 admin):列表(含停用)、新建、改名/排序/停用、删除;
- 删除分类:引用该分类的资源 `category_id` 置空(拟"未分类"),**不删资源**;
- 名称唯一;空名 → 400;重名 → 409。

### 3.3 置顶

- `PATCH /api/resources/:id` 支持 `pinned?: boolean`;权限 = 上传者或 admin;
- 非上传者且非 admin 设置 pinned → 403。

### 3.4 计数

- `POST /api/resources/:id/view`(登录即可)→ `view_count += 1`,返回 204;
- `GET /api/resources/:id/download`(仅 file)→ 成功返回文件流时 `download_count += 1`;
- 计数写入失败不阻断主流程(尽力而为,记日志)。

### 3.5 收藏

- `POST /api/resources/:id/favorite` → 收藏(幂等,重复调用 204);资源不存在 → 404;
- `DELETE /api/resources/:id/favorite` → 取消收藏(幂等,204);
- `resource.favorited` 表示**当前请求用户**是否已收藏。

### 3.6 链接去重

- `POST /api/resources`(link)若存在**完全相同** `url`(去除首尾空白后精确匹配)→ 409 `CONFLICT`,错误体带 `existing_id`;
- 请求体 `force: true` 时跳过去重检查,照常创建;
- 前端捕获 409 后弹二次确认,确认则以 `force: true` 重试。

### 3.7 预览增强

- `md` 文件:预览时按 **Markdown** 渲染(标题/加粗/列表/代码/表格/链接/图片),前端实现;
- `xlsx`/`xls`:预览时解析为**表格**(首个工作表),前端用已有 `xlsx` 依赖实现;
- 其它类型维持现状(pdf/图片/文本/视频 inline;doc/docx/ppt/pptx/zip/tar/gz 仅下载);
- **注意**:预览仍是"取回文件流在前端渲染",后端 `/preview` 对 md 仍返回 `text/plain` 防注入(前端拿到原文再渲染);
  - `xlsx` 后端预览支持判定**扩展为 supported**(返回 `application/octet-stream`),由前端解析后渲染表格。

## 4. 接口(契约 §F7 同步)

```
GET    /api/resources?type&keyword&tag_ids&category_id&uncategorized&uploader_id&mine&favorited&pinned&sort&page&page_size
POST   /api/resources                      {type:link,title,url,description?,category_id?,tag_ids?,force?}
POST   /api/resources/upload               multipart:file,title?,description?,category_id?,tag_ids?
GET    /api/resources/:id
POST   /api/resources/:id/view             → 204
GET    /api/resources/:id/download         → 文件流(仅 file;download_count+1)
GET    /api/resources/:id/preview          → 文件流(仅 file 且 supported)
PATCH  /api/resources/:id                  {title?,description?,category_id?,pinned?,tag_ids?}
DELETE /api/resources/:id                  → 204
POST   /api/resources/:id/favorite          → 204
DELETE /api/resources/:id/favorite          → 204
GET    /api/resource-categories            → {categories:[]}(active)
GET    /api/admin/resource-categories      → {categories:[]}(含停用,仅 admin)
POST   /api/admin/resource-categories      {name,sort?} → 201 {category}
PATCH  /api/admin/resource-categories/:id  {name?,sort?,active?} → {category}
DELETE /api/admin/resource-categories/:id  → 204
```

`resource` 结构新增字段(其余不变):

```json
{
  "category": {"id": "...", "name": "文献"},   // 可空 → null
  "pinned": false,
  "view_count": 12,
  "download_count": 3,
  "favorited": true
}
```

## 5. 验收标准(可测清单)

- [ ] keyword 命中 title / description / original_name / url / 标签名(任一即命中)
- [ ] `tag_ids` 多标签 AND:资源需同时具备全部标签;单标签行为与旧 `tag_id` 一致
- [ ] `sort` 五种取值结果正确;`pinned` 在任意排序下均置顶
- [ ] `mine=true` 只返回当前用户上传的;`favorited=true` 只返回我收藏的
- [ ] `uncategorized=true` 只返回 `category_id IS NULL` 的资源
- [ ] 分类:非 admin 调 `/admin/resource-categories` → 403;空名 400;重名 409;删除后资源回退未分类
- [ ] 置顶:上传者/admin 可置顶;他人 403
- [ ] 计数:view 端点 204 且 view_count+1;下载一次 download_count+1;计数失败不影响下载
- [ ] 收藏:收藏→`favorited=true`;取消→false;重复调用幂等;资源不存在 404
- [ ] 去重:重复 URL → 409 且带 existing_id;`force:true` 可创建
- [ ] 预览:`md` 前端渲染为 Markdown;`xlsx` 前端渲染为表格;docx 等仍 400 PREVIEW_UNSUPPORTED
- [ ] 回归:既有 F7 用例(上传校验/权限/隔离删除)全绿

## 6. 边界与异常

- `tag_ids` 含不存在标签 → 400(与创建行为一致);
- `category_id` 不存在或已停用 → 400;
- 排序参数非法 → 400;
- 计数为**非事务**的尽力更新;并发下允许计数器竞态(展示用途,不要求强一致);
- 收藏与浏览对**任意登录用户**可用,不校验上传者;
- 删除资源级联删除其收藏(`resource_favorites` ON DELETE CASCADE)。

## 7. 测试计划

- 单元(service):keyword 跨字段命中、多标签 AND、排序分支、mine/favorited/uncategorized、置顶权限、计数、收藏幂等、去重与 force、分类 CRUD 校验;
- handler:新区间路由的状态码与响应体;multipart 携带 category_id;409 去重体;
- 集成:真实 DB 的资源列表(带分类/收藏/计数)+ 分类管理权限矩阵;
- 前端:lint + build(无测试框架,沿用现有门禁)。

## 8. 评审记录

- [ ] 规格评审(待)
- [ ] 契约已同步
- [ ] schema.sql 已同步 + 迁移已加
