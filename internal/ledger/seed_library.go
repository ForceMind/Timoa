package ledger

import (
	"database/sql"
	"fmt"

	"xiaozhang/internal/ids"
)

// seed_library.go: 完整内置分类与模板库（spec 7.1–7.3）。
// 幂等：全部使用确定性 seed id，已存在则跳过；只补图标空缺，
// 不覆盖用户改名；种子升级不影响历史账单。

type seedChild struct{ name, icon string }
type seedGroup struct {
	name, icon string
	children   []seedChild
	// reuseID: 阶段 1 平铺种子已存在同名分类时复用其 id 作为父级
	reuseID string
}

var expenseLibrary = []seedGroup{
	{"餐饮", "🍜", []seedChild{{"早餐", "🥣"}, {"午餐", "🍱"}, {"晚餐", "🍲"}, {"外卖", "🥡"}, {"食堂", "🍚"}, {"咖啡奶茶", "🧋"}, {"聚餐", "🥂"}}, "seed-cat-exp-00"},
	{"买菜食品", "🥬", []seedChild{{"蔬菜水果", "🥦"}, {"肉蛋奶", "🥩"}, {"米面粮油", "🌾"}, {"零食饮料", "🍿"}, {"超市采购", "🛒"}}, ""},
	{"居住", "🏠", []seedChild{{"房租", "🏘️"}, {"物业费", "🧾"}, {"水费", "🚰"}, {"电费", "💡"}, {"燃气费", "🔥"}, {"供暖费", "♨️"}, {"家居维修", "🔧"}}, "seed-cat-exp-03"},
	{"交通", "🚌", []seedChild{{"公交地铁", "🚇"}, {"打车", "🚕"}, {"停车", "🅿️"}, {"加油", "⛽"}, {"充电", "🔋"}, {"过路费", "🛣️"}, {"火车票", "🚄"}, {"机票", "✈️"}}, "seed-cat-exp-01"},
	{"购物", "🛍️", []seedChild{{"日用品", "🧻"}, {"衣物鞋包", "👕"}, {"洗护用品", "🧴"}, {"家电", "📺"}, {"数码设备", "📱"}, {"家居用品", "🛋️"}}, "seed-cat-exp-02"},
	{"医疗健康", "💊", []seedChild{{"挂号", "🏥"}, {"检查", "🩻"}, {"药品", "💊"}, {"牙科", "🦷"}, {"体检", "🩺"}, {"护理用品", "🩹"}}, ""},
	{"子女教育", "🎒", []seedChild{{"学费", "🏫"}, {"托管", "🧸"}, {"课外班", "🎨"}, {"书本文具", "📚"}, {"校餐", "🍙"}, {"奶粉", "🍼"}, {"尿裤", "👶"}}, ""},
	{"赡养照护", "👴", []seedChild{{"给父母生活费", "💝"}, {"老人用品", "🦯"}, {"护理服务", "🧑‍⚕️"}, {"养老服务", "🏡"}}, ""},
	{"人情往来", "🧧", []seedChild{{"婚礼随礼", "💒"}, {"生日礼物", "🎂"}, {"节日红包", "🧧"}, {"探望礼品", "🎁"}, {"请客", "🍻"}}, ""},
	{"娱乐休闲", "🎬", []seedChild{{"电影", "🎬"}, {"游戏", "🎮"}, {"运动健身", "🏃"}, {"旅行住宿", "🏨"}, {"景点门票", "🎫"}, {"兴趣课程", "🎹"}}, ""},
	{"通信订阅", "📡", []seedChild{{"手机话费", "📱"}, {"宽带", "🌐"}, {"视频会员", "📺"}, {"音乐会员", "🎵"}, {"网盘", "☁️"}, {"软件订阅", "💿"}}, ""},
	{"宠物", "🐱", []seedChild{{"宠物食品", "🐟"}, {"宠物医疗", "💉"}, {"宠物用品", "🧶"}, {"宠物美容", "✂️"}}, ""},
	{"其他", "📦", []seedChild{{"保障型保险保费", "🛡️"}, {"公益捐赠", "❤️"}, {"手续费", "💳"}, {"其他支出", "📦"}}, "seed-cat-exp-04"},
}

