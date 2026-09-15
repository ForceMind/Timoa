import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError, type AssetsOverview, type BudgetStatus, type Category, type CategoryNet, type DailySum, type Overview, type Receivable, type SavingsGoal } from './api'
import { Chart } from './chart'
import { CalendarPanel, ForecastPanel } from './forecast'
import { formatCents, parseYuan } from './money'

// 统计（一级页，页内切换）：概览 / 趋势 / 分类 / 预算 / 资产 / 目标 / 往来。
// 区间支持本月、上月、本季度、本年、自定义；环比为等长上一区间，
// 同比为去年同期；上期为零不显示虚构百分比。

type PeriodKey = 'month' | 'prevMonth' | 'quarter' | 'year' | 'custom'

function periodRange(key: PeriodKey, custom?: { from: string; to: string }): { from: string; to: string; label: string } {
  const now = new Date()
  const fmt = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  switch (key) {
    case 'month':
      return { from: fmt(new Date(now.getFullYear(), now.getMonth(), 1)), to: fmt(new Date(now.getFullYear(), now.getMonth() + 1, 1)), label: `${now.getMonth() + 1}月` }
    case 'prevMonth':
      return { from: fmt(new Date(now.getFullYear(), now.getMonth() - 1, 1)), to: fmt(new Date(now.getFullYear(), now.getMonth(), 1)), label: `${now.getMonth() === 0 ? 12 : now.getMonth()}月（上月）` }
    case 'quarter': {
      const q = Math.floor(now.getMonth() / 3)
      return { from: fmt(new Date(now.getFullYear(), q * 3, 1)), to: fmt(new Date(now.getFullYear(), q * 3 + 3, 1)), label: `Q${q + 1} 季度` }
    }
    case 'year':
      return { from: `${now.getFullYear()}-01-01`, to: `${now.getFullYear() + 1}-01-01`, label: `${now.getFullYear()} 年` }
    case 'custom':
      return { from: custom?.from || fmt(new Date(now.getFullYear(), now.getMonth(), 1)), to: custom?.to || fmt(now), label: '自定义' }
  }
}

function prevPeriod(from: string, to: string): { from: string; to: string } {
  // 等长上一区间（环比）
  const ms = new Date(to).getTime() - new Date(from).getTime()
  return { from: new Date(new Date(from).getTime() - ms).toISOString().slice(0, 10), to: from }
}

function yoyPeriod(from: string, to: string): { from: string; to: string } {
  // 去年同期
  const y = (d: string) => `${Number(d.slice(0, 4)) - 1}${d.slice(4)}`
  return { from: y(from), to: y(to) }
}

function pctChange(cur: string, prev: string): string | null {
  const c = BigInt(cur), p = BigInt(prev)
  if (p <= 0n) return null
  const diff = ((c - p) * 100n) / p
  const abs = diff < 0n ? -diff : diff
  return `${diff >= 0n ? '+' : '-'}${abs}%`
}

