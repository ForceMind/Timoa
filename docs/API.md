# API 概览（v1，随实现更新）

约定：JSON；金额字段为十进制整数分字符串（输入金额用元字符串，最多两位小数）；错误为 `{"error":{"code","message"}}`。认证为 HttpOnly Cookie（`xz_session`），变更请求要求同源（Origin 校验）。

## 健康与设置（匿名）

- `GET /healthz` → `{status, version}`
- `GET /readyz` → `{status, version, sqlite_version, migration_version}`，迁移失败不就绪
- `GET /api/v1/setup/status` → `{needs_init}`

## 认证

- `POST /api/v1/auth/login` `{username,password}` → 设置会话 cookie；连续失败限速锁定（5 次 / 15 分钟）
- `POST /api/v1/auth/logout`（登录）
- `GET /api/v1/auth/me`（登录）→ 用户、账本、角色

## 账户（管理员写）

- `GET /api/v1/accounts` → 含 `balance_cents`（整数分字符串）、`balance_unconfirmed`
- `POST /api/v1/accounts` `{name,type,opening_balance?,opening_date?,balance_confirmed?}`
- `POST /api/v1/accounts/{id}/archive` — 有交易的账户只能归档

## 分类（管理员写）

- `GET /api/v1/categories?kind=expense|income`
- `POST /api/v1/categories` `{name,kind,parent_id?}` — 最多两层

## 交易（成员）

- `GET /api/v1/transactions?limit=`
- `POST /api/v1/transactions` `{type, business_date, amount, category_id?, splits?[{part_type,category_id?,counterparty?,amount}], from_account_id?, to_account_id?, note?, operation_id}`
- `GET /api/v1/transactions/{id}` → 详情（拆分、退款记录、应收状态、修订链）
- `POST /api/v1/transactions/{id}/refund` `{allocations:[{split_id?,amount}], account_id, business_date?, operation_id}` → 409 `refund_cap_exceeded`
- `POST /api/v1/transactions/{id}/income-refund` `{amount, account_id, operation_id}`
- `POST /api/v1/transactions/{id}/settle` `{amount, account_id, counterparty?, operation_id}` → 409 `settlement_cap_exceeded`
- `POST /api/v1/transactions/{id}/reclass` `{amount, counterparty, operation_id}` → 409 `reclass_cap_exceeded`
- `POST /api/v1/transactions/{id}/writeoff` `{amount, reason, operation_id}`
- `POST /api/v1/transactions/{id}/revise` `{reason, replacement?{type,business_date,amount,category_id,from_account_id?,to_account_id?}}` → 409 `dependency_blocked`
- `GET /api/v1/receivables` → 应收往来（形成/已收/核销/待收/账龄）

幂等：所有写操作要求 operation_id；同内容重放返回原结果，异内容 409 `idempotency_conflict`。

## 统计

- `GET /api/v1/summary?from&to` → 净收入/净支出/结余
- `GET /api/v1/stats/daily?from&to` → 每日收支
- `GET /api/v1/stats/overview?from&to` → 原收入/收入退回/净收入/原费用/退款/净支出/结余
- `GET /api/v1/stats/categories?from&to&basis=accrual|origin` → 分类净额（两种跨期口径）

## 服务器本地命令

- `xiaozhang init-admin -data <dir> -username <name>`（密码来自 `XIAOZHANG_ADMIN_PASSWORD` 或交互输入）
- `xiaozhang reset-password -data <dir> -username <name>`（吊销旧会话，写审计）
