# 功能规格:社区信息流/点赞/评论(F4)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.2 F4 |
| 关联契约 | api-contract.md §F4 |
| 涉及表 | documents(公开), reactions, comments |

## 1. 背景与动机

高频社区功能养留存:首页时间线展示全组公开文档(帖子),支持点赞与评论——"科研朋友圈"的核心体验。

## 2. 行为需求

- **信息流**:`GET /feed` 返回公开文档,`sort=latest`(创建时间倒序)/ `sort=hot`(点赞数倒序),分页;
- **点赞**:对公开文档 toggle(已有则取消,默认 emoji 👍);一人一文档一表情一次(唯一约束);
- **评论**:对公开文档评论(作者也可评论自己的公开文档);一级回复(reply_to_id);
- **删除评论**:仅评论作者。

## 3. 接口

契约 §F4:`GET /feed`、`POST /documents/:id/reactions`、`GET /documents/:id/comments`、`POST /documents/:id/comments`、`DELETE /comments/:id`。

## 4. 验收标准(可测清单)

- [ ] 信息流只含公开文档(私有不出现);latest 按创建倒序;hot 按点赞数倒序
- [ ] 信息流条目含 author/tags/reactions_count/comments_count
- [ ] 点赞 toggle:首次 → 计数+1;再点 → 取消;一人一文档一表情一次
- [ ] 对私有文档点赞/评论 → 404
- [ ] 评论成功 → 201 + 作者信息;回复(reply_to_id)合法
- [ ] 删除评论仅作者,他人 → 403
- [ ] 未登录 → 401

## 5. 边界与异常

- 分页:page ≥ 1,page_size 1~50(默认 20);
- 计数:列表批量统计,禁止 N+1(规范 §5);
- hot 排序:点赞数相同按创建时间倒序(稳定)。

## 6. 测试计划

- 单元(service):可见性拦截、toggle 语义、hot 排序、分页边界;
- handler:契约错误码、401/403/404 断言。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
- [x] 已实现并验收:单元/handler 测试全绿,端到端冒烟通过(scripts/smoke-doc.sh),`make check` 全绿(2026-08-22)
