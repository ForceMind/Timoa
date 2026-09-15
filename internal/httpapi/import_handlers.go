package httpapi

import (
	"io"
	"net/http"
	"strings"

	"xiaozhang/internal/impexp"
)

// import_handlers.go: 导入（预览/确认/撤销/批次）、导出、附件。

func (s *server) importPreview(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := r.ParseMultipartForm(impexp.MaxFileBytes); err != nil {
		writeErr(w, 400, "invalid_input", "multipart form required")
		return
	}
	source := r.FormValue("source")
	if source != "wechat" && source != "alipay" && source != "generic_csv" {
		writeErr(w, 400, "invalid_input", "source must be wechat|alipay|generic_csv")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "invalid_input", "file required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, impexp.MaxFileBytes+1))
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := impexp.ParseFile(source, hdr.Filename, data)
	if err != nil {
		writeErr(w, 400, "parse_error", err.Error())
		return
	}
	b, rows, err := s.ledger.PreviewImport(m.ledgerID, actorID(r), source, hdr.Filename, rows)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": b, "rows": rows})
}

func (s *server) importConfirm(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BatchID    string `json:"batch_id"`
		CategoryID string `json:"category_id"`
		AccountID  string `json:"account_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.CategoryID == "" || body.AccountID == "" {
		writeErr(w, 400, "invalid_input", "category_id and account_id required")
		return
	}
	b, err := s.ledger.ConfirmImport(m.ledgerID, actorID(r), body.BatchID, body.CategoryID, body.AccountID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": b})
}

func (s *server) importBatches(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	bs, err := s.ledger.ListImportBatches(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": bs})
}

func (s *server) importUndo(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	b, blocked, err := s.ledger.UndoImportBatch(m.ledgerID, actorID(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": b, "blocked": blocked})
}

// ---------------------------------------------------------------------------
// 导出（口径与界面一致；防公式注入）
// ---------------------------------------------------------------------------

var exportHeaders = []string{"日期", "类型", "原金额", "净额", "账户", "分类", "渠道", "商户", "备注", "对方", "状态", "来源单号"}

func (s *server) exportTx(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	if from == "" || to == "" {
		writeErr(w, 400, "invalid_input", "from and to are required")
		return
	}
	rows, err := s.ledger.ExportRows(m.ledgerID, from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	format := q.Get("format")
	filename := "xiaozhang-" + from + "_" + to
	switch format {
	case "xlsx":
		data, err := impexp.ExportXLSX("流水", exportHeaders, rows)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", "attachment; filename="+filename+".xlsx")
		w.Write(data)
	default:
		data, err := impexp.ExportCSV(exportHeaders, rows)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+filename+".csv")
		w.Write(data)
	}
}

// ---------------------------------------------------------------------------
// 附件
// ---------------------------------------------------------------------------

func (s *server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeErr(w, 400, "invalid_input", "multipart form required")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "invalid_input", "file required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 5<<20+1))
	if err != nil {
		writeError(w, err)
		return
	}
	name := hdr.Filename
	if i := strings.LastIndex(name, "\\"); i >= 0 { // IE 全路径兜底
		name = name[i+1:]
	}
	a, err := s.ledger.SaveAttachment(s.cfg.DataDir, m.ledgerID, actorID(r), r.PathValue("id"), name, data)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *server) listAttachments(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	as, err := s.ledger.ListAttachments(m.ledgerID, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": as})
}

func (s *server) serveAttachment(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	path, ct, err := s.ledger.AttachmentPath(s.cfg.DataDir, m.ledgerID, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeFile(w, r, path)
}
