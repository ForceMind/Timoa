package ledger

import (
	"database/sql"
	"fmt"

	"xiaozhang/internal/ids"
)

// SeedCoreCategories installs the minimal stage-1 category set. The full
// library (spec 7.1/7.2) lands with the template module; both are
// idempotent, use stable seed IDs and never overwrite user categories.
func SeedCoreCategories(tx *sql.Tx, ledgerID string) error {
	expense := []string{"餐饮", "交通", "购物", "居住", "其他支出"}
	income := []string{"工资薪酬", "其他收入"}
	for i, name := range expense {
		if err := seedCategory(tx, ledgerID, "seed-cat-exp-"+slug(i), "expense", name, i*10); err != nil {
			return err
		}
	}
	for i, name := range income {
		if err := seedCategory(tx, ledgerID, "seed-cat-inc-"+slug(i), "income", name, i*10); err != nil {
			return err
		}
	}
	return nil
}

func slug(i int) string { return fmt.Sprintf("%02d", i) }

func seedCategory(tx *sql.Tx, ledgerID, id, kind, name string, sort int) error {
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM categories WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil // idempotent
	}
	if _, err := tx.Exec(`INSERT INTO categories(id,ledger_id,parent_id,kind,name,sort,is_seed)
		VALUES(?,?,NULL,?,?,?,1)`, id, ledgerID, kind, name, sort); err != nil {
		return err
	}
	subjectKind := "expense"
	if kind == "income" {
		subjectKind = "income"
	}
	_, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		ids.New(), ledgerID, subjectKind, subjectCode(subjectKind+":cat:", id), name)
	return err
}