export function StatsView({ expenseCats }: { expenseCats: Category[] }) {
  const [pk, setPk] = useState<PeriodKey>('month')
  const [custom, setCustom] = useState({ from: '', to: '' })
  const range = useMemo(() => periodRange(pk, custom), [pk, custom])

  const [ov, setOv] = useState<Overview | null>(null)
  const [mom, setMom] = useState<Overview | null>(null)
  const [yoy, setYoy] = useState<Overview | null>(null)
  const [days, setDays] = useState<DailySum[]>([])
  const [nets, setNets] = useState<CategoryNet[]>([])
  const [basis, setBasis] = useState<'accrual' | 'origin'>('accrual')
  const [budgets, setBudgets] = useState<BudgetStatus[]>([])
  const [assets, setAssets] = useState<AssetsOverview | null>(null)
  const [goals, setGoals] = useState<SavingsGoal[]>([])
  const [recv, setRecv] = useState<Receivable[]>([])
  const [err, setErr] = useState('')

  const monthKey = range.from.slice(0, 7)

  const load = useCallback(async () => {
    try {
      const pp = prevPeriod(range.from, range.to)
      const yp = yoyPeriod(range.from, range.to)
      const [o, mo, yo, d, n, b, a, g, r] = await Promise.all([
        api.overview(range.from, range.to),
        api.overview(pp.from, pp.to),
        api.overview(yp.from, yp.to),
        api.statsDaily(range.from, range.to),
        api.statsCategories(range.from, range.to, basis),
        api.budgetStatus(monthKey),
        api.assets(), api.goals(), api.receivables(),
      ])
      setOv(o); setMom(mo); setYoy(yo); setDays(d.days ?? []); setNets(n.categories ?? [])
      setBudgets(b.budgets ?? []); setAssets(a); setGoals(g.goals ?? [])
      setRecv((r.receivables ?? []).filter((x) => !x.fully_settled))
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [range.from, range.to, basis, monthKey])

  useEffect(() => { load() }, [load])

  const trendOption = useMemo(() => ({
    tooltip: { trigger: 'axis' },
    legend: { data: ['支出', '收入'], bottom: 0, itemWidth: 12, itemHeight: 8, textStyle: { fontSize: 11 } },
    grid: { left: 8, right: 8, top: 16, bottom: 28, containLabel: true },
    xAxis: { type: 'category', data: days.map((d) => d.date.slice(5)), axisLabel: { fontSize: 10 }, axisLine: { lineStyle: { color: '#e8eee9' } } },
    yAxis: { type: 'value', splitLine: { lineStyle: { color: '#f0f4f1' } }, axisLabel: { fontSize: 10 } },
    series: [
      { name: '支出', type: 'bar', data: days.map((d) => (Number(d.expense_cents) / 100).toFixed(2)), itemStyle: { color: '#2fa87c', borderRadius: [3, 3, 0, 0] } },
      { name: '收入', type: 'line', smooth: true, data: days.map((d) => (Number(d.income_cents) / 100).toFixed(2)), itemStyle: { color: '#8b7fd4' }, lineStyle: { width: 2 } },
    ],
  }), [days])

  const pieNets = nets.filter((n) => BigInt(n.net_cents) > 0n)
  const pieOption = useMemo(() => ({
    tooltip: { trigger: 'item', valueFormatter: (v: number) => `¥${v.toFixed(2)}` },
    legend: { bottom: 0, itemWidth: 10, itemHeight: 10, textStyle: { fontSize: 10 }, type: 'scroll' },
    series: [{
      type: 'pie', radius: ['42%', '68%'], center: ['50%', '44%'],
      label: { show: false },
      data: pieNets.map((n) => ({ name: n.category_name, value: Number(n.net_cents) / 100 })),
    }],
  }), [nets]) // eslint-disable-line react-hooks/exhaustive-deps

  const ovRows: [string, keyof Overview][] = [
    ['原收入', 'gross_income_cents'], ['收入退回', 'income_returns_cents'], ['净收入', 'net_income_cents'],
    ['原费用', 'gross_expense_cents'], ['退款', 'refunds_cents'], ['净支出', 'net_expense_cents'], ['收支结余', 'balance_cents'],
  ]

  return (
    <>
      <div className="greet"><h1>统计</h1><div className="sub">{range.label} · 按已确认记录计算</div></div>
      <div className="seg">
        {([['month', '本月'], ['prevMonth', '上月'], ['quarter', '本季度'], ['year', '本年'], ['custom', '自定义']] as const).map(([k, v]) => (
          <button key={k} className={pk === k ? 'on' : ''} onClick={() => setPk(k)}>{v}</button>
        ))}
      </div>
      {pk === 'custom' && (
        <div className="panel" style={{ display: 'flex', gap: 8 }}>
          <input type="date" value={custom.from} onChange={(e) => setCustom({ ...custom, from: e.target.value })} style={{ flex: 1, minHeight: 44, border: '1px solid var(--line)', borderRadius: 10, padding: '0 8px' }} />
          <input type="date" value={custom.to} onChange={(e) => setCustom({ ...custom, to: e.target.value })} style={{ flex: 1, minHeight: 44, border: '1px solid var(--line)', borderRadius: 10, padding: '0 8px' }} />
        </div>
      )}
      {err && <div className="alert">{err}</div>}

      <div className="panel">
        <h2>收支概览 <span className="more">环比等长区间 · 同比去年同期</span></h2>
        {ov && (
          <>
            <div className="tx" style={{ borderBottom: '1px solid var(--line)' }}>
              <div className="main" />
              <div className="meta" style={{ display: 'flex', gap: 16, fontSize: '0.72rem', color: 'var(--muted)' }}>
                <span>金额</span><span>环比</span><span>同比</span>
              </div>
            </div>
            {ovRows.map(([label, key]) => (
              <div className="tx" key={label}>
                <div className="main"><div className="title">{label}</div></div>
                <div className="amt">¥{formatCents(ov[key])}</div>
                <div className="meta" style={{ width: 108, textAlign: 'right', fontVariantNumeric: 'tabular-nums', color: 'var(--muted)', fontSize: '0.78rem' }}>
                  {mom ? (pctChange(ov[key], mom[key]) ?? '—') : '—'} / {yoy ? (pctChange(ov[key], yoy[key]) ?? '—') : '—'}
                </div>
              </div>
            ))}
          </>
        )}
      </div>

      <div className="panel">
        <h2>收支趋势</h2>
        {days.length === 0 ? <div className="empty">本区间还没有记录。</div> : <Chart option={trendOption} />}
      </div>

      <div className="panel">
        <h2>分类构成
          <span className="more">
            <button className="btn-text" style={{ padding: 2, minHeight: 0, color: basis === 'accrual' ? 'var(--primary-dark)' : 'var(--muted)' }} onClick={() => setBasis('accrual')}>发生期</button>
            {' | '}
            <button className="btn-text" style={{ padding: 2, minHeight: 0, color: basis === 'origin' ? 'var(--primary-dark)' : 'var(--muted)' }} onClick={() => setBasis('origin')}>原消费归属</button>
          </span>
        </h2>
        {pieNets.length > 0 && <Chart option={pieOption} height={230} />}
        {nets.length === 0 && <div className="empty">本区间还没有支出。</div>}
        {nets.map((n) => (
          <div className="tx" key={n.category_id}>
            <div className="main"><div className="title">{n.category_name}</div></div>
            <div className="amt">¥{formatCents(n.net_cents)}</div>
          </div>
        ))}
        {nets.some((n) => BigInt(n.net_cents) < 0n) && (
          <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.72rem', marginTop: 6 }}>含负净额分类（退款超过消费），不纳入饼图，以列表为准。</div>
        )}
      </div>

      <BudgetPanel month={monthKey} budgets={budgets} expenseCats={expenseCats} onChanged={load} />

      <ForecastPanel />
      <CalendarPanel month={monthKey} />

      {assets && (
        <div className="panel">
          <h2>资产负债 <span className="more">{assets.scope}</span></h2>
          <div className="tx"><div className="main"><div className="title">已记录资产</div></div><div className="amt">¥{formatCents(assets.asset_cents)}</div></div>
          <div className="tx"><div className="main"><div className="title">已记录负债</div></div><div className="amt">¥{formatCents(assets.liability_cents)}</div></div>
          <div className="tx"><div className="main"><div className="title">应收</div></div><div className="amt">¥{formatCents(assets.receivable_cents)}</div></div>
          <div className="tx"><div className="main"><div className="title">应付</div></div><div className="amt">¥{formatCents(assets.payable_cents)}</div></div>
          <div className="tx"><div className="main"><div className="title" style={{ fontWeight: 600 }}>已记录净资产</div></div>
            <div className="amt" style={{ color: 'var(--primary-dark)' }}>¥{formatCents(assets.net_worth_cents)}</div></div>
          {assets.unconfirmed_count > 0 && <div className="notice">{assets.unconfirmed_count} 个账户期初余额未确认，统计覆盖范围以此为准。</div>}
        </div>
      )}

      <GoalsPanel goals={goals} onChanged={load} />

      {recv.length > 0 && (
        <div className="panel">
          <h2>待收往来</h2>
          {recv.map((r) => (
            <div className="tx" key={r.original_tx_id}>
              <div className="main">
                <div className="title">{r.counterparty || '待报销/代付'}</div>
                <div className="meta">{r.business_date.slice(0, 10)} · 应收 ¥{formatCents(r.created_cents)} · {r.age_days} 天</div>
              </div>
              <div className="amt" style={{ color: 'var(--expense)' }}>¥{formatCents(r.outstanding_cents)}</div>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function BudgetPanel({ month, budgets, expenseCats, onChanged }: {
  month: string; budgets: BudgetStatus[]; expenseCats: Category[]; onChanged: () => void
}) {
  const [showForm, setShowForm] = useState(false)
  const [catID, setCatID] = useState('')
  const [amount, setAmount] = useState('')
  const [err, setErr] = useState('')

  async function submit() {
    const c = parseYuan(amount)
    if (c === null) { setErr('金额格式不正确'); return }
    try {
      await api.setBudget({ month, category_id: catID || undefined, amount: (Number(c) / 100).toFixed(2) })
      setShowForm(false); setAmount('')
      onChanged()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    }
  }

  return (
    <div className="panel">
      <h2>预算 <span className="more">{month} · 退款会让剩余增加</span></h2>
      {budgets.length === 0 && !showForm && <div className="empty">还没有设置预算。</div>}
      {budgets.map((b) => (
        <div className="tx" key={b.id}>
          <div className="main">
            <div className="title">{b.category_name || '总预算'}{b.covers_children ? '（含子分类）' : ''}</div>
            <div style={{ height: 6, background: 'var(--line)', borderRadius: 3, marginTop: 6, overflow: 'hidden' }}>
              <div style={{ width: `${Math.min(100, b.percent)}%`, height: '100%', background: b.over ? 'var(--expense)' : 'var(--primary)', borderRadius: 3 }} />
            </div>
            <div className="meta">已用 ¥{formatCents(b.spent_cents)} / ¥{formatCents(b.amount_cents)} · {b.percent}%{b.over ? ' · 已超支' : ` · 剩余 ¥${formatCents(b.remaining_cents)}`}</div>
          </div>
          <button className="btn-text" onClick={async () => { await api.deleteBudget(b.id); onChanged() }}>删除</button>
        </div>
      ))}
      {err && <div className="alert">{err}</div>}
      {!showForm && <button className="btn-text" onClick={() => setShowForm(true)}>＋ 设置预算</button>}
      {showForm && (
        <>
          <div className="field"><label>范围</label>
            <select value={catID} onChange={(e) => setCatID(e.target.value)}>
              <option value="">总预算（全部费用）</option>
              {expenseCats.filter((c) => !c.parent_id).map((c) => <option key={c.id} value={c.id}>{c.name}（含子分类）</option>)}
            </select></div>
          <div className="field"><label>月预算金额</label>
            <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
          <button className="btn" onClick={submit}>保存预算</button>
          <button className="btn-text" onClick={() => setShowForm(false)}>取消</button>
        </>
      )}
    </div>
  )
}

function GoalsPanel({ goals, onChanged }: { goals: SavingsGoal[]; onChanged: () => void }) {
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState('')
  const [target, setTarget] = useState('')
  const [err, setErr] = useState('')

  async function submit() {
    const c = parseYuan(target)
    if (c === null || !name.trim()) { setErr('请填写名称与正确金额'); return }
    try {
      await api.createGoal({ name: name.trim(), target: (Number(c) / 100).toFixed(2) })
      setShowForm(false); setName(''); setTarget('')
      onChanged()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    }
  }

  return (
    <div className="panel">
      <h2>储蓄目标</h2>
      {goals.length === 0 && !showForm && <div className="empty">还没有储蓄目标。</div>}
      {goals.map((g) => (
        <div className="tx" key={g.id}>
          <div className="main">
            <div className="title">{g.name}{g.done ? ' ✅' : ''}</div>
            <div style={{ height: 6, background: 'var(--line)', borderRadius: 3, marginTop: 6, overflow: 'hidden' }}>
              <div style={{ width: `${Math.min(100, g.percent)}%`, height: '100%', background: 'var(--primary)', borderRadius: 3 }} />
            </div>
            <div className="meta">¥{formatCents(g.saved_cents)} / ¥{formatCents(g.target_cents)} · {g.percent}%{g.target_date ? ` · 目标 ${g.target_date}` : ''}</div>
          </div>
          {!g.done && <button className="btn-text" onClick={async () => { await api.doneGoal(g.id, true); onChanged() }}>完成</button>}
        </div>
      ))}
      {err && <div className="alert">{err}</div>}
      {!showForm && <button className="btn-text" onClick={() => setShowForm(true)}>＋ 新增目标</button>}
      {showForm && (
        <>
          <div className="field"><label>目标名称</label>
            <input placeholder="例如：年底旅行基金" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div className="field"><label>目标金额</label>
            <input inputMode="decimal" placeholder="0.00" value={target} onChange={(e) => setTarget(e.target.value)} /></div>
          <button className="btn" onClick={submit}>保存目标</button>
          <button className="btn-text" onClick={() => setShowForm(false)}>取消</button>
        </>
      )}
    </div>
  )
}
