# 小账 XiaoZhang

日常小账，心里有数。单服务器自托管的个人与中国家庭记账 PWA。

## 快速开始

```bash
# 后端（默认 127.0.0.1:8787，数据目录 ./data）
go run ./cmd/xiaozhang

# 前端开发（另开终端，代理到 8787）
cd web && npm install && npm run dev
```

验证：`curl http://127.0.0.1:8787/healthz`、`/readyz`。

## 管理员初始化

（阶段 1 提供初始化命令；不设置默认弱密码，关闭公开注册。）

## 文档

- [docs/PRD.md](docs/PRD.md) 需求基线摘要
- [docs/STATUS.md](docs/STATUS.md) 当前进度与限制
- [docs/REFERENCE_RESEARCH.md](docs/REFERENCE_RESEARCH.md) 参考项目研究
- [docs/THIRD_PARTY.md](docs/THIRD_PARTY.md) 第三方许可
- [AGENTS.md](AGENTS.md) 项目约定与不变量

## 技术栈

Go + SQLite（后端，生产嵌入前端产物同源托管）· React + TypeScript + Vite（前端）· 无 Redis / 消息队列 / 外部数据库。