var incomeLibrary = []seedGroup{
	{"工资薪酬", "💼", []seedChild{{"月工资", "💰"}, {"绩效奖金", "🏆"}, {"年终奖", "🎊"}, {"加班费", "⏰"}, {"提成", "📈"}}, "seed-cat-inc-00"},
	{"兼职劳务", "🔧", []seedChild{{"兼职工资", "💵"}, {"项目结算", "🤝"}, {"咨询费", "💡"}, {"稿费", "✍️"}, {"劳务报酬", "🛠️"}}, ""},
	{"经营收入", "🏪", []seedChild{{"销售收入", "🛒"}, {"服务收入", "🧑‍💼"}, {"经营分成", "📊"}}, ""},
	{"财产收入", "🏦", []seedChild{{"房租收入", "🏠"}, {"存款利息", "💹"}, {"分红", "💎"}, {"已确认理财收益", "📈"}}, ""},
	{"养老金补助", "👵", []seedChild{{"养老金", "🧓"}, {"津贴", "🎗️"}, {"奖学金", "🎓"}, {"生活补助", "🤲"}}, ""},
	{"外部赠与", "🧧", []seedChild{{"收到红包", "🧧"}, {"礼金", "🎁"}, {"家庭外部赠与", "💝"}}, ""},
	{"其他", "📦", []seedChild{{"其他收入", "📦"}}, "seed-cat-inc-01"},
}

// 资金操作模板（spec 7.3）：驱动对应业务类型，不是普通收支。
var fundOpTemplates = []struct {
	name, icon, txType string
}{
	{"账户互转", "🔁", "transfer"},
	{"信用卡还款", "💳", "transfer"},
	{"花呗还款", "🐜", "transfer"},
	{"房贷还款", "🏠", "loan_repay"},
	{"车贷还款", "🚗", "loan_repay"},
	{"转入储蓄或理财", "💰", "transfer"},
	{"理财赎回", "📉", "redeem"},
	{"借入", "📥", "borrow"},
	{"借出", "📤", "lend"},
	{"还本金", "↩️", "repay"},
	{"收本金", "💰", "settlement"},
	{"支付押金", "🔒", "lend"},
	{"退回押金", "🔓", "settlement"},
	{"公司报销到账", "🏢", "settlement"},
	{"朋友 AA 回款", "🧑‍🤝‍🧑", "settlement"},
}

var seedTags = []string{"旅行", "装修", "婚礼", "搬家", "节日", "日常家庭"}

// SeedLibrary installs the full category/template/tag library for one
// ledger. Safe to run repeatedly (startup seeding).
func SeedLibrary(tx *sql.Tx, ledgerID string) error {
	if err := seedCategoryLibrary(tx, ledgerID, "expense", expenseLibrary); err != nil {
		return err
	}
	if err := seedCategoryLibrary(tx, ledgerID, "income", incomeLibrary); err != nil {
		return err
	}
	if err := seedFundOpTemplates(tx, ledgerID); err != nil {
		return err
	}
	return seedTagsFor(tx, ledgerID)
}

