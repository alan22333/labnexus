# 功能规格:文献元数据抓取(F8)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.3 F8 |
| 关联契约 | api-contract.md §F8 |
| 涉及表 | 无(只读外部 API) |

## 1. 背景与动机

输入 DOI/arXiv ID 自动抓取文献元数据(标题/作者/期刊/年份),填充 F7 的 paper 资源,生成标准引用(GB/T 由前端展示,后端仅存元数据)。

## 2. 行为需求

- `GET /api/resources/paper/meta?doi=10.xxxx/xxxx` 或 `?arxiv_id=2401.xxxxx`;
- 服务端调用 **Crossref API**(`https://api.crossref.org/works/{doi}`)或 **arXiv API**(`http://export.arxiv.org/api/query?id_list={id}`);
- 返回:`{title, authors:[], journal?, year?, doi?, arxiv_id?}`;
- 未找到该 DOI/ID → 404 `NOT_FOUND`;外部服务不可达 → 502 `BAD_GATEWAY`;
- 抓取失败允许手动填写(F7 paper 的 metadata 手动传)。

## 3. 接口

契约 §F8:`GET /api/resources/paper/meta`。

## 4. 验收标准(可测清单)

- [ ] doi 或 arxiv_id 至少一个,否则 400
- [ ] Crossref 正常响应 → 200 + 元数据解析正确(标题/作者/期刊/年份)
- [ ] arXiv 正常响应(Atom XML)→ 200 + 元数据解析正确
- [ ] 外部返回 404/空 → 404 `NOT_FOUND`
- [ ] 外部服务错误(网络/5xx)→ 502 `BAD_GATEWAY`
- [ ] 请求带超时(5s),不挂起
- [ ] 未登录 → 401

## 5. 边界与异常

- URL 编码(DOI 含 `/` 等特殊字符);
- httpClient 可注入(测试用 httptest 替代,不依赖真实网络);
- 解析容错:外部结构缺失字段时返回可用的部分数据,不 panic。

## 6. 测试计划

- 单元(service):mock http server 分别模拟 Crossref 成功/404/5xx、arXiv 成功;
- handler:错误码断言。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
- [x] 已实现并验收:单元/handler 测试全绿,集成测试(resource_test.go)+ 端到端冒烟通过(scripts/smoke-resource.sh),`make check` 全绿(2026-08-22)
