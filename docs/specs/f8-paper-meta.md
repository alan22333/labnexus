# 功能规格:文献元数据抓取(F8)—— 已废弃归档

| 项 | 内容 |
|---|---|
| 状态 | **废弃(归档)**。功能下线,代码与接口已移除;重做后再引入 |
| 关联 PRD | §4.3 F8 |
| 关联契约 | api-contract.md §F8(已删除) |
| 废弃记录 | 2026-08-25:依据修改清单 v1(人工审批通过),`paper` 类型与 DOI/arXiv 抓取整体下线;论文以 PDF 形式作为 `file` 上传 |

## 1. 原设计(仅供重做参考)

- 输入 DOI/arXiv ID 自动抓取文献元数据(标题/作者/期刊/年份),填充 paper 资源,生成标准引用;
- 服务端调用 Crossref API(`https://api.crossref.org/works/{doi}`)或 arXiv API(`http://export.arxiv.org/api/query?id_list={id}`);
- 返回:`{title, authors:[], journal?, year?, doi?, arxiv_id?}`;
- 未找到 → 404 `NOT_FOUND`;外部服务不可达 → 502 `BAD_GATEWAY`;抓取失败允许手动填写。

## 2. 下线内容(代码移除清单)

- `CreatePaper` / `FetchPaperMeta` / `fetchCrossref` / `fetchArxiv` / `crossrefResponse` / `arxivFeed` / `PaperMeta`;
- `CreatePaperRequest`、`ErrDOIOrArxivRequired`、`ErrPaperMetaNotFound`、`ErrPaperMetaUpstream`;
- `DefaultCrossrefBase` / `DefaultArxivBase` / `WithEndpoints` / `client` 字段 / `MetaFetchTimeout`;
- handler `PaperMeta`、`POST /api/resources` paper 分支、`GET /api/resources/paper/meta` 路由;
- 相关单元/集成/冒烟测试中的 paper/DOI/arXiv 用例;
- `resources` 表 `doi` / `arxiv_id` / `metadata` 字段(迁移 SQL 删除)。

## 3. 重做触发条件(满足后按 SDD + TDD 重新立项)

- 课题组确实需要"论文记录 + 自动补全元数据 + 引用格式"而非仅 PDF 文件;
- 届时重新评估:paper 类型与 file 的关联方式、元数据字段、抓取服务稳定性与限流。
