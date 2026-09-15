# AGENTS.md

## 项目约定

- 代码与部署标识 `xiaozhang`；产品名「小账 XiaoZhang」。品牌文案集中配置。
- Go 单体后端（cmd/xiaozhang + internal/*），React + TS + Vite 前端（web/）。模块是代码边界，不是微服务。
- 金额一律整数分（int64 / 十进制整数字符串 API），禁止浮点累计。
- 账务底层复式分录；交易、分录、拆分、幂等记录、审计在同一数据库事务提交。
- 正式财务记录不可变：更正通过冲正 + 替代版本留痕，不覆盖旧分录。
- 迁移版本化、种子幂等；禁止启动时 drop-and-recreate 正式数据。

## 关键不变量

- 每笔分录借贷平衡；余额 = 完整分录求和；有效业务投影不得「过滤原单却累计其冲正」。
- 退款/结算不得超过可退/可结算额度；数据库写事务内重新校验。
- 内部转账、本金往来、期初调整、技术冲正不计入收支。
- 权限全部由服务端校验；跨账本关联必须被拒绝。

## 常用命令

```bash
# 后端
go build ./...
go test ./...
go run ./cmd/xiaozhang -addr 127.0.0.1:8787 -data ./data

# 前端（开发时代理到 8787）
cd web && npm run dev
cd web && npm run build && npx tsc --noEmit
```

## 安全

- 密钥不进 Git、不进日志；配置见 .env.example（不入库 .env）。
- 密码 Argon2id；会话服务端可撤销；不把令牌存 localStorage。
