#!/usr/bin/env bash
# 小账 XiaoZhang 一键安装脚本（Linux + systemd）
#
# 用法：
#   bash <(curl -Ls https://raw.githubusercontent.com/ForceMind/Timoa/main/scripts/install.sh)
#   bash <(curl -Ls .../install.sh) install v1.2.3   # 指定版本
#   bash <(curl -Ls .../install.sh) upgrade          # 升级到最新
#   bash <(curl -Ls .../install.sh) uninstall        # 卸载（保留数据）
#
# 安装内容：
#   二进制  /usr/local/bin/xiaozhang
#   数据    /var/lib/xiaozhang（SQLite + 附件 + 每日备份，权限 0700）
#   服务    systemd 单元 xiaozhang.service（最小权限，开机自启）
#
# 需要 root；支持 amd64 / arm64；二进制纯静态（CGO_ENABLED=0），服务器零依赖。
set -euo pipefail

REPO="ForceMind/Timoa"
BIN="/usr/local/bin/xiaozhang"
DATA_DIR="/var/lib/xiaozhang"
UNIT="/etc/systemd/system/xiaozhang.service"
ENV_FILE="/etc/xiaozhang/env"
ADDR="0.0.0.0:8787"
USER_NAME="xiaozhang"

c_green=$'\033[32m'; c_yellow=$'\033[33m'; c_red=$'\033[31m'; c_reset=$'\033[0m'
info()  { echo "${c_green}[小账]${c_reset} $*"; }
warn()  { echo "${c_yellow}[小账]${c_reset} $*" >&2; }
die()   { echo "${c_red}[小账] $*${c_reset}" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "请以 root 运行（sudo bash ...）"
command -v systemctl >/dev/null || die "未检测到 systemd，本脚本仅支持 systemd 系统"
command -v curl >/dev/null || die "未安装 curl，请先：apt install -y curl / yum install -y curl"

# 从管道执行（bash <(curl ...)）时 stdin 被脚本占用，交互输入改读 /dev/tty
ask() { local prompt="$1" var="$2"; read -r -p "$prompt" "$var" </dev/tty; }

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)   echo amd64 ;;
		aarch64|arm64)  echo arm64 ;;
		*) die "不支持的架构：$(uname -m)（仅支持 amd64/arm64）" ;;
	esac
}

latest_version() {
	curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
		| grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4
}

install_binary() {
	local version="$1" arch tmp
	arch="$(detect_arch)"
	[[ -n "$version" ]] || version="$(latest_version)"
	[[ -n "$version" ]] || die "获取最新版本失败（可能还没有 Release；请先在 GitHub 发布）"
	info "下载 xiaozhang ${version} (linux/${arch}) ..."
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' RETURN
	curl -fsSL -o "$tmp/xiaozhang" \
		"https://github.com/${REPO}/releases/download/${version}/xiaozhang-linux-${arch}" \
		|| die "下载失败：${version} linux/${arch} 不存在或网络不通"
	# 有校验和文件则校验
	if curl -fsSL -o "$tmp/SHA256SUMS" \
		"https://github.com/${REPO}/releases/download/${version}/SHA256SUMS" 2>/dev/null; then
		(cd "$tmp" && grep "xiaozhang-linux-${arch}\$" SHA256SUMS | sha256sum -c -) \
			|| die "SHA256 校验失败，文件可能被篡改，已中止"
		info "SHA256 校验通过"
	else
		warn "该 Release 无 SHA256SUMS，跳过校验"
	fi
	install -m 0755 "$tmp/xiaozhang" "$BIN"
	info "二进制已安装：$BIN ($("$BIN" --version 2>/dev/null || echo "$version"))"
}

ensure_user_dirs() {
	id -u "$USER_NAME" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$USER_NAME"
	mkdir -p "$DATA_DIR" /etc/xiaozhang
	chown "$USER_NAME:$USER_NAME" "$DATA_DIR"
	chmod 0700 "$DATA_DIR"
}