func seedCategoryLibrary(tx *sql.Tx, ledgerID, kind string, groups []seedGroup) error {
	for gi, g := range groups {
		parentID, err := ensureCategory(tx, ledgerID, kind, "", g.reuseID, "seed-cat-"+kindCode(kind)+fmt.Sprintf("g%02d", gi), g.name, g.icon, gi*100)
		if err != nil {
			return err
		}
		for ci, ch := range g.children {
			if _, err := ensureCategory(tx, ledgerID, kind, parentID, "",
				fmt.Sprintf("seed-cat-%s-g%02d-c%02d", kindCode(kind), gi, ci), ch.name, ch.icon, gi*100+ci+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func kindCode(kind string) string {
	if kind == "income" {
		return "inc"
	}
	return "exp"
}

// ensureCategory: 复用指定 id 或按 seed id 创建；只在图标为空时补图标。
func ensureCategory(tx *sql.Tx, ledgerID, kind, parentID, reuseID, seedID, name, icon string, sort int) (string, error) {
	if reuseID != "" {
		var id string
		err := tx.QueryRow(`SELECT id FROM categories WHERE id=? AND ledger_id=?`, reuseID, ledgerID).Scan(&id)
		if err == nil {
			if _, err := tx.Exec(`UPDATE categories SET icon=? WHERE id=? AND (icon IS NULL OR icon='')`, icon, id); err != nil {
				return "", err
			}
			return id, nil
		}
	}
	var id string
	err := tx.QueryRow(`SELECT id FROM categories WHERE id=?`, seedID).Scan(&id)
	if err == nil {
		return id, nil // 幂等
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}
	if _, err := tx.Exec(`INSERT INTO categories(id,ledger_id,parent_id,kind,name,sort,is_seed,icon)
		VALUES(?,?,?,?,?,?,1,?)`, seedID, ledgerID, parent, kind, name, sort, icon); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		ids.New(), ledgerID, kind, kind+":cat:"+seedID, name); err != nil {
		return "", err
	}
	// 叶子分类生成同名模板（聚餐/房租等一点即记）
	if parentID != "" {
		tplID := "seed-tpl-" + seedID[len("seed-cat-"):]
		if _, err := tx.Exec(`INSERT INTO templates(id,ledger_id,name,icon,tx_type,category_id,amount_policy,pinned,sort,enabled,is_seed)
			SELECT ?,?,?,?,?,?,'manual',0,?,1,1 WHERE NOT EXISTS(SELECT 1 FROM templates WHERE id=?)`,
			tplID, ledgerID, name, icon, kind, seedID, sort, tplID); err != nil {
			return "", err
		}
	}
	return seedID, nil
}

func seedFundOpTemplates(tx *sql.Tx, ledgerID string) error {
	for i, t := range fundOpTemplates {
		id := fmt.Sprintf("seed-tpl-op-%02d", i)
		if _, err := tx.Exec(`INSERT INTO templates(id,ledger_id,name,icon,tx_type,amount_policy,pinned,sort,enabled,is_seed)
			SELECT ?,?,?,?,?,'manual',0,?,1,1 WHERE NOT EXISTS(SELECT 1 FROM templates WHERE id=?)`,
			id, ledgerID, t.name, t.icon, t.txType, 900+i, id); err != nil {
			return err
		}
	}
	return nil
}

func seedTagsFor(tx *sql.Tx, ledgerID string) error {
	for i, name := range seedTags {
		id := fmt.Sprintf("seed-tag-%02d", i)
		if _, err := tx.Exec(`INSERT INTO tags(id,ledger_id,name,is_seed)
			SELECT ?,?,?,1 WHERE NOT EXISTS(SELECT 1 FROM tags WHERE id=?)`, id, ledgerID, name, id); err != nil {
			return err
		}
	}
	return nil
}

// EnsureSeeds runs idempotent seeding for every ledger at startup.
func EnsureSeeds(db *sql.DB) error {
	rows, err := db.Query(`SELECT id FROM ledgers`)
	if err != nil {
		return err
	}
	var ledgerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ledgerIDs = append(ledgerIDs, id)
	}
	rows.Close()
	for _, id := range ledgerIDs {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := SeedCoreCategories(tx, id); err != nil {
			tx.Rollback()
			return err
		}
		if err := SeedLibrary(tx, id); err != nil {
			tx.Rollback()
			return err
		}
		// 应收/应付科目兜底（老库升级）
		for _, s := range []struct{ id, kind, code, name string }{
			{"seed-recv-" + id, "asset", "asset:receivable", "应收款项"},
			{"seed-pay-" + id, "liability", "liability:payable", "应付款项"},
		} {
			if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name)
				SELECT ?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM subjects WHERE ledger_id=? AND code=?)`,
				s.id, id, s.kind, s.code, s.name, id, s.code); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
