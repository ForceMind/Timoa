# 架构说明（随实现更新）

## 总览

单进程 Go 单体 + 嵌入式 SQLite；React SPA 前端（生产由 Go 同源托管构建产物，阶段 1 尚未接入嵌入）。无 Redis / 消息队列 / 外部数据库。

```
cmd/xiaozhang        入口：serve（默认）/ init-admin / reset-password
internal/config      进程配置（flags + env）
internal/storage     SQLite 打开与 pragma、版本化迁移（migrations/*.sql）
internal/ids         UUIDv4 / 随机令牌（crypto/rand）
internal/money       元字符串 ↔ 整数分；拒绝浮点
internal/auth        Argon2id、服务端会话（cookie）、登录限速
internal/bootstrap   首次管理员初始化、本地密码恢复
internal/ledger      复式账务核心：账户、分类、记账、余额、汇总、种子
internal/httpapi     /healthz /readyz /api/v1/* 与安全头、同源检查
web/                 Vite + React + TS 前端
docs/                文档
```

## 数据模型（0001/0002 迁移）

- users / ledgers / ledger_members（role: admin|member）/ sessions（存 sha256(token)）
- subjects：内部科目（asset/liability/income/expense/equity），账户与收支分类都映射到科目
- accounts：资金账户（现金/银行卡/微信零钱/支付宝余额/储值卡/信用卡/花呗/其他资产/借款负债），含期初余额、基准日、确认状态、归档
- categories：收支分类（最多两层）
- transactions：业务交易（expense/income/transfer，posted），含业务日期、日期精度、operation_id + content_hash（幂等）
- entries：复式分录（debit/credit，金额>0），每笔交易借贷平衡
- audit_log：操作审计（不含密码/令牌/凭证）

## 关键决策

- **金额**：int64 分；API 金额字段为十进制整数字符串；`money.ParseYuan` 精确转换 "12.34"→1234，拒绝非法/超两位小数/超范围/零。
- **余额**：期初 + 完整分录求和。资产 = 期初 +（借-贷）；负债 = 期初 +（贷-借），溢缴保留负号。
- **幂等**：(ledger_id, operation_id) 唯一；同内容重放返回原交易，不同内容返回 409 idempotency_conflict；写入由进程内写互斥 + 数据库事务串行化。
- **不可变**：正式交易没有 update/delete 路径（更正/作废走阶段 2 的冲正链）。
- **会话**：HttpOnly + SameSite=Lax cookie，Secure 由 `-secure-cookies` 开启；30 天绝对有效期；改密/恢复/撤销即吊销。
- **CSRF**：SameSite=Lax + 变更请求 Origin 校验；开发代理来源需显式配置 `XIAOZHANG_ALLOWED_ORIGINS`。
- **管理员初始化**：仅服务器本地 `init-admin` 命令；已有管理员时拒绝再次执行，不存在公网抢注。
- **SQLite**：WAL + synchronous FULL + foreign_keys + busy_timeout(5000)，单连接串行写；实测嵌入 SQLite 3.53.4。

## 后台任务

周期扫描、备份等进程内调度任务在对应阶段加入（状态持久化，时间可注入测试）。

## 同步与离线

阶段 5 实现：本地草稿 + 操作队列 + 服务端幂等确认；服务端单调游标。
