package ledger_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"xiaozhang/internal/ledger"
)

// TestPerfScale10W（T44）以可复现的方式造 10 万条流水，实测
// 记账吞吐与核心读路径延迟。默认运行（使用真实 WAL+FULL 存储路径）；
// PERF_SCALE=5000 等可缩小规模用于快速复跑。
//
// 造数规则固定：日期 2026-01-01 起按序摊开（每天 100 笔，
// 真实日历推进，覆盖约 1000 天）、金额按序 100 分递增，
// 任何机器复跑数据量与形状一致。
func TestPerfScale10W(t *testing.T) {
	n := 100000
	if v := os.Getenv("PERF_SCALE"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}

	e := newTestEnv(t)
	acc := e.account(t, "性能现金", "cash", 100_000_000_00)
	cat := e.categoryID(t, "expense", "餐饮")

	start := time.Now()
	for i := 0; i < n; i++ {
		// 每天 100 笔，从 2026-01-01 起真实日历推进，覆盖约 1000 天。
		date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i/100).Format("2006-01-02")
		res, err := e.svc.Post(ledger.PostInput{
			LedgerID:      e.ledgerID,
			ActorID:       e.userID,
			Type:          "expense",
			BusinessDate:  date,
			AmountCents:   int64(100 + i%5000),
			CategoryID:    cat,
			FromAccountID: acc.ID,
			Note:          fmt.Sprintf("perf-%d", i),
			OperationID:   fmt.Sprintf("perf-op-%d", i),
		})
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		if res.Replayed {
			t.Fatalf("post %d unexpectedly replayed", i)
		}
	}
	postDur := time.Since(start)
	t.Logf("造数：%d 笔支出，耗时 %v（%.1f 笔/秒）", n, postDur, float64(n)/postDur.Seconds())

	type readCase struct {
		name string
		run  func(t *testing.T)
	}
	cases := []readCase{
		{"账户余额", func(t *testing.T) {
			if _, err := e.svc.GetAccount(e.ledgerID, acc.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"月度概览", func(t *testing.T) {
			if _, err := e.svc.Overview(e.ledgerID, "2026-06-01", "2026-06-30"); err != nil {
				t.Fatal(err)
			}
		}},
		{"分类净额(发生期)", func(t *testing.T) {
			if _, err := e.svc.CategoryNets(e.ledgerID, "2026-06-01", "2026-07-01", "accrual"); err != nil {
				t.Fatal(err)
			}
		}},
		{"最近流水50", func(t *testing.T) {
			if _, err := e.svc.ListTransactions(e.ledgerID, 50); err != nil {
				t.Fatal(err)
			}
		}},
		{"月汇总", func(t *testing.T) {
			if _, err := e.svc.PeriodSummary(e.ledgerID, "2026-06-01", "2026-06-30"); err != nil {
				t.Fatal(err)
			}
		}},
		{"同步拉取(游标500)", func(t *testing.T) {
			if _, err := e.svc.TransactionsSinceRowID(e.ledgerID, int64(n-500), 500); err != nil {
				t.Fatal(err)
			}
		}},
		{"日历月视图", func(t *testing.T) {
			if _, err := e.svc.CalendarMonth(e.ledgerID, "2026-06"); err != nil {
				t.Fatal(err)
			}
		}},
		{"资产负债", func(t *testing.T) {
			if _, err := e.svc.AssetsOverview(e.ledgerID); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		// 预热 1 次，再计时 3 次取最好值（反映缓存稳态）。
		c.run(t)
		best := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ {
			s := time.Now()
			c.run(t)
			if d := time.Since(s); d < best {
				best = d
			}
		}
		t.Logf("读路径 %-18s 最优 %v", c.name, best)
	}

	// 不变量抽查：余额 = 期初 + 全部分录求和（已由 GetAccount 实现），
	// 这里确认数量与总额精确。
	wantTotal := int64(0)
	for i := 0; i < n; i++ {
		wantTotal += int64(100 + i%5000)
	}
	a, err := e.svc.GetAccount(e.ledgerID, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.ParseInt(a.Balance, 10, 64)
	if want := 100_000_000_00 - wantTotal; got != want {
		t.Fatalf("余额不变量破坏：got %d want %d", got, want)
	}
	t.Logf("不变量校验通过：余额 %d 分 = 期初 - Σ%d 笔支出", got, n)
}
