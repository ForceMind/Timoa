import { useEffect, useState } from 'react'
import { api, ApiError, type Category, type RecurrenceRule, type Template } from './api'
import { formatCents, parseYuan } from './money'

// 我的 → 模板管理与周期管理：新增/启用/置顶均在同页完成（二级页面内
// 行内操作，不叠加第三层）。

export function TemplatesPanel({ onChanged }: { onChanged: () => void }) {
  const [tpls, setTpls] = useState<Template[]>([])
  const [err, setErr] = useState('')

  const load = () => api.templates().then((r) => setTpls(r.templates)).catch((e) => setErr(String(e)))
  useEffect(() => { load() }, [])

  const pinned = tpls.filter((t) => t.enabled && t.pinned)
  const active = tpls.filter((t) => t.enabled && !t.pinned)
  const disabled = tpls.filter((t) => !t.enabled)

  const row = (t: Template) => (
    <div className="tx" key={t.id}>
      <div className="icon" style={{ background: 'var(--primary-soft)' }}>{t.icon || '▫️'}</div>
      <div className="main">
        <div className="title">{t.name}</div>
        <div className="meta">{t.tx_type}{t.category_name ? ` · ${t.category_name}` : ''}{t.is_seed ? ' · 内置' : ''}</div>
      </div>
      <button className="btn-text" onClick={async () => { await api.pinTemplate(t.id, !t.pinned); load(); onChanged() }}>
        {t.pinned ? '取消置顶' : '置顶'}
      </button>
      <button className="btn-text" onClick={async () => { await api.enableTemplate(t.id, !t.enabled); load(); onChanged() }}>
        {t.enabled ? '停用' : '启用'}
      </button>
    </div>
  )

  return (
    <div className="panel">
      <h2>模板管理 <span className="more">置顶模板固定在首页「常用」</span></h2>
      {err && <div className="alert">{err}</div>}
      {pinned.length > 0 && <div className="date-group">已置顶</div>}
      {pinned.map(row)}
      <div className="date-group">启用中</div>
      {active.slice(0, 30).map(row)}
      {active.length > 30 && <div className="empty">还有 {active.length - 30} 个内置模板…</div>}
      {disabled.length > 0 && <div className="date-group">已停用</div>}
      {disabled.map(row)}
    </div>
  )
}

const FREQ_LABEL: Record<string, string> = {
  daily: '每天', weekly: '每周', biweekly: '每两周', monthly: '每月', month_end: '每月末',
  quarterly: '每季度', yearly: '每年', every_n_days: '间隔 N 天',
}

export function RulesPanel({ expenseCats, onChanged }: { expenseCats: Category[]; onChanged: () => void }) {
  const [rules, setRules] = useState<RecurrenceRule[]>([])
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState('')
  const [freq, setFreq] = useState('monthly')
  const [monthDay, setMonthDay] = useState('5')
  const [weekday, setWeekday] = useState('1')
  const [catID, setCatID] = useState('')
  const [fixed, setFixed] = useState('')
  const [err, setErr] = useState('')

  const load = () => api.rules().then((r) => setRules(r.rules)).catch((e) => setErr(String(e)))
  useEffect(() => { load() }, [])

  async function submit() {
    setErr('')
    if (!name.trim()) { setErr('请填写名称'); return }
    const today = new Date().toISOString().slice(0, 10)
    let anchor = today
    const body: Record<string, unknown> = {
      name: name.trim(), tx_type: 'expense', frequency: freq, start_date: today,
      category_id: catID || undefined, amount_policy: fixed.trim() ? 'fixed' : 'manual',
    }
    if (fixed.trim()) {
      const c = parseYuan(fixed)
      if (c === null) { setErr('固定金额格式不正确'); return }
      body.fixed_amount_cents = c
    }
    if (freq === 'monthly') {
      const d = parseInt(monthDay, 10)
      if (isNaN(d) || d < 1 || d > 31) { setErr('每月日期需为 1–31'); return }
      body.month_day = d
      anchor = `${today.slice(0, 8)}${String(d).padStart(2, '0')}`
    }
    if (freq === 'weekly' || freq === 'biweekly') {
      body.by_weekday = parseInt(weekday, 10)
    }
    body.anchor_date = anchor
    try {
      await api.createRule(body)
      setShowForm(false); setName(''); setFixed('')
      load(); onChanged()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    }
  }

  return (
    <div className="panel">
      <h2>周期规则 <span className="more">到期自动生成待确认，不会自动入账</span></h2>
      {err && <div className="alert">{err}</div>}
      {rules.length === 0 && <div className="empty">还没有周期规则，例如「每月 5 日房租」。</div>}
      {rules.map((r) => (
        <div className="tx" key={r.id}>
          <div className="main">
            <div className="title">{r.name}</div>
            <div className="meta">
              {FREQ_LABEL[r.frequency] ?? r.frequency}
              {r.frequency === 'monthly' ? ` ${r.month_day} 日` : ''}
              {(r.frequency === 'weekly' || r.frequency === 'biweekly') ? ` 周${'一二三四五六日'[(r.by_weekday ?? 1) - 1]}` : ''}
              {r.category_name ? ` · ${r.category_name}` : ''}
              {r.fixed_amount_cents ? ` · ¥${formatCents(r.fixed_amount_cents)}` : ''}
            </div>
          </div>
          <button className="btn-text" onClick={async () => { await api.enableRule(r.id, !r.enabled); load(); onChanged() }}>
            {r.enabled ? '停用' : '启用'}
          </button>
        </div>
      ))}
      {!showForm && <button className="btn-text" onClick={() => setShowForm(true)}>＋ 新增周期规则</button>}
      {showForm && (
        <div style={{ marginTop: 8 }}>
          <div className="field"><label>名称</label>
            <input placeholder="例如：房租" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="field"><label>周期</label>
            <select value={freq} onChange={(e) => setFreq(e.target.value)}>
              {Object.entries(FREQ_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select></div>
          {freq === 'monthly' && (
            <div className="field"><label>每月几日（短月自动落月末，下月恢复）</label>
              <input inputMode="numeric" value={monthDay} onChange={(e) => setMonthDay(e.target.value)} /></div>
          )}
          {(freq === 'weekly' || freq === 'biweekly') && (
            <div className="field"><label>星期几</label>
              <select value={weekday} onChange={(e) => setWeekday(e.target.value)}>
                {['一', '二', '三', '四', '五', '六', '日'].map((w, i) => <option key={i} value={i + 1}>周{w}</option>)}
              </select></div>
          )}
          <div className="field"><label>分类（可选）</label>
            <select value={catID} onChange={(e) => setCatID(e.target.value)}>
              <option value="">不指定</option>
              {expenseCats.map((c) => <option key={c.id} value={c.id}>{c.parent_id ? '　' : ''}{c.name}</option>)}
            </select></div>
          <div className="field"><label>固定金额（可选，如房租 2000.00）</label>
            <input inputMode="decimal" placeholder="留空则每次手填" value={fixed} onChange={(e) => setFixed(e.target.value)} /></div>
          <button className="btn" onClick={submit}>创建规则</button>
          <button className="btn-text" onClick={() => setShowForm(false)}>取消</button>
        </div>
      )}
    </div>
  )
}
