// Package impexp implements CSV/XLSX import (mapping, dedup, preview,
// batch confirm/undo) and export with formula-injection defense.
//
// 兼容声明（诚实口径）：微信/支付宝预设仅对合成样例测试过的格式
// 声明兼容；其他格式走通用映射。失败/关闭/未支付记录不默认入账；
// 退款不默认当收入。
package impexp

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"xiaozhang/internal/money"
)

// 限制（spec 11.1）：文件大小、行数、解析时间。
const (
	MaxFileBytes = 10 << 20 // 10 MB
	MaxRows      = 5000
)

// RawRow 是解析后的待导入行。
type RawRow struct {
	Index    int    `json:"index"`
	Date     string `json:"date"` // YYYY-MM-DD（导入只有日期 → date_precision=day）
	Type     string `json:"type"` // expense | income
	Amount   string `json:"amount"`
	Category string `json:"category,omitempty"`
	Merchant string `json:"merchant,omitempty"`
	Note     string `json:"note,omitempty"`
	TxNo     string `json:"tx_no,omitempty"`
	Status   string `json:"status,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Skip     bool   `json:"skip"`
	SkipWhy  string `json:"skip_reason,omitempty"`
	Err      string `json:"error,omitempty"`
}

// ParseFile 按来源解析上传内容。
func ParseFile(source string, filename string, data []byte) ([]RawRow, error) {
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("file exceeds %d MB limit", MaxFileBytes>>20)
	}
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".xlsx") {
		return parseXLSX(source, data)
	}
	// CSV：微信/支付宝导出常见 GBK，先尝试 UTF-8，失败或乱码标记则转 GBK
	text, err := decodeText(data)
	if err != nil {
		return nil, err
	}
	return parseCSV(source, text)
}

// decodeText：UTF-8（含 BOM）优先；微信/支付宝 GBK 文件转码。
func decodeText(data []byte) (string, error) {
	s := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	if strings.Contains(s, "交易时间") || strings.Contains(s, "日期") {
		return s, nil
	}
	// GBK 尝试
	out, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), string(data))
	if err != nil {
		return "", errors.New("cannot decode file (expect UTF-8 or GBK)")
	}
	return strings.TrimPrefix(out, "\xef\xbb\xbf"), nil
}

func parseCSV(source, text string) ([]RawRow, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv parse: %w", err)
	}
	if len(recs) > MaxRows {
		return nil, fmt.Errorf("more than %d rows", MaxRows)
	}
	switch source {
	case "wechat":
		return mapWechat(recs), nil
	case "alipay":
		return mapAlipay(recs), nil
	default:
		return mapGeneric(recs), nil
	}
}

func parseXLSX(source string, data []byte) ([]RawRow, error) {
	// excelize 不执行公式（取缓存值）；大小受限的输入在 ParseFile 已校验
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("xlsx parse: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("xlsx has no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxRows {
		return nil, fmt.Errorf("more than %d rows", MaxRows)
	}
	switch source {
	case "wechat":
		return mapWechat(rows), nil
	case "alipay":
		return mapAlipay(rows), nil
	default:
		return mapGeneric(rows), nil
	}
}

// ---------------------------------------------------------------------------
// 预设映射（仅对合成样例测试过的格式声明兼容）
// ---------------------------------------------------------------------------

// headerIndex 找到表头行并索引该行全部非空单元格；minMatch 为必须
// 命中的关键列数（预设宽松、通用严格）。
func headerIndex(recs [][]string, want []string, minMatch int) (int, map[string]int) {
	for i, rec := range recs {
		idx := map[string]int{}
		matched := 0
		for j, cell := range rec {
			c := strings.TrimSpace(cell)
			if c == "" {
				continue
			}
			if _, dup := idx[c]; !dup {
				idx[c] = j
			}
			for _, w := range want {
				if c == w {
					matched++
				}
			}
		}
		if matched >= minMatch {
			return i, idx
		}
	}
	return -1, nil
}

var wechatHeaders = []string{"交易时间", "交易对方", "收/支", "金额(元)", "交易单号"}

// mapWechat 微信支付账单（GBK/UTF-8，表头「交易时间…」）。
func mapWechat(recs [][]string) []RawRow {
	hi, idx := headerIndex(recs, wechatHeaders, 2)
	if hi < 0 {
		return nil
	}
	get := func(rec []string, name string) string {
		j, ok := idx[name]
		if !ok || j >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[j])
	}
	var out []RawRow
	for i := hi + 1; i < len(recs); i++ {
		rec := recs[i]
		row := RawRow{Index: i, Channel: "微信支付"}
		row.Date = normalizeDate(get(rec, "交易时间"))
		dir := get(rec, "收/支")
		switch dir {
		case "支出":
			row.Type = "expense"
		case "收入":
			row.Type = "income"
		default:
			row.Skip = true
			row.SkipWhy = "非收支方向（" + dir + "），不默认入账"
		}
		// 金额格式：¥12.34
		amt := strings.TrimPrefix(get(rec, "金额(元)"), "¥")
		if _, err := money.ParseYuanRequired(amt); err != nil {
			row.Err = "金额无法解析: " + amt
		} else {
			row.Amount = amt
		}
		status := get(rec, "当前状态")
		if status != "" && status != "支付成功" && status != "已转账" && status != "对方已收钱" && status != "已存入零钱" {
			row.Skip = true
			row.SkipWhy = "状态「" + status + "」，不默认入账"
		}
		if txNo := get(rec, "交易单号"); txNo != "" {
			row.TxNo = "wechat:" + txNo
		}
		row.Merchant = get(rec, "交易对方")
		row.Note = strings.TrimSpace(get(rec, "商品") + " " + get(rec, "备注"))
		if row.Date == "" && !row.Skip {
			row.Err = "日期无法解析"
		}
		if row.Err == "" || row.Skip {
			out = append(out, row)
		} else {
			out = append(out, row)
		}
	}
	return out
}

var alipayHeaders = []string{"交易时间", "交易对方", "收/支", "金额（元）", "交易号"}

// mapAlipay 支付宝账单（表头「交易时间…金额（元）」）。
func mapAlipay(recs [][]string) []RawRow {
	hi, idx := headerIndex(recs, alipayHeaders, 2)
	if hi < 0 {
		return nil
	}
	get := func(rec []string, name string) string {
		j, ok := idx[name]
		if !ok || j >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[j])
	}
	var out []RawRow
	for i := hi + 1; i < len(recs); i++ {
		rec := recs[i]
		row := RawRow{Index: i, Channel: "支付宝"}
		row.Date = normalizeDate(get(rec, "交易时间"))
		switch get(rec, "收/支") {
		case "支出":
			row.Type = "expense"
		case "收入":
			row.Type = "income"
		default:
			row.Skip = true
			row.SkipWhy = "非收支方向，不默认入账"
		}
		amt := strings.TrimPrefix(get(rec, "金额（元）"), "¥")
		if _, err := money.ParseYuanRequired(amt); err != nil {
			row.Err = "金额无法解析: " + amt
		} else {
			row.Amount = amt
		}
		status := get(rec, "交易状态")
		if status != "" && status != "交易成功" && status != "支付成功" {
			row.Skip = true
			row.SkipWhy = "状态「" + status + "」，不默认入账"
		}
		if txNo := get(rec, "交易号"); txNo != "" {
			row.TxNo = "alipay:" + txNo
		}
		row.Merchant = get(rec, "交易对方")
		row.Note = get(rec, "商品名称")
		out = append(out, row)
	}
	return out
}

var genericHeaders = []string{"日期", "类型", "金额"}

// mapGeneric 通用 CSV：日期,类型(支出/收入),金额,分类,商户,备注,单号
func mapGeneric(recs [][]string) []RawRow {
	hi, idx := headerIndex(recs, genericHeaders, 3)
	if hi < 0 {
		return nil
	}
	get := func(rec []string, name string) string {
		j, ok := idx[name]
		if !ok || j >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[j])
	}
	var out []RawRow
	for i := hi + 1; i < len(recs); i++ {
		rec := recs[i]
		row := RawRow{Index: i}
		row.Date = normalizeDate(get(rec, "日期"))
		switch get(rec, "类型") {
		case "支出", "expense":
			row.Type = "expense"
		case "收入", "income":
			row.Type = "income"
		default:
			row.Skip = true
			row.SkipWhy = "类型无法识别"
		}
		amt := get(rec, "金额")
		if _, err := money.ParseYuanRequired(amt); err != nil {
			row.Err = "金额无法解析: " + amt
		} else {
			row.Amount = amt
		}
		row.Category = get(rec, "分类")
		row.Merchant = get(rec, "商户")
		row.Note = get(rec, "备注")
		if no := get(rec, "单号"); no != "" {
			row.TxNo = "generic:" + no
		}
		out = append(out, row)
	}
	return out
}

// normalizeDate：接受 2026-09-01 12:30:00 / 2026/09/01 / 2026.09.01。
// 导入记录只有日期粒度 → 返回 YYYY-MM-DD（不伪造小时）。
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 导出（防公式注入：以 = + - @ 开头的文本加前缀）
// ---------------------------------------------------------------------------

// SanitizeCell 防止表格公式注入。
func SanitizeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return "'" + s
	}
	return s
}

// ExportCSV 生成交易导出 CSV（口径与界面一致）。
func ExportCSV(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // BOM 便于 Excel 识别 UTF-8
	w := csv.NewWriter(&buf)
	if err := w.Write(headers); err != nil {
		return nil, err
	}
	for _, r := range rows {
		safe := make([]string, len(r))
		for i, c := range r {
			safe[i] = SanitizeCell(c)
		}
		if err := w.Write(safe); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// ExportXLSX 生成交易导出 XLSX。
func ExportXLSX(sheet string, headers []string, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	for j, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(j+1, 1)
		f.SetCellValue(sheet, cell, SanitizeCell(h))
	}
	for i, r := range rows {
		for j, c := range r {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+2)
			f.SetCellValue(sheet, cell, SanitizeCell(c))
		}
	}
	// 删除默认 Sheet1
	if idx, _ := f.GetSheetIndex("Sheet1"); idx >= 0 && sheet != "Sheet1" {
		f.DeleteSheet("Sheet1")
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var _ = io.EOF
