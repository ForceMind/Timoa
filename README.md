# 小账 XiaoZhang

日常小账，心里有数。单服务器自托管的个人与中国家庭记账 PWA。

## 服务器一键安装

```bash
bash <(curl -Ls https://raw.githubusercontent.com/ForceMind/Timoa/main/scripts/install.sh)
```

一条命令完成安装（预编译二进制 + systemd 服务 + 管理员初始化），访问 `http://<服务器IP>:8787`。

**国内服务器加速**（脚本本身与其下载的二进制均走代理前缀）：

```bash
export XIAOZHANG_GH_PROXY="https://gh-proxy.org/"
bash <(curl -Ls https://gh-proxy.org/https://raw.githubusercontent.com/ForceMind/Timoa/main/scripts/install.sh)
```

升级/卸载/手动部署详见 [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)。

## 本地开发

```bash
# 后端（默认 127.0.0.1:8787，数据目录 ./data）
go run ./cmd/xiaozhang

# 前端开发（另开终端，代理到 8787）
cd web && npm install && npm run dev
```

验证：`curl http://127.0.0.1:8787/healthz`、`/readyz`。

## 管理员初始化

```bash
# 关闭公开注册；首个管理员只能在服务器本机创建
XIAOZHANG_ADMIN_PASSWORD='<至少8位>' go run ./cmd/xiaozhang init-admin -username admin
# 忘记密码（需要服务器本机权限，吊销旧会话并写审计）
XIAOZHANG_ADMIN_PASSWORD='<新密码>' go run ./cmd/xiaozhang reset-password -username admin
```

## 测试

```bash
go test ./...
cd web && npx tsc --noEmit && npm run build
```

## 文档

- [docs/PRD.md](docs/PRD.md) 需求基线摘要
- [docs/STATUS.md](docs/STATUS.md) 当前进度与限制
- [docs/REFERENCE_RESEARCH.md](docs/REFERENCE_RESEARCH.md) 参考项目研究
- [docs/THIRD_PARTY.md](docs/THIRD_PARTY.md) 第三方许可
- [AGENTS.md](AGENTS.md) 项目约定与不变量

## 技术栈

Go + SQLite（后端，生产嵌入前端产物同源托管）· React + TypeScript + Vite（前端）· 无 Redis / 消息队列 / 外部数据库。
