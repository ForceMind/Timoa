import { useEffect, useRef, useState } from 'react'

// 凭证附件：图片 / PDF，魔数校验，授权接口访问。

interface Attachment {
  id: string
  file_name: string
  content_type: string
  size: number
  created_at: string
}

export function AttachmentsPanel({ txID }: { txID: string }) {
  const [items, setItems] = useState<Attachment[]>([])
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = () => fetch(`/api/v1/transactions/${txID}/attachments`, { credentials: 'same-origin' })
    .then((r) => r.json()).then((d) => setItems(d.attachments ?? [])).catch(() => {})
  useEffect(() => { load() }, [txID])

  async function upload() {
    setErr('')
    const file = fileRef.current?.files?.[0]
    if (!file) { setErr('请选择图片或 PDF 文件'); return }
    setBusy(true)
    try {
      const fd = new FormData()
      fd.append('file', file)
      const res = await fetch(`/api/v1/transactions/${txID}/attachments`, { method: 'POST', body: fd, credentials: 'same-origin' })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error?.message ?? '上传失败')
      if (fileRef.current) fileRef.current.value = ''
      load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : '上传失败')
    } finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h2>凭证附件 <span className="more">图片 / PDF，最多 5 个</span></h2>
      {err && <div className="alert">{err}</div>}
      {items.map((a) => (
        <div className="tx" key={a.id}>
          <div className="icon" style={{ background: 'var(--primary-soft)' }}>{a.content_type === 'application/pdf' ? '📄' : '🖼️'}</div>
          <div className="main">
            <div className="title">{a.file_name}</div>
            <div className="meta">{(a.size / 1024).toFixed(0)} KB</div>
          </div>
          <a className="btn-text" href={`/api/v1/attachments/${a.id}`} target="_blank" rel="noreferrer">查看</a>
        </div>
      ))}
      {items.length === 0 && <div className="empty">还没有附件。</div>}
      {items.length < 5 && (
        <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
          <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp,application/pdf" style={{ flex: 1, minHeight: 44 }} />
          <button className="btn" style={{ width: 'auto', padding: '0 20px' }} disabled={busy} onClick={upload}>{busy ? '上传中…' : '上传'}</button>
        </div>
      )}
    </div>
  )
}
