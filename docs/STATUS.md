# 项目状态

更新日期：2026-09-15

## 当前阶段

阶段 0（骨架）→ 阶段 1（认证、账户、账务核心、快速记账）。

## 已实现

- Go 单体后端骨架：`cmd/xiaozhang`，默认监听 127.0.0.1:8787，数据目录 `-data`（默认 ./data）。
- SQLite：modernc.org/sqlite v1.58.0（嵌入 SQLite 3.53.4），WAL + synchronous FULL + 外键 + busy_timeout，单连接串行写。
- 版本化迁移框架（每个迁移一个事务，文件名 NNNN_name.sql）；0001_init。
- /healthz、/readyz（含版本、sqlite_version、migration_version）、/api/v1/meta；基础安全响应头。
- Vite + React + TS 前端骨架（web/），dev 代理到 8787；生产将由 Go 同源托管构建产物（待接）。
- 文档基线：PRD、REFERENCE_RESEARCH（许可证已实测）、THIRD_PARTY、本文件。

## 已验证（真实执行）

- `go build ./...` 通过；服务启动后 /healthz、/readyz、/api/v1/meta 返回 200（本机 127.0.0.1:18787 实测）。
- `npm run build`、`npx tsc --noEmit` 通过。
- 参考仓库许可证经 GitHub API + LICENSE 原文核验（见 REFERENCE_RESEARCH）。

## 未完成 / 下一步

- 阶段 1：Argon2id 认证与服务端会话、管理员初始化命令、资金账户、复式分录核心（整数分）、快速记账闭环、金额/平衡不变量测试。
- 静态产物嵌入 Go、PWA、部署文件、备份恢复均未开始。

## 已知限制

- 未做生产 HTTPS 验证；未做真机验证；性能测试未执行。
- 前端仅为阶段 0 壳，非正式信息架构。
