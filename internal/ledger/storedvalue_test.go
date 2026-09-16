package ledger_test

import (
	"testing"
	"time"
)

// TestStoredValueExpiry：储值卡效期——设置面额/到期日、即将到期列表、日历事件。
func TestStoredValueExpiry(t *testing.T) {
	e := newTestEnv(t)

	// 储值卡 + 到期日
	sv, err := e.svc.CreateAccount(e.ledgerID, e.userID, "超市储值卡", "stored_value", nil, 500_00, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if err := e.svc.SetStoredValueMeta(e.ledgerID, sv.ID, 1000_00, exp); err != nil {
		t.Fatal(err)
	}

	// 即将到期列表（7 天内）
	expiring, err := e.svc.ExpiringStoredValue(e.ledgerID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(expiring) != 1 || expiring[0].ID != sv.ID {
		t.Fatalf("expiring: %+v", expiring)
	}
	if expiring[0].ExpiresOn != exp || expiring[0].FaceValue != "100000" {
		t.Fatalf("meta: %+v", expiring[0])
	}

	// 日历事件包含到期提醒
	month := time.Now().AddDate(0, 0, 5).Format("2006-01")
	days, err := e.svc.CalendarMonth(e.ledgerID, month)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range days {
		for _, ev := range d.Events {
			if ev.Kind == "stored_value_expiry" && ev.RefID == sv.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("calendar missing stored_value_expiry event")
	}

	// 校验：非储值卡不能设置效期
	cash, err := e.svc.CreateAccount(e.ledgerID, e.userID, "现金", "cash", nil, 0, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetStoredValueMeta(e.ledgerID, cash.ID, 0, exp); err == nil {
		t.Fatal("non-stored-value account accepted")
	}
	// 校验：非法日期
	if err := e.svc.SetStoredValueMeta(e.ledgerID, sv.ID, 0, "not-a-date"); err == nil {
		t.Fatal("bad expires_on accepted")
	}
	// 已过期卡不进待办
	expired := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	if err := e.svc.SetStoredValueMeta(e.ledgerID, sv.ID, 0, expired); err != nil {
		t.Fatal(err)
	}
	expiring2, _ := e.svc.ExpiringStoredValue(e.ledgerID, 7)
	if len(expiring2) != 0 {
		t.Fatalf("expired card in expiring list: %+v", expiring2)
	}
}
