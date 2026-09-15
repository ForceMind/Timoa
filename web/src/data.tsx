import { useEffect, useRef, useState } from 'react'
import type { Account, Category } from './api'

// 我的 → 数据：导入向导（预览 → 确认）、导出、批次管理。
// 全部停留在二级工作区内；预览不入正式账。

interface RawRow {
  index: number
  date: string
  type: string
  amount: string
  merchant?: string
  note?: string
  tx_no?: string
  skip: boolean
  skip_reason?: string
  error?: string
}

interface ImportBatch {
  id: string
  source: string
  filename: string
  total_rows: number
  importable: number
  duplicates: number
  skipped: number
  failed: number
  imported: number
  status: string
  created_at: string
}

export function DataPanel({ accounts, expenseCats, onChanged }: {
  accounts: Account[]; expenseCats: Category[]; onChanged: () => void
}) {
  const [source, setSource] = useState('wechat')
  const [batch, setBatch] = useState<ImportBatch | null>(null)
  const [rows, setRows] = useState<RawRow[]>([])
  const [catID, setCatID] = useState('')
  const [acctID, setAcctID] = useState('')
  const [batches, setBatches] = useState<ImportBatch[]>([])
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const loadBatches = () => fetch('/api/v1/import/batches', { credentials: 'same-origin' })
    .then((r) => r.json()).then((d) => setBatches(d.batches ?? [])).catch(() => {})
  useEffect(() => { loadBatches() }, [])

  async function preview() {
    setErr(''); setMsg('')
    const file = fileRef.current?.files?.[0]
    if (!file) { setErr('请选择文件'); return }
    setBusy(true)
    try {
      const fd = new FormData()
      fd.append('source', source)
      fd.append('file', file)
      const res = await fetch('/api/v1/import/preview', { method: 'POST', body: fd, credentials: 'same-origin' })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error?.message ?? '解析失败')
      setBatch(data.batch)
      setRows(data.rows ?? [])
      if (data.batch.importable === 0) setErr('没有可导入的行（可能全部重复或格式不符）')
    } catch (e) {
      setErr(e instanceof Error ? e.message : '解析失败')
    } finally { setBusy(false) }
  }

  async function confirm() {
    if (!batch) return
    if (!catID || !acctID) { setErr('请选择入账分类与资金账户'); return }
    setBusy(true); setErr('')
    try {
      const res = await fetch('/api/v1/import/confirm', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ batch_id: batch.id, category_id: catID, account_id: acctID }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error?.message ?? '入账失败')
      setMsg(`已入账 ${data.batch.imported} 笔（失败 ${data.batch.failed}）`)
      setBatch(null); setRows([])
      loadBatches(); onChanged()
    } catch (e) {
      setErr(e instanceof Error ? e.message : '入账失败')
    } finally { setBusy(false) }
  }

  async function undo(id: string) {
    setBusy(true); setErr(''); setMsg('')
    try {
      const res = await fetch(`/api/v1/import/batches/${id}/undo`, { method: 'POST', credentials: 'same-origin' })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error?.message ?? '撤销失败')
      setMsg(data.blocked?.length ? `已撤销；${data.blocked.length} 笔因有退款等关联被保留（留痕）` : '批次已撤销（全部留痕冲正）')
      loadBatches(); onChanged()
    } catch (e) {
      setErr(e instanceof Error ? e.message : '撤销失败')
    } finally { setBusy(false) }
  }

  const now = new Date()
  const from = `${now.getFullYear()}-01-01`
  const to = `${now.getFullYear() + 1}-01-01`

  return (
    <div className="panel">
      <h2>数据 <span className="more">预览不入账；单号强去重</span></h2>
      {err && <div className="alert">{err}</div>}
      {msg && <div className="notice">{msg}</div>}

      <div className="date-group">导入账单（CSV / XLSX）</div>
      <div className="field">
        <label>来源（微信/支付宝预设仅对已测试格式声明兼容）</label>
        <select value={source} onChange={(e) => setSource(e.target.value)}>
          <option value="wechat">微信支付账单</option>
          <option value="alipay">支付宝账单</option>
          <option value="generic_csv">通用（日期,类型,金额,…）</option>
        </select>
      </div>
      <div className="field">
        <input ref={fileRef} type="file" accept=".csv,.xlsx" style={{ minHeight: 44 }} />
      </div>
      <button className="btn" disabled={busy} onClick={preview}>{busy ? '解析中…' : '解析并预览'}</button>

      {batch && (
        <>
          <div className="notice" style={{ marginTop: 10 }}>
            共 {batch.total_rows} 行：可导入 {batch.importable} · 重复 {batch.duplicates} · 跳过 {batch.skipped} · 失败 {batch.failed}
          </div>
          <div style={{ maxHeight: 220, overflow: 'auto', marginBottom: 10 }}>
            {rows.slice(0, 50).map((r) => (
              <div className="tx" key={r.index} style={{ opacity: r.skip || r.error ? 0.45 : 1 }}>
                <div className="main">
                  <div className="title">{r.date} · {r.type === 'expense' ? '支出' : '收入'} · ¥{r.amount}</div>
                  <div className="meta">{r.merchant} {r.note} {r.skip_reason ? `· ${r.skip_reason}` : ''} {r.error ? `· ${r.error}` : ''}</div>
                </div>
              </div>
            ))}
          </div>
          <div className="field"><label>入账分类（全部行）</label>
            <select value={catID} onChange={(e) => setCatID(e.target.value)}>
              <option value="">请选择</option>
              {expenseCats.map((c) => <option key={c.id} value={c.id}>{c.parent_id ? '　' : ''}{c.name}</option>)}
            </select></div>
          <div className="field"><label>资金账户</label>
            <select value={acctID} onChange={(e) => setAcctID(e.target.value)}>
              <option value="">请选择</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select></div>
          <button className="btn" disabled={busy || batch.importable === 0} onClick={confirm}>确认导入 {batch.importable} 笔</button>
          <button className="btn-text" onClick={() => { setBatch(null); setRows([]) }}>放弃本批</button>
        </>
      )}

      <div className="date-group">导出</div>
      <div style={{ display: 'flex', gap: 8 }}>
        <a className="btn" style={{ textAlign: 'center', lineHeight: '50px', textDecoration: 'none' }}
          href={`/api/v1/export/transactions?from=${from}&to=${to}&format=csv`} download>导出 CSV</a>
        <a className="btn" style={{ textAlign: 'center', lineHeight: '50px', textDecoration: 'none', background: 'var(--primary-dark)' }}
          href={`/api/v1/export/transactions?from=${from}&to=${to}&format=xlsx`} download>导出 Excel</a>
      </div>

      {batches.length > 0 && (
        <>
          <div className="date-group">导入批次</div>
          {batches.map((b) => (
            <div className="tx" key={b.id}>
              <div className="main">
                <div className="title">{b.filename}</div>
                <div className="meta">{b.created_at.slice(0, 10)} · 入账 {b.imported}/{b.total_rows} · {b.status === 'confirmed' ? '已确认' : b.status === 'undone' ? '已撤销' : '待确认'}</div>
              </div>
              {b.status === 'confirmed' && <button className="btn-text" disabled={busy} onClick={() => undo(b.id)}>撤销</button>}
            </div>
          ))}
        </>
      )}
    </div>
  )
}
