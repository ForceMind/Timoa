package impexp_test

import (
	"strings"
	"testing"

	"xiaozhang/internal/impexp"
)

const wechatSample = `微信支付账单明细,,,,,,,,,
,,,,,,,,,
----------------------微信支付账单明细列表--------------------,,,,,,,,,
交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注
2026-09-01 12:30:00,商户消费,麦当劳,午餐,支出,¥28.00,零钱,支付成功,4200001234567890,12345,
2026-09-02 09:00:00,转账,张三,转账,收入,¥100.00,零钱,已存入零钱,4200001234567891,12346,
2026-09-03 10:00:00,商户消费,滴滴,打车,支出,¥15.50,零钱,已关闭,4200001234567892,12347,
2026-09-04 11:00:00,退款,麦当劳,退款,收入,¥5.00,零钱,退款成功,4200001234567893,12348,
`

func TestWechatParse(t *testing.T) {
	rows, err := impexp.ParseFile("wechat", "wx.csv", []byte(wechatSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	if rows[0].Type != "expense" || rows[0].Amount != "28.00" || rows[0].Date != "2026-09-01" || rows[0].TxNo == "" {
		t.Fatalf("row0 = %+v", rows[0])
	}
	// 已关闭不默认入账
	if !rows[2].Skip || rows[2].Type != "expense" {
		t.Fatalf("closed row should be skipped: %+v", rows[2])
	}
	// 退款行保留为收入候选由用户决定（预览中可见，不自动当正常收入——带状态提示）
	if rows[3].Type != "income" {
		t.Fatalf("refund row = %+v", rows[3])
	}
}

func TestGenericParseAndInjectionGuard(t *testing.T) {
	csvText := "日期,类型,金额,分类,商户,备注,单号\n2026-09-01,支出,12.34,餐饮,麦当劳,=HYPERLINK(\"http://evil\"),A001\n"
	rows, err := impexp.ParseFile("generic_csv", "g.csv", []byte(csvText))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != "12.34" || rows[0].TxNo != "generic:A001" {
		t.Fatalf("rows = %+v", rows)
	}
	// 公式注入防护
	out, err := impexp.ExportCSV([]string{"备注"}, [][]string{{rows[0].Note}, {"正常"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "'=HYPERLINK") {
		t.Fatalf("formula not neutralized: %q", s)
	}
}

func TestXLSXRoundTrip(t *testing.T) {
	data, err := impexp.ExportXLSX("流水", []string{"日期", "金额"}, [][]string{{"2026-09-01", "12.34"}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := impexp.ParseFile("generic_csv", "x.xlsx", data)
	// 表头不是通用映射 → 无行（通用映射器只对识别的表头生效）
	if err == nil && len(rows) > 0 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	// 用通用表头再导出可解析
	data2, _ := impexp.ExportXLSX("流水", []string{"日期", "类型", "金额"}, [][]string{{"2026-09-01", "支出", "12.34"}})
	rows2, err := impexp.ParseFile("generic_csv", "x.xlsx", data2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 1 || rows2[0].Amount != "12.34" {
		t.Fatalf("xlsx round trip = %+v", rows2)
	}
}

func TestOversizeRejected(t *testing.T) {
	big := make([]byte, impexp.MaxFileBytes+1)
	if _, err := impexp.ParseFile("generic_csv", "big.csv", big); err == nil {
		t.Fatal("oversize file accepted")
	}
}
