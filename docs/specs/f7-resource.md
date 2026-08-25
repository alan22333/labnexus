# 功能规格:资源库(F7,link + file)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(重写:去除 paper 类型,重做后再引入) |
| 关联 PRD | §4.3 F7 |
| 关联契约 | api-contract.md §F7 |
| 涉及表 | resources, resource_tags(修改);tags 复用 |
| 修订记录 | 2026-08-25 v2:删除 paper/DOI/arXiv,保留 link+file,新增描述/下载/预览/文件安全(依据修改清单 v1,人工审批通过) |

## 1. 背景与动机

**解决"微信群文件过期"的核心痛点**:链接/文件统一入库,带标签可检索。资源是组内**共享库**(全组可见),无私有概念;修改/删除仅上传者或 admin。

**v2 变更动机**:论文以 PDF 形式作为文件上传即可,无需单独 `paper` 类型与 DOI/arXiv 元数据抓取(该设计重做后再引入,见 `f8-paper-meta.md` 归档说明)。

## 2. 行为需求

- **两种资源类型**:
  - `link`:收藏 URL(标题必填,url 必填且仅 http/https,可选 description);
  - `file`:上传文件(PDF/Word/PPT/Excel/图片/文本/压缩包/视频),存储到服务器 `data/uploads/`,随机文件名;
- **创建**:登录用户即可;`type` 合法(link/file);title 非空;link 需合法 url;file 走 multipart 上传;
- **列表**:全组可见;筛选 `type` / `tag_id` / `keyword`(标题 ILIKE)+ 分页;
- **详情**:任意登录用户;
- **下载/预览**:仅 file 类型,任意登录用户;link 调用返回 400;
- **修改/删除**:仅上传者或 admin(角色判断走 can 逻辑),他人 403;删除 file 同步删磁盘文件,删除失败记日志;
- **标签**:资源可打标签(复用全局标签库,不存在 → 400)。

## 3. 接口

契约 §F7:
- `GET /api/resources`(query: type?/tag_id?/keyword?/page/page_size)
- `POST /api/resources`(link)
- `POST /api/resources/upload`(multipart: file, title?, description?, tag_ids?)
- `GET /api/resources/:id`
- `GET /api/resources/:id/download`(file 流)
- `GET /api/resources/:id/preview`(file 流,支持类型)
- `PATCH /api/resources/:id`
- `DELETE /api/resources/:id`

## 4. 验收标准(可测清单)

- [ ] 创建 link:201;url 缺失 → 400;url 非法协议(如 `javascript:`/`ftp:`)→ 400;title 空 → 400;description 可选
- [ ] 上传 file:201;磁盘出现文件(data/uploads/);响应含 id/标题/描述/mime_type/file_size/original_name
- [ ] 非法 type → 400
- [ ] 文件类型:白名单外扩展名 → 400;扩展名与内容不符(如改名的 exe)→ 400
- [ ] 文件大小:超过类型上限 → 400
- [ ] 标签:tag_ids 含不存在标签 → 400;列表返回 tags 数组
- [ ] 列表:type 筛选 / tag_id 筛选 / keyword 筛选(标题 ILIKE)/ 分页 total 正确
- [ ] 详情:任意登录用户 200
- [ ] 下载:file 200,Content-Disposition attachment + 原始文件名;link → 400;非上传者也可下载
- [ ] 预览:pdf/图片/文本/视频 → 200 inline;docx/xlsx/zip → 400 PREVIEW_UNSUPPORTED;link → 400
- [ ] 修改:上传者 200(含 description);他人 403;admin 可改
- [ ] 删除:上传者 204 且磁盘文件删除;他人 403
- [ ] 未登录 → 401

## 5. 边界与异常

- 文件:扩展名白名单(pdf/doc/docx/txt/md/ppt/pptx/xls/xlsx/png/jpg/jpeg/webp/gif/zip/tar/gz/mp4/webm);
- MIME 双校验:读取文件头 512 字节 `http.DetectContentType`,PDF 查 `%PDF-`,Office/zip 查 ZIP magic `PK\x03\x04`;扩展名与检测 MIME 不一致 → 400;
- 大小:普通文件 ≤ 50MB;视频(mp4/webm)≤ 100MB;整体请求体 `http.MaxBytesReader` 上限 100MB+余量;
- 随机文件名防覆盖;FilePath 字段不随响应暴露;
- 预览:text/md 以 `text/plain; charset=utf-8` 返回(防 HTML 注入);视频用 `http.ServeContent` 支持 Range;
- 资源共享:列表/详情/下载/预览不区分上传者(全组可见);
- 软删除:资源无软删除(物理删除 + 删文件),删除为永久操作;
- 孤儿文件:删除时若磁盘删除失败,记录错误日志(不阻塞数据库删除)。

## 6. 测试计划

- 单元(service):创建校验(link url 协议/格式)、文件类型双校验、大小限制、预览支持判定、描述字段、权限(上传者/admin/他人);FileStore 用内存替身;
- handler:httptest + multipart 上传(真实 MIME 内容)、download/preview 路由与响应头;
- 集成:资源库增删改查 + 下载/预览 + 权限(真实 DB);
- 冒烟:scripts/smoke-resource.sh(link+file+下载/预览)。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-25,v2 重写)
- [x] 已实现并验收:单元/handler 全绿;集成测试(含下载/预览/上传边界)全绿;冒烟 scripts/smoke-resource.sh 通过;`make check` 全绿(2026-08-25)
