# 功能规格:账号系统(F1)

| 项 | 内容 |
|---|---|
| 状态 | 已评审(待实现) |
| 关联 PRD | §4.2 F1 |
| 关联契约 | api-contract.md §F1 |
| 涉及表 | users, invite_codes |

## 1. 背景与动机

课题组内部平台,防止外部人混入:注册必须凭管理员生成的邀请码;登录后所有接口走 JWT(access 短时 + refresh 存 Redis 可撤销)。

## 2. 行为需求

- **注册**:用户提交 `invite_code + username + display_name + password`,邀请码有效(存在、未过期、未使用)才允许注册;注册成功后邀请码标记已使用,自动创建 `space`(个人空间)。
- **登录**:`username + password` 校验通过 → 签发 access token(15 min)与 refresh token(30 天);refresh 存 Redis,access 返回给前端。
- **刷新**:携带有效 refresh → 签发新 access + 新 refresh(轮换,旧 refresh 作废)。
- **登出**:撤销当前 refresh(Redis 删除)。
- **修改资料**:登录用户可改 `display_name`、`password`(改密码需旧密码校验)。
- **角色**:注册默认 `student`;`supervisor`/`admin` 由管理端调整(见管理端规格)。

## 3. 接口

契约 §F1:
- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `GET /api/me`、`PATCH /api/me`

## 4. 验收标准(可测清单)

- [ ] 注册:有效邀请码 → 201 + 用户创建 + 邀请码标记已用 + space 自动创建
- [ ] 注册:邀请码不存在/已过期/已使用 → 401 `INVALID_INVITE`
- [ ] 注册:username 重复 → 409 `CONFLICT`;password < 8 位 → 400 `VALIDATION`
- [ ] 登录:正确凭据 → 200 + access token + httpOnly refresh cookie
- [ ] 登录:错误密码/不存在用户 → 401 `AUTH_FAILED`(不区分具体原因)
- [ ] 刷新:有效 refresh → 新 access + 新 refresh,旧 refresh 立即失效(轮换)
- [ ] 刷新:无效/已撤销 refresh → 401 `AUTH_REQUIRED`
- [ ] 登出:调用后旧 refresh 不可再用
- [ ] `GET /api/me` 返回当前用户(不含 password_hash)
- [ ] `PATCH /api/me` 改 display_name 立即生效;改密码需旧密码正确,否则 400
- [ ] 密码存储为 bcrypt,数据库中不可见明文(手工验证)

## 5. 边界与异常

- 权限:refresh/logout 读 cookie,access 走 Authorization header;两者失效各自返回 401;
- 空态:注册成功但 space 创建失败 → 事务回滚,用户不落库;
- 并发:同一邀请码并发注册 → 数据库唯一约束兜底,仅一人成功;
- 错误码:统一 `{error:{code,message}}`,见契约 §通用约定。

## 6. 测试计划

- 单元(service):注册/登录/刷新/登出的业务规则,repository 用替身;
- handler:httptest 验证状态码、错误格式、cookie 设置;
- 集成:可选,阶段 2 起引入容器 Postgres 覆盖唯一约束/事务场景。

## 7. 评审记录

- [x] §1.3 checklist 通过(2026-08-22)
