import { useCallback, useEffect, useMemo, useState } from 'react'
import { ACCOUNT_TYPES, api, ApiError, type Account, type Category, type CategoryNet, type DailySum, type Me, type Overview, type Receivable, type Summary, type Tx } from './api'
import { brand } from './brand'
import { TxDetailView } from './detail'
import { formatCents, parseYuan, parseYuanAllowZero } from './money'

type View = 'home' | 'txs' | 'entry' | 'stats' | 'me'

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [booting, setBooting] = useState(true)

  useEffect(() => {
    api.me().then(setMe).catch(() => setMe(null)).finally(() => setBooting(false))
  }, [])

  if (booting) return <div className="shell"><p className="empty">加载中…</p></div>
  if (!me) return <Login onLogin={setMe} />
  return <Main me={me} onLogout={() => setMe(null)} />
}

function Login({ onLogin }: { onLogin: (m: Me) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [needsInit, setNeedsInit] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => { api.setupStatus().then((s) => setNeedsInit(s.needs_init)).catch(() => {}) }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      await api.login(username, password)
      onLogin(await api.me())
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '网络错误')
    } finally { setBusy(false) }
  }

  return (
    <div className="login-wrap">
      <div className="logo">账</div>
      <h1>{brand.name}</h1>
      <p className="slogan">{brand.slogan}</p>
      {needsInit && <div className="notice">尚未创建管理员。请在服务器本机执行：xiaozhang init-admin -username &lt;用户名&gt;</div>}
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field"><label htmlFor="u">用户名</label>
          <input id="u" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required /></div>
        <div className="field"><label htmlFor="p">密码</label>
          <input id="p" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required /></div>
        <button className="btn" disabled={busy}>{busy ? '登录中…' : '登录'}</button>
      </form>
    </div>
  )
}

const fmtDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

function useMonth() {
  return useMemo(() => {
    const now = new Date()
    const from = new Date(now.getFullYear(), now.getMonth(), 1)
    const to = new Date(now.getFullYear(), now.getMonth() + 1, 1)
    // 上月同进度区间（同长度，按账本本地日）
    const dayMs = 86400000
    const elapsed = Math.min(Math.floor((now.getTime() - from.getTime()) / dayMs) + 1, 31)
    const prevFrom = new Date(now.getFullYear(), now.getMonth() - 1, 1)
    const prevTo = new Date(prevFrom.getTime() + elapsed * dayMs)
    return {
      from: fmtDay(from), to: fmtDay(to), label: `${now.getMonth() + 1}月`,
      prevFrom: fmtDay(prevFrom), prevTo: fmtDay(prevTo),
    }
  }, [])
}

// 分类 → 图标与底色（本地素材，无外部依赖）
const CAT_STYLE: Record<string, { icon: string; bg: string }> = {
  '餐饮': { icon: '🍜', bg: '#fdeee0' },
  '交通': { icon: '🚌', bg: '#e3effc' },
  '购物': { icon: '🛒', bg: '#f3e8fd' },
  '居住': { icon: '🏠', bg: '#e3f4ec' },
  '其他支出': { icon: '📦', bg: '#f0f1f3' },
  '工资薪酬': { icon: '💼', bg: '#e3f4ec' },
  '其他收入': { icon: '🧧', bg: '#fdeee0' },
}
const catStyle = (name?: string) => (name && CAT_STYLE[name]) || { icon: '💴', bg: '#f0f1f3' }

