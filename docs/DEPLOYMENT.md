# 部署指南（单服务器自托管）

> 目标：一台 Linux 服务器即可运行；不需要 Node 常驻、Redis、外部数据库。

## 方式 A：一键脚本（推荐）

在服务器上以 root 执行一条命令（支持 amd64/arm64，需 systemd + curl）：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/ForceMind/Timoa/main/scripts/install.sh)
```

脚本自动完成：下载最新 Release 预编译二进制（SHA256 校验）→ 创建 xiaozhang 系统用户与
`/var/lib/xiaozhang` 数据目录 → 写入最小权限 systemd 单元并启动 → 交互式初始化管理员。

- 访问：`http://<服务器IP>:8787`（公网访问需在防火墙放行 8787，如 `ufw allow 8787`）
- 升级：`bash <(curl -Ls .../install.sh) upgrade`（数据保留）
- 卸载：`bash <(curl -Ls .../install.sh) uninstall`（数据目录保留）
- 指定版本：`bash <(curl -Ls .../install.sh) install v1.0.0`
- 配置：`/etc/xiaozhang/env`（改后 `systemctl restart xiaozhang`）

> 一键方式监听 0.0.0.0:8787 且为明文 HTTP：适合内网/家庭服务器/先试用。
> 长期公网使用建议套 HTTPS 反代（见下文），并在 `/etc/xiaozhang/env` 开启
> `XIAOZHANG_SECURE_COOKIES=1`。

## 方式 B：Docker Compose

```bash
git clone <repo> && cd Timoa
docker compose up -d --build
# 初始化管理员（在容器内执行本地命令）
docker compose exec xiaozhang xiaozhang init-admin -data /data -username admin
curl http://127.0.0.1:8787/healthz
```

升级：`docker compose up -d --build`（数据在 named volume，升级不丢数据）。
备份：`docker compose exec xiaozhang xiaozhang backup -data /data`，或等待每日自动备份。

## 方式 C：systemd 原生（从源码构建）

```bash
# 构建（需要 Go 1.27+ 与 Node 24+）
./scripts/build.sh            # 产出 build/xiaozhang（嵌入前端）
sudo install -m 0755 build/xiaozhang /usr/local/bin/xiaozhang

sudo useradd -r -s /usr/sbin/nologin xiaozhang || true
sudo install -d -o xiaozhang -g xiaozhang -m 0700 /var/lib/xiaozhang
sudo cp deploy/xiaozhang.service /etc/systemd/system/
sudo systemctl daemon-reload

# 初始化管理员（服务器本地执行，密码不入命令行历史）
sudo -u xiaozhang env XIAOZHANG_ADMIN_PASSWORD='<至少8位>' \
  xiaozhang init-admin -data /var/lib/xiaozhang -username admin

sudo systemctl enable --now xiaozhang
systemctl status xiaozhang
curl http://127.0.0.1:8787/readyz
```

## 反向代理

- Nginx：参考 `deploy/nginx.conf.example`（根路径或子路径均可；覆盖 12MB 导入/附件上限）。
- Apache：参考 `deploy/apache.conf.example`。
- 启用 HTTPS 后设置 `XIAOZHANG_SECURE_COOKIES=1`（示例配置已默认）。
- 应用只信任直接对端 IP；示例配置清除了外部转发头。生产 HTTPS 尚未在本仓库实测验证。

## 配置

全部通过环境变量（见 `.env.example`）：监听地址、数据目录、备份时间与加密口令、开发期跨域来源。密钥不进 Git、不出现在前端与日志。

## 常见操作

| 操作 | 命令 |
| --- | --- |
| 初始化管理员 | `xiaozhang init-admin -data <dir> -username <name>` |
| 忘记密码 | `xiaozhang reset-password -data <dir> -username <name>`（吊销旧会话） |
| 手动备份 | `xiaozhang backup -data <dir>` |
| 恢复 | 停服后 `xiaozhang restore -yes -data <dir> <备份包>`（详见 docs/BACKUP_RESTORE.md） |
| 健康检查 | `GET /healthz` `GET /readyz` |

## 排错

- 端口被占用：`ss -ltnp | grep 8787`，改 `XIAOZHANG_ADDR`，不要停止其他服务。
- 迁移失败：服务不就绪（/readyz 非 200）；从最近备份恢复后再升级。
- 日志：`journalctl -u xiaozhang -f` 或 `docker compose logs -f`。