write_unit() {
	cat > "$ENV_FILE" <<EOF
# 小账环境配置（install.sh 生成，可编辑后 systemctl restart xiaozhang）
XIAOZHANG_ADDR=${ADDR}
XIAOZHANG_DATA_DIR=${DATA_DIR}
XIAOZHANG_BACKUP_TIME=03:17
# 备份加密口令（可选；启用后每日备份加密，口令另行保存）
# XIAOZHANG_BACKUP_KEY=
# 若日后套 HTTPS 反代，取消下一行注释：
# XIAOZHANG_SECURE_COOKIES=1
EOF
	chmod 0600 "$ENV_FILE"
	cat > "$UNIT" <<EOF
[Unit]
Description=Timoa XiaoZhang (小账) personal & family accounting
After=network.target

[Service]
Type=simple
User=${USER_NAME}
Group=${USER_NAME}
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN} serve
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=${DATA_DIR}
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
	systemctl daemon-reload
	systemctl enable --now xiaozhang
}

init_admin() {
	# 已有管理员则跳过（探针用户名不会真的创建：InitAdmin 先拒绝重复初始化）
	local probe_out
	probe_out="$(sudo -u "$USER_NAME" env XIAOZHANG_ADMIN_PASSWORD='probe-pass-123' XIAOZHANG_DATA_DIR="$DATA_DIR" \
		"$BIN" init-admin -data "$DATA_DIR" -username __probe__ 2>&1)" || true
	if echo "$probe_out" | grep -q "already exists"; then
		info "管理员已存在，跳过初始化"
		return 0
	fi
	echo
	info "初始化管理员账号（也可稍后执行：xiaozhang init-admin -data ${DATA_DIR} -username <名字>）"
	local user pass pass2
	ask "管理员用户名（留空跳过）: " user
	[[ -n "$user" ]] || { warn "已跳过管理员初始化"; return 0; }
	while true; do
		read -r -s -p "管理员密码（至少 8 位，不显示）: " pass </dev/tty; echo
		[[ ${#pass} -ge 8 ]] || { warn "密码至少 8 位"; continue; }
		read -r -s -p "再次输入确认: " pass2 </dev/tty; echo
		[[ "$pass" == "$pass2" ]] || { warn "两次输入不一致"; continue; }
		break
	done
	if sudo -u "$USER_NAME" env XIAOZHANG_ADMIN_PASSWORD="$pass" XIAOZHANG_DATA_DIR="$DATA_DIR" \
		"$BIN" init-admin -data "$DATA_DIR" -username "$user"; then
		info "管理员 $user 创建成功"
		systemctl restart xiaozhang
	else
		warn "管理员初始化失败，可稍后手动执行 init-admin"
	fi
}

health_check() {
	local i
	for i in $(seq 1 15); do
		if curl -fsS -o /dev/null "http://127.0.0.1:8787/healthz" 2>/dev/null; then
			info "健康检查通过"
			return 0
		fi
		sleep 1
	done
	warn "健康检查未通过，请查看日志：journalctl -u xiaozhang -n 50"
	return 1
}

do_install() {
	local version="${1:-}"
	install_binary "$version"
	ensure_user_dirs
	write_unit
	health_check || true
	init_admin
	echo
	info "安装完成！"
	local ip
	ip="$(curl -fsSL -m 3 https://api.ipify.org 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')"
	echo "  访问地址: http://${ip:-<服务器IP>}:8787"
	echo "  数据目录: ${DATA_DIR}（每日自动备份，保留 14 份）"
	echo "  配置文件: ${ENV_FILE}"
	echo "  常用命令: systemctl status|restart xiaozhang；journalctl -u xiaozhang -f"
	warn "提示：默认未开防火墙放行，如需公网访问请放行 8787 端口（如 ufw allow 8787）"
}

do_upgrade() {
	[[ -x "$BIN" ]] || die "未检测到已安装的小账，请直接运行 install"
	systemctl stop xiaozhang || true
	install_binary "${1:-}"
	systemctl start xiaozhang
	health_check && info "升级完成"
}

do_uninstall() {
	warn "卸载将停止并删除服务与二进制，数据目录 ${DATA_DIR} 保留。"
	local confirm
	ask "确认卸载？[y/N] " confirm
	[[ "$confirm" == "y" || "$confirm" == "Y" ]] || { info "已取消"; exit 0; }
	systemctl disable --now xiaozhang 2>/dev/null || true
	rm -f "$UNIT" "$BIN"
	systemctl daemon-reload
	info "已卸载。数据保留在 ${DATA_DIR}（确认不再需要可手动删除）；环境文件 ${ENV_FILE} 保留。"
}

case "${1:-install}" in
	install)   do_install "${2:-}" ;;
	upgrade)   do_upgrade "${2:-}" ;;
	uninstall) do_uninstall ;;
	*) die "未知命令：$1（可用：install [版本] / upgrade [版本] / uninstall）" ;;
esac
