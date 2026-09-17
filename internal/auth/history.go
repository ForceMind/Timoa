package auth

import (
	"database/sql"
	"strings"

	"xiaozhang/internal/ids"
)

// history.go: 登录/登出遥测 —— 记录 IP、User-Agent，并解析出设备/系统/浏览器，
// 供运营后台「访问记录」查看。解析是尽力而为的启发式，不做精确指纹。

// RecordLogin 记录一次登录或登出。action ∈ login/logout。
func RecordLogin(db *sql.DB, userID, action, ip, ua string) {
	d, o, b := ParseUA(ua)
	_, _ = db.Exec(`INSERT INTO login_history(id,user_id,action,ip,user_agent,device,os,browser)
		VALUES(?,?,?,?,?,?,?,?)`, ids.New(), userID, action, ip, ua, d, o, b)
}

// ParseUA 从 User-Agent 粗略解析 设备 / 系统 / 浏览器。
func ParseUA(ua string) (device, os, browser string) {
	l := strings.ToLower(ua)

	// 设备形态
	switch {
	case strings.Contains(l, "ipad"), strings.Contains(l, "tablet"):
		device = "平板"
	case strings.Contains(l, "mobile"), strings.Contains(l, "iphone"),
		strings.Contains(l, "android") && !strings.Contains(l, "tablet"):
		device = "手机"
	case ua == "":
		device = "未知"
	default:
		device = "桌面"
	}

	// 操作系统
	switch {
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipad"):
		os = "iOS"
	case strings.Contains(l, "android"):
		os = "Android"
	case strings.Contains(l, "windows"):
		os = "Windows"
	case strings.Contains(l, "mac os"), strings.Contains(l, "macintosh"):
		os = "macOS"
	case strings.Contains(l, "linux"):
		os = "Linux"
	case ua == "":
		os = "未知"
	default:
		os = "其它"
	}

	// 浏览器（顺序敏感：先特征后通用）
	switch {
	case strings.Contains(l, "micromessenger"):
		browser = "微信"
	case strings.Contains(l, "edg"):
		browser = "Edge"
	case strings.Contains(l, "opr"), strings.Contains(l, "opera"):
		browser = "Opera"
	case strings.Contains(l, "firefox"):
		browser = "Firefox"
	case strings.Contains(l, "samsungbrowser"):
		browser = "三星浏览器"
	case strings.Contains(l, "ucbrowser"):
		browser = "UC"
	case strings.Contains(l, "chrome"), strings.Contains(l, "crios"):
		browser = "Chrome"
	case strings.Contains(l, "safari"):
		browser = "Safari"
	case ua == "":
		browser = "未知"
	default:
		browser = "其它"
	}
	return device, os, browser
}
