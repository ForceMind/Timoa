import { useEffect, useState } from 'react'
import { api, type CalendarDay, type Forecast } from './api'
import { formatCents } from './money'

// 现金流预测：固定计划与历史估计分开显示，来源可追溯。

export function ForecastPanel() {
  const [f, setF] = useState<Forecast | null>(null)
  useEffect(() => { api.forecast().then(setF).catch(() => {}) }, [])
  if (!f) return null
  return (
    <div className="panel">
      <h2>月底预测 <span className="more">{f.month} · 已过 {f.elapsed_days}/{f.total_days} 天</span></h2>
      <div className="tx"><div className="main"><div className="title">已确认净支出</div></div><div className="amt">¥{formatCents(f.spent_cents)}</div></div>
      <div className="tx"><div className="main"><div className="title">剩余固定费用（周期计划）</div></div><div className="amt">¥{formatCents(f.remaining_fixed_cents)}</div></div>
      <div className="tx"><div className="main"><div className="title">剩余可变费用估计</div>
        <div className="meta">{f.insufficient ? '数据不足 7 天，仅展示已知计划' : '变动日均 × 剩余天数'}</div></div>
        <div className="amt">{f.insufficient ? '—' : `¥${formatCents(f.variable_estimate_cents)}`}</div></div>
      <div className="tx"><div className="main"><div className="title" style={{ fontWeight: 600 }}>预计月底净支出</div></div>
        <div className="amt">¥{formatCents(f.expected_end_expense_cents)}</div></div>
      <div className="tx"><div className="main"><div className="title">当前可用资金</div></div><div className="amt">¥{formatCents(f.available_funds_cents)}</div></div>
      <div className="tx"><div className="main"><div className="title" style={{ fontWeight: 600 }}>预计月底可用资金</div></div>
        <div className="amt" style={{ color: f.gap ? 'var(--expense)' : 'var(--primary-dark)' }}>¥{formatCents(f.expected_end_funds_cents)}</div></div>
      {f.gap && <div className="alert">按当前计划，月底可用资金预计出现缺口。</div>}
      <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.72rem', marginTop: 6 }}>{f.basis}</div>
    </div>
  )
}

// 日历：真实账单与周期事项（含信用卡还款日）分开展示。

export function CalendarPanel({ month }: { month: string }) {
  const [days, setDays] = useState<CalendarDay[]>([])
  useEffect(() => { api.calendar(month).then((r) => setDays(r.days ?? [])).catch(() => {}) }, [month])

  const byDate = new Map(days.map((d) => [d.date, d]))
  const [y, m] = month.split('-').map(Number)
  const first = new Date(y, m - 1, 1)
  const totalDays = new Date(y, m, 0).getDate()
  const startWeekday = (first.getDay() + 6) % 7 // 周一起
  const cells: (CalendarDay | null)[] = []
  for (let i = 0; i < startWeekday; i++) cells.push(null)
  for (let d = 1; d <= totalDays; d++) {
    const key = `${month}-${String(d).padStart(2, '0')}`
    cells.push(byDate.get(key) ?? { date: key, expense_cents: '0', income_cents: '0' })
  }
  const today = new Date().toISOString().slice(0, 10)

  return (
    <div className="panel">
      <h2>日历 <span className="more">{month} · 🗓️ 为周期事项（未入账）</span></h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2, fontSize: '0.72rem' }}>
        {['一', '二', '三', '四', '五', '六', '日'].map((w) => (
          <div key={w} style={{ textAlign: 'center', color: 'var(--muted)', padding: '4px 0' }}>{w}</div>
        ))}
        {cells.map((c, i) => (
          <div key={i} style={{
            minHeight: 46, padding: 3, borderRadius: 8,
            background: c?.date === today ? 'var(--primary-soft)' : 'transparent',
            border: '1px solid var(--line)',
          }}>
            {c && (
              <>
                <div style={{ color: 'var(--muted)' }}>{Number(c.date.slice(8))}</div>
                {BigInt(c.expense_cents) > 0n && <div style={{ color: 'var(--ink)', fontVariantNumeric: 'tabular-nums' }}>-{(Number(c.expense_cents) / 100).toFixed(0)}</div>}
                {BigInt(c.income_cents) > 0n && <div style={{ color: 'var(--income)', fontVariantNumeric: 'tabular-nums' }}>+{(Number(c.income_cents) / 100).toFixed(0)}</div>}
                {c.events?.map((e, j) => (
                  <div key={j} title={`${e.name}${e.status ? `（${e.status}）` : ''}`} style={{ color: 'var(--purple)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    🗓️{e.name}
                  </div>
                ))}
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