function Main({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const [view, setView] = useState<View>('home')
  const [detailID, setDetailID] = useState<string | null>(null)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [expenseCats, setExpenseCats] = useState<Category[]>([])
  const [incomeCats, setIncomeCats] = useState<Category[]>([])
  const [txs, setTxs] = useState<Tx[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [prevSummary, setPrevSummary] = useState<Summary | null>(null)
  const [days, setDays] = useState<DailySum[]>([])
  const [err, setErr] = useState('')
  const m = useMonth()

  const reload = useCallback(async () => {
    try {
      const [a, ec, ic, t, s, ps, d] = await Promise.all([
        api.accounts(), api.categories('expense'), api.categories('income'),
        api.transactions(100), api.summary(m.from, m.to), api.summary(m.prevFrom, m.prevTo),
        api.statsDaily(m.from, m.to),
      ])
      setAccounts(a.accounts.filter((x) => !x.archived))
      setExpenseCats(ec.categories.filter((x) => !x.archived))
      setIncomeCats(ic.categories.filter((x) => !x.archived))
      setTxs(t.transactions)
      setSummary(s); setPrevSummary(ps); setDays(d.days)
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [m.from, m.to, m.prevFrom, m.prevTo])

  useEffect(() => { reload() }, [reload])

  const openDetail = (id: string) => setDetailID(id)
  const nav = (v: View) => { setDetailID(null); setView(v) }

  if (accounts.length === 0 && view !== 'me') {
    return (
      <div className="shell">
        <div className="greet"><h1>{me.ledger_name}</h1><div className="sub">{brand.slogan}</div></div>
        {err && <div className="alert" role="alert">{err}</div>}
        <FirstAccount onCreated={reload} />
      </div>
    )
  }

  // 二级页面：账单详情（替换当前工作区，返回保留上下文）
  if (detailID) {
    return (
      <div className="shell">
        <TxDetailView id={detailID} accounts={accounts} onBack={() => setDetailID(null)} onChanged={reload} />
      </div>
    )
  }

  return (
    <div className="shell">
      {err && <div className="alert" role="alert">{err}</div>}
      {view === 'home' && <HomeView me={me} monthLabel={m.label} summary={summary} prevSummary={prevSummary} days={days} txs={txs} onOpen={openDetail} />}
      {view === 'txs' && <TxsView txs={txs} onOpen={openDetail} />}
      {view === 'entry' && (
        <EntryView accounts={accounts} expenseCats={expenseCats} incomeCats={incomeCats}
          onSaved={() => { reload(); setView('home') }} onCancel={() => setView('home')} />
      )}
      {view === 'stats' && <StatsView days={days} monthLabel={m.label} monthFrom={m.from} monthTo={m.to} />}
      {view === 'me' && <MeView me={me} accounts={accounts} onLogout={onLogout} />}

      <nav className="tabbar" aria-label="主导航">
        <button className={view === 'home' ? 'on' : ''} onClick={() => nav('home')}><span className="ti">⌂</span>首页</button>
        <button className={view === 'txs' ? 'on' : ''} onClick={() => nav('txs')}><span className="ti">☰</span>流水</button>
        <button className="fab-wrap" onClick={() => nav('entry')} aria-label="记一笔"><span className="fab">＋</span><span>记一笔</span></button>
        <button className={view === 'stats' ? 'on' : ''} onClick={() => nav('stats')}><span className="ti">▤</span>统计</button>
        <button className={view === 'me' ? 'on' : ''} onClick={() => nav('me')}><span className="ti">☺</span>我的</button>
      </nav>
    </div>
  )
}

function compareBadge(cur: string, prev: string): string | null {
  const c = BigInt(cur), p = BigInt(prev)
  if (p <= 0n) return null // 上期为零：不显示虚构增长
  const diff = ((c - p) * 100n) / p
  const arrow = diff >= 0n ? '↑' : '↓'
  const abs = diff < 0n ? -diff : diff
  return `较上月 ${arrow}${abs}%`
}

function HomeView({ me, monthLabel, summary, prevSummary, days, txs, onOpen }: {
  me: Me; monthLabel: string; summary: Summary | null; prevSummary: Summary | null
  days: DailySum[]; txs: Tx[]; onOpen: (id: string) => void
}) {
  const [tab, setTab] = useState<'all' | 'expense' | 'income' | 'transfer'>('all')
  const filtered = txs.filter((t) => tab === 'all' || t.type === tab).slice(0, 8)
  const badge = summary && prevSummary ? compareBadge(summary.expense_cents, prevSummary.expense_cents) : null

  return (
    <>
      <div className="greet">
        <h1>{brand.greeting}</h1>
        <div className="sub">{me.ledger_name} · {monthLabel}</div>
      </div>

      <div className="hero">
        <div className="row"><span className="label">本月支出</span>
          {badge && <span className="badge">{badge}</span>}</div>
        <div className="big">¥ {summary ? formatCents(summary.expense_cents) : '—'}</div>
        <div className="row">
          <span className="label">结余 {summary ? formatCents(summary.net_cents) : '—'}</span>
          <span className="income"><span className="label">本月收入</span><br /><span className="v">¥ {summary ? formatCents(summary.income_cents) : '—'}</span></span>
        </div>
      </div>

      <div className="seg" role="tablist" aria-label="流水筛选">
        {([['all', '全部'], ['expense', '支出'], ['income', '收入'], ['transfer', '转账']] as const).map(([k, v]) => (
          <button key={k} className={tab === k ? 'on' : ''} onClick={() => setTab(k)}>{v}</button>
        ))}
      </div>

      <div className="panel">
        <h2>本月趋势</h2>
        <Bars days={days} />
      </div>

      <div className="panel">
        <h2>最近记录</h2>
        {filtered.length === 0 && <div className="empty">还没有记录，点下方「记一笔」开始。</div>}
        {filtered.map((t) => <TxRow key={t.id} t={t} onOpen={onOpen} />)}
      </div>
    </>
  )
}

function Bars({ days }: { days: DailySum[] }) {
  if (days.length === 0) return <div className="empty">本月还没有支出记录。</div>
  const max = days.reduce((m, d) => { const v = BigInt(d.expense_cents); return v > m ? v : m }, 0n)
  if (max === 0n) return <div className="empty">本月还没有支出记录。</div>
  const today = fmtDay(new Date())
  const first = days[0].date.slice(5), last = days[days.length - 1].date.slice(5)
  return (
    <>
      <div className="bars" role="img" aria-label="每日支出柱状图">
        {days.map((d) => {
          const v = BigInt(d.expense_cents)
          const h = v === 0n ? 2 : Math.max(4, Number((v * 100n) / max))
          return <div key={d.date} className={`bar ${d.date === today ? 'today' : ''}`} style={{ height: `${h}%` }} title={`${d.date} 支出 ${formatCents(d.expense_cents)}`} />
        })}
      </div>
      <div className="bars-x"><span>{first}</span><span>{last}</span></div>
    </>
  )
}

function TxRow({ t, onOpen }: { t: Tx; onOpen?: (id: string) => void }) {
  const st = catStyle(t.category_name)
  const title = t.type === 'transfer' ? '转账' : t.category_name || '拆分账单'
  const sub = t.type === 'transfer'
    ? `${t.from_account_name} → ${t.to_account_name}`
    : t.type === 'expense' ? `${t.from_account_name}` : `${t.to_account_name}`
  return (
    <div className="tx" onClick={onOpen ? () => onOpen(t.id) : undefined} style={onOpen ? { cursor: 'pointer' } : undefined}>
      <div className="icon" style={{ background: st.bg }}>{t.type === 'transfer' ? '🔁' : st.icon}</div>
      <div className="main">
        <div className="title">{title}{t.note ? ` · ${t.note}` : ''}</div>
        <div className="meta">{t.business_date.slice(0, 10)} · {sub}</div>
      </div>
      <div className={`amt ${t.type}`}>
        {t.type === 'expense' ? '-' : t.type === 'income' ? '+' : ''}¥{formatCents(t.amount_cents)}
      </div>
    </div>
  )
}

function TxsView({ txs, onOpen }: { txs: Tx[]; onOpen: (id: string) => void }) {
  const groups = new Map<string, Tx[]>()
  for (const t of txs) {
    const d = t.business_date.slice(0, 10)
    if (!groups.has(d)) groups.set(d, [])
    groups.get(d)!.push(t)
  }
  return (
    <>
      <div className="greet"><h1>流水</h1><div className="sub">全部已确认记录 · 点按查看详情与退款/更正</div></div>
      {txs.length === 0 && <div className="panel"><div className="empty">还没有账单。</div></div>}
      {[...groups.entries()].map(([d, list]) => (
        <div key={d}>
          <div className="date-group">{d}</div>
          <div className="panel">{list.map((t) => <TxRow key={t.id} t={t} onOpen={onOpen} />)}</div>
        </div>
      ))}
    </>
  )
}

function StatsView({ days, monthLabel, monthFrom, monthTo }: {
  days: DailySum[]; monthLabel: string; monthFrom: string; monthTo: string
}) {
  const [ov, setOv] = useState<Overview | null>(null)
  const [nets, setNets] = useState<CategoryNet[]>([])
  const [basis, setBasis] = useState<'accrual' | 'origin'>('accrual')
  const [recv, setRecv] = useState<Receivable[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    Promise.all([api.overview(monthFrom, monthTo), api.receivables()])
      .then(([o, r]) => { setOv(o); setRecv(r.receivables.filter((x) => !x.fully_settled)) })
      .catch((e) => setErr(e instanceof ApiError ? e.message : '加载失败'))
  }, [monthFrom, monthTo])

  useEffect(() => {
    api.statsCategories(monthFrom, monthTo, basis).then((r) => setNets(r.categories))
      .catch(() => setNets([]))
  }, [monthFrom, monthTo, basis])

  const rows: [string, string][] = ov ? [
    ['原收入', ov.gross_income_cents],
    ['收入退回', ov.income_returns_cents],
    ['净收入', ov.net_income_cents],
    ['原费用', ov.gross_expense_cents],
    ['退款', ov.refunds_cents],
    ['净支出', ov.net_expense_cents],
    ['收支结余', ov.balance_cents],
  ] : []

  return (
    <>
      <div className="greet"><h1>统计</h1><div className="sub">{monthLabel} · 按已确认记录计算</div></div>
      {err && <div className="alert">{err}</div>}
      <div className="panel">
        <h2>收支概览</h2>
        {rows.map(([label, v]) => (
          <div className="tx" key={label}>
            <div className="main"><div className="title">{label}</div></div>
            <div className="amt">¥{formatCents(v)}</div>
          </div>
        ))}
      </div>
      <div className="panel">
        <h2>每日支出</h2>
        <Bars days={days} />
      </div>
      <div className="panel">
        <h2>分类净额
          <span className="more">
            <button className="btn-text" style={{ padding: 2, minHeight: 0, color: basis === 'accrual' ? 'var(--primary-dark)' : 'var(--muted)' }} onClick={() => setBasis('accrual')}>发生期</button>
            {' | '}
            <button className="btn-text" style={{ padding: 2, minHeight: 0, color: basis === 'origin' ? 'var(--primary-dark)' : 'var(--muted)' }} onClick={() => setBasis('origin')}>原消费归属</button>
          </span>
        </h2>
        {nets.length === 0 && <div className="empty">本月还没有支出。</div>}
        {nets.map((n) => (
          <div className="tx" key={n.category_id}>
            <div className="main"><div className="title">{n.category_name}</div></div>
            <div className="amt">¥{formatCents(n.net_cents)}</div>
          </div>
        ))}
        <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.72rem', marginTop: 8 }}>
          {basis === 'accrual' ? '发生期口径：退款按退款发生日计入所在期间。' : '原消费归属口径：退款归到原消费所在期间，截至当前更正后结果。'}
        </div>
      </div>
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
      <div className="notice">预算、资产负债与现金流预测将在统计阶段完整提供。</div>
    </>
  )
}

function MeView({ me, accounts, onLogout }: { me: Me; accounts: Account[]; onLogout: () => void }) {
  return (
    <>
      <div className="greet"><h1>我的</h1><div className="sub">{me.display_name} · {me.role === 'admin' ? '管理员' : '成员'}</div></div>
      <div className="panel">
        <h2>资金账户 <span className="more">按已录入记录计算</span></h2>
        {accounts.map((a) => (
          <div className="tx" key={a.id}>
            <div className="icon" style={{ background: 'var(--primary-soft)' }}>💳</div>
            <div className="main">
              <div className="title">{a.name}</div>
              <div className="meta">{ACCOUNT_TYPES[a.type] ?? a.type}{a.balance_unconfirmed ? ' · 余额未确认' : ''}</div>
            </div>
            <div className="amt">¥{formatCents(a.balance_cents)}</div>
          </div>
        ))}
        {accounts.length === 0 && <div className="empty">还没有账户。</div>}
      </div>
      <div className="panel">
        <h2>关于</h2>
        <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.85rem', lineHeight: 1.8 }}>
          {brand.name} · {brand.slogan}<br />数据保存在你自己的服务器上。
        </div>
      </div>
      <button className="btn" style={{ background: 'var(--card)', color: 'var(--expense)' }}
        onClick={async () => { await api.logout(); onLogout() }}>退出登录</button>
    </>
  )
}

function FirstAccount({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [type, setType] = useState('bank_card')
  const [opening, setOpening] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    const cents = parseYuanAllowZero(opening)
    if (cents === null) { setErr('期初余额格式不正确'); return }
    const c = BigInt(cents)
    setBusy(true); setErr('')
    try {
      await api.createAccount({
        name, type,
        opening_balance: `${c / 100n}.${(c % 100n).toString().padStart(2, '0')}`,
        balance_confirmed: opening.trim() !== '',
      })
      onCreated()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h2>建立第一个资金账户</h2>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field"><label htmlFor="an">账户名称</label>
          <input id="an" value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：招商银行卡" required /></div>
        <div className="field"><label htmlFor="at">类型</label>
          <select id="at" value={type} onChange={(e) => setType(e.target.value)}>
            {Object.entries(ACCOUNT_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </select></div>
        <div className="field"><label htmlFor="ao">期初余额（可暂不填写）</label>
          <input id="ao" inputMode="decimal" value={opening} onChange={(e) => setOpening(e.target.value)} placeholder="0.00" /></div>
        <button className="btn" disabled={busy}>{busy ? '创建中…' : '创建账户'}</button>
      </form>
    </div>
  )
}

function EntryView({ accounts, expenseCats, incomeCats, onSaved, onCancel }: {
  accounts: Account[]; expenseCats: Category[]; incomeCats: Category[]
  onSaved: () => void; onCancel: () => void
}) {
  const [type, setType] = useState<'expense' | 'income' | 'transfer'>('expense')
  const [amount, setAmount] = useState('')
  const [catID, setCatID] = useState('')
  const [fromID, setFromID] = useState('')
  const [toID, setToID] = useState('')
  const [note, setNote] = useState('')
  const [more, setMore] = useState(false)
  const [advance, setAdvance] = useState('') // 代付/垫付金额
  const [counterparty, setCounterparty] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const cats = type === 'income' ? incomeCats : expenseCats

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    const cents = parseYuan(amount)
    if (cents === null) { setErr('请输入正确金额（最多两位小数，不能为 0）'); return }
    if (type !== 'transfer' && !catID) { setErr('请选择分类'); return }
    if ((type === 'expense' || type === 'transfer') && !fromID) { setErr('请选择付款账户'); return }
    if ((type === 'income' || type === 'transfer') && !toID) { setErr('请选择收款账户'); return }
    if (type === 'transfer' && fromID === toID) { setErr('转账账户不能相同'); return }

    // 代付拆分：总额 = 自担费用 + 垫付应收
    let splits: unknown
    if (type === 'expense' && advance.trim() !== '') {
      const adv = parseYuan(advance)
      if (adv === null) { setErr('垫付金额格式不正确'); return }
      if (!counterparty.trim()) { setErr('请填写垫付对象（如：朋友、同事）'); return }
      const self = BigInt(cents) - BigInt(adv)
      if (self <= 0n) { setErr('垫付金额必须小于总金额'); return }
      const to2 = (v: bigint) => `${v / 100n}.${(v % 100n).toString().padStart(2, '0')}`
      splits = [
        { part_type: 'expense', category_id: catID, amount: to2(self) },
        { part_type: 'receivable', counterparty: counterparty.trim(), amount: to2(BigInt(adv)) },
      ]
    }

    setBusy(true)
    try {
      const [i, f = ''] = amount.trim().split('.')
      await api.post({
        type, business_date: new Date().toISOString(), amount: `${i}.${f.padEnd(2, '0')}`,
        category_id: type === 'transfer' || splits ? undefined : catID,
        splits,
        from_account_id: fromID || undefined, to_account_id: toID || undefined,
        note: note || undefined, operation_id: crypto.randomUUID(),
      })
      onSaved()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败'); setBusy(false)
    }
  }

  return (
    <>
      <div className="greet" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h1>记一笔</h1>
        <button className="btn-text" onClick={onCancel}>取消</button>
      </div>
      <div className="seg" role="tablist">
        {([['expense', '支出'], ['income', '收入'], ['transfer', '转账']] as const).map(([k, v]) => (
          <button key={k} className={type === k ? 'on' : ''} onClick={() => { setType(k); setCatID('') }}>{v}</button>
        ))}
      </div>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="panel">
          <div className="amount-row">
            <span>¥</span>
            <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} aria-label="金额" autoFocus />
          </div>
          {type !== 'transfer' && (
            <div className="chips">
              {cats.map((c) => {
                const st = catStyle(c.name)
                return (
                  <button type="button" key={c.id} className={`chip ${catID === c.id ? 'on' : ''}`} onClick={() => setCatID(c.id)}>
                    <span className="ic" style={{ background: st.bg }}>{st.icon}</span><span>{c.name}</span>
                  </button>
                )
              })}
            </div>
          )}
          {(type === 'expense' || type === 'transfer') && (
            <div className="field"><label>{type === 'expense' ? '付款账户' : '转出账户'}</label>
              <select value={fromID} onChange={(e) => setFromID(e.target.value)}>
                <option value="">请选择</option>
                {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
              </select></div>
          )}
          {(type === 'income' || type === 'transfer') && (
            <div className="field"><label>{type === 'income' ? '收款账户' : '转入账户'}</label>
              <select value={toID} onChange={(e) => setToID(e.target.value)}>
                <option value="">请选择</option>
                {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
              </select></div>
          )}

          {type === 'expense' && (
            <>
              <button type="button" className="btn-text" onClick={() => setMore(!more)}>
                {more ? '收起 ▴' : '更多（备注 · 代付）▾'}
              </button>
              {more && (
                <>
                  <div className="field"><label>备注（可选）</label>
                    <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="一句话说明" /></div>
                  <div className="field"><label>含代付/垫付金额（可选，将形成应收）</label>
                    <input inputMode="decimal" placeholder="0.00" value={advance} onChange={(e) => setAdvance(e.target.value)} /></div>
                  {advance.trim() !== '' && (
                    <div className="field"><label>垫付对象</label>
                      <input placeholder="例如：朋友小李" value={counterparty} onChange={(e) => setCounterparty(e.target.value)} /></div>
                  )}
                </>
              )}
            </>
          )}
          {type !== 'expense' && (
            <div className="field"><label>备注（可选）</label>
              <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="一句话说明" /></div>
          )}
          <button className="btn" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
        </div>
      </form>
    </>
  )
}
