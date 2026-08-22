# 功能规格:资源库(F7)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.3 F7 |
| 关联契约 | api-contract.md §F7 |
| 涉及表 | resources, resource_tags(新增);tags 复用 |

## 1. 背景与动机

**解决"微信群文件过期"的核心痛点**:文献/链接/文件统一入库,带标签可检索。资源是组内**共享库**(全组可见),无私有概念;修改/删除仅上传者或 admin。

## 2. 行为需求

- **三种资源类型**:
  - `link`:收藏 URL(标题必填,自动抓标题/摘要为辅助,失败不阻塞);
  - `paper`:DOI/arXiv 文献(元数据由 F8 抓取或手动填写);
  - `file`:上传文件(PDF/Word/图片等),存储到服务器 `data/uploads/`,随机文件名;
- **创建**:登录用户即可;`type` 合法(link/paper/file);title 非空;link 需 url;paper 需 doi 或 arxiv_id 至少其一;file 走 multipart 上传;
- **列表**:全组可见;筛选 `type` / `tag_id` / `keyword`(标题 ILIKE)+ 分页;
- **详情**:任意登录用户;
- **修改/删除**:仅上传者或 admin(角色判断走 can 逻辑),他人 403;删除 file 同步删磁盘文件;
- **标签**:资源可打标签(复用全局标签库,不存在 → 400)。

## 3. 接口

契约 §F7:
- `GET /api/resources`(query: type?/tag_id?/keyword?/page/page_size)
- `POST /api/resources`(link/paper)
- `POST /api/resources/upload`(multipart: file, title?, tag_ids?)
- `GET /api/resources/:id`
- `PATCH /api/resources/:id`
- `DELETE /api/resources/:id`

## 4. 验收标准(可测清单)

- [ ] 创建 link:201;url 缺失 → 400;title 空 → 400
- [ ] 创建 paper:doi 或 arxiv_id 至少一个,否则 400;metadata 可手动传
- [ ] 上传 file:201;磁盘出现文件(data/uploads/);响应含 id/标题
- [ ] 非法 type → 400
- [ ] 标签:tag_ids 含不存在标签 → 400;列表返回 tags 数组
- [ ] 列表:type 筛选 / tag_id 筛选 / keyword 筛选(标题 ILIKE)/ 分页 total 正确
- [ ] 详情:任意登录用户 200
- [ ] 修改:上传者 200;他人 403;admin 可改
- [ ] 删除:上传者 204 且磁盘文件删除;他人 403
- [ ] 未登录 → 401

## 5. 边界与异常

- 文件:类型白名单(pdf/doc/docx/txt/md/png/jpg/jpeg/webp/gif)+ 大小上限 20MB;随机文件名防覆盖;
- 资源共享:列表/详情不区分上传者(全组可见);
- 软删除:资源无软删除(物理删除 + 删文件),删除为永久操作;
- FilePath 字段不随响应暴露。

## 6. 测试计划

- 单元(service):创建校验、筛选、权限(上传者/admin/他人)、标签校验;FileStore 用内存替身;
- handler:httptest + multipart 上传;
- 集成:资源库增删改查 + 权限(真实 DB)。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
- [x] 已实现并验收:单元/handler 测试全绿,集成测试(resource_test.go)+ 端到端冒烟通过(scripts/smoke-resource.sh),`make check` 全绿(2026-08-22)
- [x] 深度接口测试:筛选组合/分页/上传边界/文件生命周期/越权细化/契约结构(resource_deep_test.go)
