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
- `POST /api/v1/transactions` `{type:expense|income|transfer, business_date, date_precision?, amount（元字符串）, category_id?, from_account_id?, to_account_id?, note?, merchant?, channel?, operation_id}`
  - 幂等：相同 operation_id + 相同内容返回原交易（`replayed:true`）；不同内容 → 409 `idempotency_conflict`
  - 错误码：`invalid_amount` `invalid_input` `account_not_found` `account_archived` `category_not_found` `cross_ledger` `unauthenticated` `permission_denied` `rate_limited`

## 汇总

- `GET /api/v1/summary?from=YYYY-MM-DD&to=YYYY-MM-DD` → `{income_cents,expense_cents,net_cents,as_of}`（左闭右开）

## 服务器本地命令

- `xiaozhang init-admin -data <dir> -username <name>`（密码来自 `XIAOZHANG_ADMIN_PASSWORD` 或交互输入）
- `xiaozhang reset-password -data <dir> -username <name>`（吊销旧会话，写审计）
