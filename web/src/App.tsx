import { useCallback, useEffect, useMemo, useState } from 'react'
import { ACCOUNT_TYPES, api, ApiError, type Account, type Category, type DailySum, type Me, type Recommendation, type RecurrenceInstance, type Summary, type Template, type Tx } from './api'
import { brand } from './brand'
import { DataPanel } from './data'
import { TxDetailView } from './detail'
import { AmortPanel, NotesPanel, RulesPanel, TemplatesPanel } from './manage'
import { MembersPanel } from './members'
import { formatCents, parseYuan, parseYuanAllowZero } from './money'
import { StatsView } from './stats'
import { sync, type SyncState } from './sync'
import { exportOutbox } from './db'
import { Landing } from './landing'
import { AdminPanel } from './admin'
import { OpsPanel } from './opspanel'
import { getFontSizePref, getThemePref, isDarkNow, setFontSizePref, setThemePref, subscribeTheme, type FontSizePref, type ThemePref } from './theme'
import { installState, promptInstall, subscribeInstall, type InstallState } from './pwa'

type View = 'home' | 'txs' | 'entry' | 'stats' | 'me'

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [booting, setBooting] = useState(true)
  const [joinToken] = useState(() => new URLSearchParams(window.location.search).get('join'))
  const [showLogin, setShowLogin] = useState(false)
  // 运营面板随机路径：形如 /ops-x7k9p2qm，命中则渲染独立面板（不走普通应用壳）
  const [opsPath] = useState(() => {
    const m = window.location.pathname.match(/^\/(ops-[a-f0-9]{8})\/?$/)
    return m ? m[1] : ''
  })

  useEffect(() => {
    api.me().then(setMe).catch(() => setMe(null)).finally(() => setBooting(false))
  }, [])

  if (opsPath) return <OpsPanel opsPath={opsPath} LoginView={(p) => <Login onLogin={p.onLogin} />} />
  if (booting) return <div className="shell"><p className="empty">加载中…</p></div>
  if (!me) {
    if (joinToken) return <JoinView token={joinToken} onJoined={setMe} />
    if (showLogin) return <Login onLogin={setMe} onBack={() => setShowLogin(false)} />
    return <Landing onLogin={setMe} onGoLogin={() => setShowLogin(true)} />
  }
  // 首次部署的初始超管：登录后必须先改初始密码，否则不进入应用
  if (me.must_change_password) {
    return <ForceChangePassword onDone={async () => setMe(await api.me())} />
  }
  return <Main me={me} onLogout={() => setMe(null)} />
}

// ForceChangePassword 强制改密框（不可关闭）：初始随机密码登录后必须设置新密码。
function ForceChangePassword({ onDone }: { onDone: () => void }) {
  const [oldPwd, setOldPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')
  const [newPwd2, setNewPwd2] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (newPwd.length < 8) { setErr('新密码至少 8 位'); return }
    if (newPwd !== newPwd2) { setErr('两次输入的新密码不一致'); return }
    setBusy(true); setErr('')
    try {
      await api.changePassword(oldPwd, newPwd)
      onDone()
    } catch (e2) {
      setErr(e2 instanceof ApiError ? e2.message : '修改失败')
    } finally { setBusy(false) }
  }

  return (
    <div className="login-wrap">
      <h1>设置新密码</h1>
      <p className="slogan">首次登录，请先修改初始密码</p>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field"><label htmlFor="op">初始密码</label>
          <input id="op" type="password" value={oldPwd} onChange={(e) => setOldPwd(e.target.value)} autoComplete="current-password" required /></div>
        <div className="field"><label htmlFor="np">新密码（至少 8 位）</label>
          <input id="np" type="password" value={newPwd} onChange={(e) => setNewPwd(e.target.value)} autoComplete="new-password" required /></div>
        <div className="field"><label htmlFor="np2">确认新密码</label>
          <input id="np2" type="password" value={newPwd2} onChange={(e) => setNewPwd2(e.target.value)} autoComplete="new-password" required /></div>
        <button className="btn" type="submit" disabled={busy}>{busy ? '提交中…' : '确认修改'}</button>
      </form>
    </div>
  )
}

function JoinView({ token, onJoined }: { token: string; onJoined: (m: Me) => void }) {
  const [username, setUsername] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      await api.join({ token, username, password, display_name: name || undefined })
      window.history.replaceState({}, '', '/')
      onJoined(await api.me())
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加入失败（邀请可能已使用或过期）')
    } finally { setBusy(false) }
  }

  return (
    <div className="login-wrap">
      <img src="/icons/icon-192.png" alt="Timoa" style={{ width: 72, height: 72, borderRadius: 18, marginBottom: 14 }} />
      <h1>{brand.name}</h1>
      <p className="slogan">你受邀加入家庭账本。设置你的账号即可开始共同记账。</p>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field"><label htmlFor="jn">昵称</label>
          <input id="jn" value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：妈妈" /></div>
        <div className="field"><label htmlFor="ju">用户名</label>
          <input id="ju" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required /></div>
        <div className="field"><label htmlFor="jp">密码（至少 8 位）</label>
          <input id="jp" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} /></div>
        <button className="btn" disabled={busy}>{busy ? '加入中…' : '加入账本'}</button>
      </form>
    </div>
  )
}

function Login({ onLogin, onBack }: { onLogin: (m: Me) => void; onBack?: () => void }) {
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
      <img src="/icons/icon-192.png" alt="Timoa" style={{ width: 72, height: 72, borderRadius: 18, marginBottom: 14 }} />
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
        {onBack && <button type="button" className="btn-text" onClick={onBack}>返回首页</button>}
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

// 分类 → 图标与底色（本地素材，无外部依赖）；深浅两套底色按当前主题取值。
const CAT_STYLE: Record<string, { icon: string; bg: string; bgDark: string }> = {
  '餐饮': { icon: '🍜', bg: '#fdeee0', bgDark: '#3d2f1e' },
  '交通': { icon: '🚌', bg: '#e3effc', bgDark: '#1e3242' },
  '购物': { icon: '🛒', bg: '#f3e8fd', bgDark: '#33244a' },
  '居住': { icon: '🏠', bg: '#e3f4ec', bgDark: '#1f3a2e' },
  '其他支出': { icon: '📦', bg: '#f0f1f3', bgDark: '#2a322d' },
  '工资薪酬': { icon: '💼', bg: '#e3f4ec', bgDark: '#1f3a2e' },
  '其他收入': { icon: '🧧', bg: '#fdeee0', bgDark: '#3d2f1e' },
}
const catStyle = (name?: string) => {
  const st = (name && CAT_STYLE[name]) || { icon: '💴', bg: '#f0f1f3', bgDark: '#2a322d' }
  return { icon: st.icon, bg: isDarkNow() ? st.bgDark : st.bg }
}

function Main({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const [view, setView] = useState<View>('home')
  const [detailID, setDetailID] = useState<string | null>(null)
  const [prefill, setPrefill] = useState<EntryPrefill | null>(null)
  const [skipAccount, setSkipAccount] = useState(false)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [expenseCats, setExpenseCats] = useState<Category[]>([])
  const [incomeCats, setIncomeCats] = useState<Category[]>([])
  const [txs, setTxs] = useState<Tx[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [prevSummary, setPrevSummary] = useState<Summary | null>(null)
  const [days, setDays] = useState<DailySum[]>([])
  const [templates, setTemplates] = useState<Template[]>([])
  const [pending, setPending] = useState<RecurrenceInstance[]>([])
  const [recs, setRecs] = useState<Recommendation[]>([])
  const [err, setErr] = useState('')
  const m = useMonth()

  const reload = useCallback(async () => {
    try {
      const [a, ec, ic, t, s, ps, d, tp, pd, rc] = await Promise.all([
        api.accounts(), api.categories('expense'), api.categories('income'),
        api.transactions(100), api.summary(m.from, m.to), api.summary(m.prevFrom, m.prevTo),
        api.statsDaily(m.from, m.to), api.templates(), api.pending(), api.recommendations(),
      ])
      setAccounts((a.accounts ?? []).filter((x) => !x.archived))
      setExpenseCats((ec.categories ?? []).filter((x) => !x.archived))
      setIncomeCats((ic.categories ?? []).filter((x) => !x.archived))
      setTxs(t.transactions ?? [])
      setSummary(s); setPrevSummary(ps); setDays(d.days ?? [])
      setTemplates(tp.templates ?? [])
      setPending(pd.instances ?? [])
      setRecs(rc.recommendations ?? [])
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [m.from, m.to, m.prevFrom, m.prevTo])

  useEffect(() => { reload() }, [reload])

  const openDetail = (id: string) => setDetailID(id)
  const nav = (v: View) => { setDetailID(null); setView(v) }
  const startEntry = (p: EntryPrefill | null) => { setPrefill(p); setView('entry') }

  if (accounts.length === 0 && !skipAccount && view !== 'me') {
    return (
      <div className="shell">
        <div className="greet"><h1>{me.ledger_name}</h1><div className="sub">{brand.slogan}</div></div>
        {err && <div className="alert" role="alert">{err}</div>}
        <FirstAccount onCreated={reload} />
        <button className="btn-text" style={{ width: '100%' }} onClick={() => setSkipAccount(true)}>
          先跳过，直接开始记账（可稍后在「我的 → 资金账户」添加）
        </button>
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
      {view === 'home' && (
        <HomeView me={me} monthLabel={m.label} summary={summary} prevSummary={prevSummary} days={days} txs={txs}
          pending={pending} recs={recs} templates={templates} accounts={accounts}
          onOpen={openDetail} onEntry={startEntry} onChanged={reload} />
      )}
      {view === 'txs' && <TxsView txs={txs} onOpen={openDetail} />}
      {view === 'entry' && (
        <EntryView accounts={accounts} expenseCats={expenseCats} incomeCats={incomeCats} prefill={prefill}
          onSaved={(again) => { reload(); if (!again) setView('home'); setPrefill(null) }}
          onCancel={() => { setView('home'); setPrefill(null) }} />
      )}
      {view === 'stats' && <StatsView expenseCats={expenseCats} />}
      {view === 'me' && <MeView me={me} accounts={accounts} expenseCats={expenseCats} onLogout={onLogout} onChanged={reload} />}
      <nav className="tabbar" aria-label="主导航">
        <button className={view === 'home' ? 'on' : ''} onClick={() => nav('home')}><img src="/icons/nav-home.png" alt="" className="ti" />首页</button>
        <button className={view === 'txs' ? 'on' : ''} onClick={() => nav('txs')}><img src="/icons/nav-list.png" alt="" className="ti" />流水</button>
        <button className="fab-wrap" onClick={() => startEntry(null)} aria-label="记一笔"><img src="/icons/nav-plus.png" alt="" className="fab-img" /><span>记一笔</span></button>
        <button className={view === 'stats' ? 'on' : ''} onClick={() => nav('stats')}><img src="/icons/nav-stats.png" alt="" className="ti" />统计</button>
        <button className={view === 'me' ? 'on' : ''} onClick={() => nav('me')}><img src="/icons/nav-me.png" alt="" className="ti" />我的</button>
      </nav>
    </div>
  )
}

export interface EntryPrefill {
  type?: 'expense' | 'income' | 'transfer'
  categoryID?: string
  amountYuan?: string
  fromID?: string
  toID?: string
  note?: string
  instanceID?: string
}

function compareBadge(cur: string, prev: string): string | null {
  const c = BigInt(cur), p = BigInt(prev)
  if (p <= 0n) return null // 上期为零：不显示虚构增长
  const diff = ((c - p) * 100n) / p
  const arrow = diff >= 0n ? '↑' : '↓'
  const abs = diff < 0n ? -diff : diff
  return `较上月 ${arrow}${abs}%`
}

function HomeView({ me, monthLabel, summary, prevSummary, days, txs, pending, recs, templates, accounts, onOpen, onEntry, onChanged }: {
  me: Me; monthLabel: string; summary: Summary | null; prevSummary: Summary | null
  days: DailySum[]; txs: Tx[]
  pending: RecurrenceInstance[]; recs: Recommendation[]; templates: Template[]
  accounts: Account[]
  onOpen: (id: string) => void
  onEntry: (p: EntryPrefill) => void
  onChanged: () => void
}) {
  const [tab, setTab] = useState<'all' | 'expense' | 'income' | 'transfer'>('all')
  const filtered = txs.filter((t) => tab === 'all' || t.type === tab).slice(0, 8)
  const badge = summary && prevSummary ? compareBadge(summary.expense_cents, prevSummary.expense_cents) : null
  const pinnedTpls = templates.filter((t) => t.enabled && t.pinned).slice(0, 8)
  // 储值卡到期提醒（7 天内到期且有余额）
  const expiringSV = accounts.filter((a) =>
    a.type === 'stored_value' && a.expires_on && a.balance_cents !== '0' &&
    new Date(a.expires_on) <= new Date(Date.now() + 7 * 86400000))

  const yuan = (cents?: string) => cents ? (Number(cents) / 100).toFixed(2) : undefined

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

      {expiringSV.length > 0 && (
        <div className="panel">
          <h2>储值卡到期提醒</h2>
          {expiringSV.map((a) => (
            <div className="tx" key={a.id}>
              <div className="icon" style={{ background: 'var(--primary-soft)' }}>⏰</div>
              <div className="main">
                <div className="title">{a.name}</div>
                <div className="meta">{a.expires_on} 到期 · 余额 ¥{formatCents(a.balance_cents)}</div>
              </div>
            </div>
          ))}
        </div>
      )}

      {pending.length > 0 && (
        <div className="panel">
          <h2>待确认 <span className="more">周期事项不会自动入账</span></h2>
          {pending.slice(0, 4).map((p) => (
            <div className="tx" key={p.id}>
              <div className="icon" style={{ background: 'var(--primary-soft)' }}>🗓️</div>
              <div className="main">
                <div className="title">{p.rule_name}</div>
                <div className="meta">
                  {p.planned_date}
                  {p.planned_amount_cents ? ` · 计划 ¥${formatCents(p.planned_amount_cents)}` : ''}
                  {p.status === 'partial' ? ` · 已记 ¥${formatCents(p.confirmed_amount_cents)}` : ''}
                </div>
              </div>
              <button className="btn-text" onClick={() => onEntry({
                type: 'expense', categoryID: p.category_id, fromID: p.account_id,
                amountYuan: p.planned_amount_cents ? yuan((BigInt(p.planned_amount_cents) - BigInt(p.confirmed_amount_cents)).toString()) : undefined,
                instanceID: p.id, note: p.rule_name,
              })}>去记</button>
              <button className="btn-text" onClick={async () => { await api.skipInstance(p.id); onChanged() }}>跳过</button>
            </div>
          ))}
        </div>
      )}

      {recs.length > 0 && (
        <div className="panel">
          <h2>现在可能要记</h2>
          <div className="chips">
            {recs.slice(0, 4).map((r, i) => (
              <div key={i} style={{ position: 'relative' }}>
                <button className="chip" title={r.reason}
                  onClick={() => onEntry({ type: 'expense', categoryID: r.category_id, amountYuan: yuan(r.amount_hint_cents), instanceID: r.instance_id, fromID: r.account_id })}>
                  <span className="ic" style={{ background: 'var(--primary-soft)' }}>{r.icon || '💡'}</span>
                  <span>{r.category_name || r.rule_name}</span>
                </button>
                {(r.kind === 'habit' || r.kind === 'recurrence') && (
                  <button aria-label="不再提醒" title="不再提醒"
                    style={{ position: 'absolute', top: -4, right: -2, border: 'none', background: 'var(--card)', borderRadius: '50%', width: 20, height: 20, fontSize: 11, color: 'var(--muted)', cursor: 'pointer', boxShadow: '0 1px 3px rgb(0 0 0 / 15%)' }}
                    onClick={async () => {
                      await api.dismissRecommendation(r.kind === 'habit' ? 'habit' : 'rule', (r.kind === 'habit' ? r.category_id : r.rule_id) ?? '')
                      onChanged()
                    }}>✕</button>
                )}
              </div>
            ))}
          </div>
          <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.72rem' }}>{recs[0].reason} · 点 ✕ 不再提醒</div>
        </div>
      )}

      {pinnedTpls.length > 0 && (
        <div className="panel">
          <h2>常用</h2>
          <div className="chips">
            {pinnedTpls.map((t) => (
              <button key={t.id} className="chip"
                onClick={() => onEntry({ type: t.tx_type === 'income' ? 'income' : 'expense', categoryID: t.category_id, amountYuan: yuan(t.fixed_amount_cents), note: t.name })}>
                <span className="ic" style={{ background: 'var(--primary-soft)' }}>{t.icon || '▫️'}</span>
                <span>{t.name}</span>
              </button>
            ))}
          </div>
        </div>
      )}

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
  const [q, setQ] = useState('')
  const [results, setResults] = useState<Tx[] | null>(null)

  useEffect(() => {
    if (!q.trim()) { setResults(null); return }
    const t = setTimeout(() => {
      api.search(q.trim()).then((r) => setResults(r.transactions)).catch(() => setResults([]))
    }, 250)
    return () => clearTimeout(t)
  }, [q])

  const list = results ?? txs
  const groups = new Map<string, Tx[]>()
  for (const t of list) {
    const d = t.business_date.slice(0, 10)
    if (!groups.has(d)) groups.set(d, [])
    groups.get(d)!.push(t)
  }
  return (
    <>
      <div className="greet"><h1>流水</h1><div className="sub">全部已确认记录 · 点按查看详情与退款/更正</div></div>
      <div className="field">
        <input placeholder="搜索备注、商户、对方、分类…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="搜索" />
      </div>
      {list.length === 0 && <div className="panel"><div className="empty">{results ? '没有匹配的账单。' : '还没有账单。'}</div></div>}
      {[...groups.entries()].map(([d, list]) => (
        <div key={d}>
          <div className="date-group">{d}</div>
          <div className="panel">{list.map((t) => <TxRow key={t.id} t={t} onOpen={onOpen} />)}</div>
        </div>
      ))}
    </>
  )
}

function MeView({ me, accounts, expenseCats, onLogout, onChanged }: {
  me: Me; accounts: Account[]; expenseCats: Category[]; onLogout: () => void; onChanged: () => void
}) {
  const [manage, setManage] = useState<'' | 'templates' | 'rules' | 'data' | 'members' | 'settings' | 'notes' | 'amort' | 'platform'>('')
  const [syncState, setSyncState] = useState<SyncState>({ status: 'disabled', pending: 0 })
  const [offlineOn, setOfflineOn] = useState(false)
  const [themePref, setThemePrefState] = useState<ThemePref>(getThemePref())
  const [fontPref, setFontPrefState] = useState<FontSizePref>(getFontSizePref())
  useEffect(() => sync.subscribe(setSyncState), [])
  useEffect(() => { setOfflineOn(syncState.status !== 'disabled') }, [syncState.status])
  // 主题变化时强制重渲染（分类底色等按当前主题取值的渲染随之刷新）。
  const [, setThemeTick] = useState(0)
  useEffect(() => subscribeTheme(() => setThemeTick((t) => t + 1)), [])
  // PWA 安装引导：事件到来/安装完成后刷新状态（beforeinstallprompt 由 main.tsx 捕获）。
  const [installSt, setInstallSt] = useState<InstallState>(installState())
  useEffect(() => subscribeInstall(() => setInstallSt(installState())), [])
  // 子账户创建表单
  const [subForm, setSubForm] = useState('')
  const [subKind, setSubKind] = useState<'current' | 'deposit' | 'investment'>('current')
  const [subName, setSubName] = useState('')
  const [subOpening, setSubOpening] = useState('')
  async function addSub(parentID: string) {
    if (!subName.trim()) return
    try {
      await api.createSubAccount(parentID, {
        name: subName.trim(), sub_kind: subKind,
        opening_balance: subOpening.trim() || undefined, balance_confirmed: true,
      })
      setSubForm(''); setSubName(''); setSubOpening('')
      onChanged()
    } catch (e) {
      alert(e instanceof ApiError ? e.message : '创建失败')
    }
  }

  const statusText: Record<string, string> = {
    disabled: '离线缓存未启用（共享设备建议保持关闭）',
    offline: '离线中：记账将先保存到本机',
    pending: `${syncState.pending} 笔待同步`,
    synced: `已同步${syncState.lastSync ? ` · ${syncState.lastSync.slice(0, 16).replace('T', ' ')}` : ''}`,
    generation_mismatch: '服务器经历恢复，需要全量重同步后才能继续',
    error: '同步出错，将自动重试',
  }

  return (
    <>
      <div className="greet"><h1>我的</h1><div className="sub">{me.display_name} · {me.role === 'admin' ? '管理员' : '成员'}</div></div>
      <div className="panel">
        <h2>资金账户 <span className="more">按已录入记录计算</span></h2>
        {accounts.filter((a) => !a.parent_id).map((a) => (
          <div key={a.id}>
            <div className="tx">
              <div className="icon" style={{ background: 'var(--primary-soft)' }}>💳</div>
              <div className="main">
                <div className="title">{a.name}</div>
                <div className="meta">
                  {ACCOUNT_TYPES[a.type] ?? a.type}{a.balance_unconfirmed ? ' · 余额未确认' : ''}
                  {a.type === 'stored_value' && a.expires_on ? ` · ${a.expires_on} 到期` : ''}
                </div>
              </div>
              <div className="amt">¥{formatCents(a.balance_cents)}</div>
              {me.role === 'admin' && !a.archived && a.type === 'stored_value' && (
                <button className="btn-text" style={{ fontSize: '0.8rem' }}
                  onClick={() => {
                    const d = prompt('到期日（YYYY-MM-DD，留空清除）', a.expires_on ?? '')
                    if (d === null) return
                    void api.setStoredValueMeta(a.id, a.face_value_cents ? formatCents(a.face_value_cents) : '', d.trim()).then(onChanged)
                  }}>效期</button>
              )}
              {me.role === 'admin' && !a.archived && !['credit_card', 'huabei', 'loan_liability'].includes(a.type) && (
                <button className="btn-text" style={{ fontSize: '0.8rem' }}
                  onClick={() => setSubForm(subForm === a.id ? '' : a.id)}>＋子账户</button>
              )}
            </div>
            {subForm === a.id && (
              <div className="tx" style={{ paddingLeft: 32 }}>
                <div className="main">
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 6 }}>
                    {([['current', '活期'], ['deposit', '定期'], ['investment', '理财']] as const).map(([k, v]) => (
                      <button key={k} className={`chip${subKind === k ? ' on' : ''}`} style={{ minHeight: 36, padding: '4px 12px' }}
                        onClick={() => setSubKind(k)}>{v}</button>
                    ))}
                  </div>
                  <input placeholder="名称（如：一年定期）" value={subName} onChange={(e) => setSubName(e.target.value)}
                    style={{ width: '100%', minHeight: 40, border: '1px solid var(--line)', borderRadius: 10, padding: '0 10px', fontSize: '0.95rem', background: 'var(--field-bg)', color: 'var(--ink)', marginBottom: 6 }} />
                  <input placeholder="期初余额（元，可空）" inputMode="decimal" value={subOpening} onChange={(e) => setSubOpening(e.target.value)}
                    style={{ width: '100%', minHeight: 40, border: '1px solid var(--line)', borderRadius: 10, padding: '0 10px', fontSize: '0.95rem', background: 'var(--field-bg)', color: 'var(--ink)', marginBottom: 6 }} />
                  <div style={{ display: 'flex', gap: 8 }}>
                    <button className="btn" style={{ minHeight: 40, fontSize: '0.95rem' }} onClick={() => void addSub(a.id)}>创建</button>
                    <button className="btn-text" onClick={() => setSubForm('')}>取消</button>
                  </div>
                </div>
              </div>
            )}
            {accounts.filter((s) => s.parent_id === a.id).map((s) => (
              <div className="tx" key={s.id} style={{ paddingLeft: 32 }}>
                <div className="icon" style={{ background: 'var(--primary-soft)', fontSize: '0.8rem' }}>
                  {s.sub_kind === 'current' ? '活' : s.sub_kind === 'deposit' ? '定' : '理'}
                </div>
                <div className="main">
                  <div className="title">{s.name}</div>
                  <div className="meta">{s.sub_kind === 'current' ? '活期' : s.sub_kind === 'deposit' ? '定期' : '理财'}{s.balance_unconfirmed ? ' · 余额未确认' : ''}</div>
                </div>
                <div className="amt">¥{formatCents(s.balance_cents)}</div>
              </div>
            ))}
          </div>
        ))}
        {accounts.length === 0 && <div className="empty">还没有账户。</div>}
      </div>
      <div className="panel">
        <h2>管理</h2>
        <div className="chips" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(64px, 1fr))' }}>
          <button className="chip" onClick={() => setManage(manage === 'members' ? '' : 'members')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>👪</span><span>成员</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'templates' ? '' : 'templates')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>📋</span><span>模板</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'rules' ? '' : 'rules')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>🗓️</span><span>周期</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'data' ? '' : 'data')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>📦</span><span>数据</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'notes' ? '' : 'notes')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>📝</span><span>便笺</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'amort' ? '' : 'amort')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>🧩</span><span>分摊</span>
          </button>
          <button className="chip" onClick={() => setManage(manage === 'settings' ? '' : 'settings')}>
            <span className="ic" style={{ background: 'var(--primary-soft)' }}>⚙️</span><span>设置</span>
          </button>
          {me.platform_role === 'superadmin' && (
            <button className="chip" onClick={() => setManage(manage === 'platform' ? '' : 'platform')}>
              <span className="ic" style={{ background: 'var(--primary-soft)' }}>🛡️</span><span>平台</span>
            </button>
          )}
        </div>
      </div>
      {manage === 'platform' && me.platform_role === 'superadmin' && <AdminPanel onClose={() => setManage('')} />}
      {manage === 'members' && <MembersPanel meID={me.user_id} isAdmin={me.role === 'admin'} onChanged={onChanged} />}
      {manage === 'templates' && <TemplatesPanel onChanged={onChanged} />}
      {manage === 'rules' && <RulesPanel expenseCats={expenseCats} onChanged={onChanged} />}
      {manage === 'data' && <DataPanel accounts={accounts} expenseCats={expenseCats} onChanged={onChanged} />}
      {manage === 'notes' && <NotesPanel />}
      {manage === 'amort' && <AmortPanel expenseCats={expenseCats} accounts={accounts} isAdmin={me.role === 'admin'} onChanged={onChanged} />}
      {manage === 'settings' && (
        <div className="panel">
          <h2>外观</h2>
          <div className="tx">
            <div className="main">
              <div className="title">主题</div>
              <div className="meta">跟随系统时随设备深浅色自动切换</div>
            </div>
          </div>
          <div className="seg">
            {([['system', '跟随系统'], ['light', '浅色'], ['dark', '深色']] as [ThemePref, string][]).map(([k, v]) => (
              <button key={k} className={themePref === k ? 'on' : ''}
                onClick={() => { setThemePref(k); setThemePrefState(k) }}>{v}</button>
            ))}
          </div>
          <div className="tx">
            <div className="main">
              <div className="title">字号</div>
              <div className="meta">大字模式适合长辈或远距离查看</div>
            </div>
          </div>
          <div className="seg">
            {([['standard', '标准'], ['large', '大字']] as [FontSizePref, string][]).map(([k, v]) => (
              <button key={k} className={fontPref === k ? 'on' : ''}
                onClick={() => { setFontSizePref(k); setFontPrefState(k) }}>{v}</button>
            ))}
          </div>
          <h2>安装应用</h2>
          <div className="tx">
            <div className="main">
              <div className="title">安装到主屏幕 / 桌面</div>
              <div className="meta">
                {installSt === 'installed' && '已作为应用安装，离线可打开'}
                {installSt === 'promptable' && '安装后离线可用、启动更快'}
                {installSt === 'ios' && 'Safari 打开分享菜单 → 添加到主屏幕'}
                {installSt === 'unavailable' && '当前浏览器暂不支持安装（可用系统浏览器重试）'}
              </div>
            </div>
            {installSt === 'promptable' && (
              <button className="btn-text" onClick={() => { void promptInstall() }}>安装</button>
            )}
          </div>
          <h2>离线与同步</h2>
          <div className="tx">
            <div className="main">
              <div className="title">本机离线缓存</div>
              <div className="meta">可信设备才开启；共享设备不要开启（本机缓存不是加密保险箱）</div>
            </div>
            <button className="btn-text" onClick={() => sync.setEnabled(!offlineOn)}>{offlineOn ? '关闭' : '开启'}</button>
          </div>
          <div className="notice">{statusText[syncState.status]}</div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <button className="btn-text" onClick={() => sync.flush()}>立即同步</button>
            {syncState.status === 'generation_mismatch' && (
              <button className="btn-text" onClick={() => sync.fullResync()}>全量重同步</button>
            )}
            {syncState.pending > 0 && (
              <button className="btn-text" onClick={async () => {
                const blob = new Blob([await exportOutbox()], { type: 'application/json' })
                const a = document.createElement('a')
                a.href = URL.createObjectURL(blob)
                a.download = 'xiaozhang-outbox.json'
                a.click()
              }}>导出未同步内容</button>
            )}
          </div>
        </div>
      )}
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

function EntryView({ accounts, expenseCats, incomeCats, prefill, onSaved, onCancel }: {
  accounts: Account[]; expenseCats: Category[]; incomeCats: Category[]
  prefill: EntryPrefill | null
  onSaved: (again: boolean) => void; onCancel: () => void
}) {
  const [type, setType] = useState<'expense' | 'income' | 'transfer'>(prefill?.type ?? 'expense')
  const [op, setOp] = useState<'' | 'lend' | 'borrow' | 'loan_repay' | 'redeem'>('')
  const [amount, setAmount] = useState(prefill?.amountYuan ?? '')
  const [catID, setCatID] = useState(prefill?.categoryID ?? '')
  const [fromID, setFromID] = useState(prefill?.fromID ?? '')
  const [toID, setToID] = useState(prefill?.toID ?? '')
  const [note, setNote] = useState(prefill?.note ?? '')
  const [more, setMore] = useState(false)
  const [advance, setAdvance] = useState('')
  const [counterparty, setCounterparty] = useState('')
  const [discount, setDiscount] = useState('')
  const [merchant, setMerchant] = useState('')
  const [catTouched, setCatTouched] = useState(false)
  const [suggest, setSuggest] = useState<string | null>(null)
  const [opCp, setOpCp] = useState('')
  const [loanID, setLoanID] = useState('')
  const [principal, setPrincipal] = useState('')
  const [err, setErr] = useState('')
  const [okLocal, setOkLocal] = useState('')
  const [busy, setBusy] = useState(false)

  const cats = type === 'income' ? incomeCats : expenseCats
  const opDate = () => new Date().toISOString()
  const yuanOf = (s: string) => { const [i, f = ''] = s.trim().split('.'); return `${i}.${f.padEnd(2, '0')}` }

  // 商户 → 分类记忆：输入商户后给出建议，但绝不覆盖用户手动选择
  useEffect(() => {
    const q = merchant.trim()
    if (q.length < 2 || type === 'transfer') { setSuggest(null); return }
    const t = setTimeout(() => {
      api.merchantSuggest(q).then((r) => {
        if (r.suggestion) {
          setSuggest(`历史习惯：${r.suggestion.merchant} → ${r.suggestion.category_name}`)
          if (!catTouched && r.suggestion.category_id) setCatID(r.suggestion.category_id)
        } else {
          setSuggest(null)
        }
      }).catch(() => setSuggest(null))
    }, 300)
    return () => clearTimeout(t)
  }, [merchant, catTouched, type])

  async function submit(again: boolean) {
    setErr('')
    // 资金操作类
    if (op) {
      const cents = parseYuan(amount)
      if (cents === null) { setErr('请输入正确金额'); return }
      setBusy(true)
      try {
        if (op === 'lend' || op === 'borrow') {
          if (!opCp.trim()) { setErr('请填写对方（如：朋友、房东）'); setBusy(false); return }
          const fn = op === 'lend' ? api.lend : api.borrow
          await fn({ business_date: opDate(), amount: yuanOf(amount), counterparty: opCp.trim(),
            from_account_id: fromID || undefined, to_account_id: toID || undefined, note: note || undefined, operation_id: crypto.randomUUID() })
        } else if (op === 'loan_repay') {
          const pc = parseYuan(principal)
          if (pc === null) { setErr('请填写本金部分'); setBusy(false); return }
          if (!loanID) { setErr('请选择贷款账户'); setBusy(false); return }
          if (!catID) { setErr('请选择利息分类'); setBusy(false); return }
          await api.loanRepay({ business_date: opDate(), from_account_id: fromID, loan_account_id: loanID,
            total: yuanOf(amount), principal: yuanOf(principal), interest_category_id: catID, note: note || undefined, operation_id: crypto.randomUUID() })
        } else {
          const pc = parseYuan(principal)
          if (pc === null) { setErr('请填写本金部分'); setBusy(false); return }
          if (!loanID) { setErr('请选择理财账户'); setBusy(false); return }
          await api.redeem({ business_date: opDate(), to_account_id: toID, invest_account_id: loanID,
            total: yuanOf(amount), principal: yuanOf(principal), yield_category_id: catID || undefined, note: note || undefined, operation_id: crypto.randomUUID() })
        }
        onSaved(again)
        if (again) { setAmount(''); setNote(''); setBusy(false) }
      } catch (e) {
        setErr(e instanceof ApiError ? e.message : '保存失败'); setBusy(false)
      }
      return
    }

    const cents = parseYuan(amount)
    if (cents === null) { setErr('请输入正确金额（最多两位小数，不能为 0）'); return }
    if (type !== 'transfer' && !catID) { setErr('请选择分类'); return }
    if ((type === 'expense' || type === 'transfer') && !fromID) { setErr('请选择付款账户'); return }
    if ((type === 'income' || type === 'transfer') && !toID) { setErr('请选择收款账户'); return }
    if (type === 'transfer' && fromID === toID) { setErr('转账账户不能相同'); return }

    // 拆分：代付 / 支付优惠（应付 = 实付 + 优惠）
    let splits: unknown
    const to2 = (v: bigint) => `${v / 100n}.${(v % 100n).toString().padStart(2, '0')}`
    const parts: { part_type: string; category_id?: string; counterparty?: string; amount: string }[] = []
    if (type === 'expense' && (advance.trim() !== '' || discount.trim() !== '')) {
      let self = BigInt(cents)
      if (discount.trim() !== '') {
        const d = parseYuan(discount)
        if (d === null) { setErr('优惠金额格式不正确'); return }
        self += BigInt(d) // 应付 = 实付 + 优惠
        parts.push({ part_type: 'discount', category_id: catID, amount: to2(BigInt(d)) })
      }
      if (advance.trim() !== '') {
        const adv = parseYuan(advance)
        if (adv === null) { setErr('垫付金额格式不正确'); return }
        if (!counterparty.trim()) { setErr('请填写垫付对象'); return }
        self -= BigInt(adv)
        parts.push({ part_type: 'receivable', counterparty: counterparty.trim(), amount: to2(BigInt(adv)) })
      }
      if (self <= 0n) { setErr('垫付金额必须小于应付金额'); return }
      parts.unshift({ part_type: 'expense', category_id: catID, amount: to2(self) })
      splits = parts
    }

    setBusy(true)
    const payload = {
      type, business_date: new Date().toISOString(), amount: yuanOf(amount),
      category_id: type === 'transfer' || splits ? undefined : catID,
      splits,
      from_account_id: fromID || undefined, to_account_id: toID || undefined,
      note: note || undefined, merchant: merchant || undefined,
      recurrence_instance_id: prefill?.instanceID,
      operation_id: crypto.randomUUID(),
    }
    // 离线：本机队列保存（待同步），明确告知不是服务器确认
    if (sync.enabled && !navigator.onLine) {
      try {
        await sync.queue({ operation_id: payload.operation_id as string, type: 'transaction', payload })
        setOkLocal('已保存到本机（待同步，联网后自动入账）')
        onSaved(again)
        if (again) { setAmount(''); setNote(''); setAdvance(''); setDiscount(''); setBusy(false) }
      } catch {
        setErr('本机存储失败（可能空间不足），请保留当前输入并联网后重试')
        setBusy(false)
      }
      return
    }
    try {
      await api.post(payload)
      onSaved(again)
      if (again) { setAmount(''); setNote(''); setAdvance(''); setDiscount(''); setBusy(false) }
    } catch (e) {
      // 网络故障且已启用离线：转入本机队列；其他错误照常显示
      if (sync.enabled && e instanceof TypeError) {
        await sync.queue({ operation_id: payload.operation_id as string, type: 'transaction', payload })
        setOkLocal('网络异常，已保存到本机（待同步）')
        onSaved(again)
      } else {
        setErr(e instanceof ApiError ? e.message : '保存失败'); setBusy(false)
      }
    }
  }

  const OP_LABEL: Record<string, string> = { lend: '借出/押金', borrow: '借入', loan_repay: '房贷/车贷还款', redeem: '理财赎回' }

  return (
    <>
      <div className="greet" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h1>记一笔</h1>
        <button className="btn-text" onClick={onCancel}>取消</button>
      </div>
      <div className="seg" role="tablist">
        {([['expense', '支出'], ['income', '收入'], ['transfer', '转账']] as const).map(([k, v]) => (
          <button key={k} className={type === k && !op ? 'on' : ''} onClick={() => { setType(k); setOp(''); setCatID('') }}>{v}</button>
        ))}
        <button className={op ? 'on' : ''} onClick={() => setOp(op || 'lend')}>更多类型</button>
      </div>
      {op && (
        <div className="seg">
          {(Object.entries(OP_LABEL) as [typeof op, string][]).map(([k, v]) => (
            <button key={k} className={op === k ? 'on' : ''} onClick={() => setOp(k)}>{v}</button>
          ))}
        </div>
      )}
      {err && <div className="alert" role="alert">{err}</div>}
      {okLocal && <div className="notice">{okLocal}</div>}
      <div className="panel">
        <div className="amount-row">
          <span>¥</span>
          <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} aria-label="金额" autoFocus />
        </div>

        {!op && type !== 'transfer' && (
          <div className="chips">
            {cats.map((c) => {
              const st = { icon: c.icon || catStyle(c.name).icon, bg: catStyle(c.name).bg }
              return (
                <button type="button" key={c.id} className={`chip ${catID === c.id ? 'on' : ''}`} onClick={() => { setCatID(c.id); setCatTouched(true) }}>
                  <span className="ic" style={{ background: st.bg }}>{st.icon}</span><span>{c.name}</span>
                </button>
              )
            })}
          </div>
        )}
        {suggest && !op && type !== 'transfer' && (
          <div className="meta" style={{ color: 'var(--muted)', fontSize: '0.75rem', margin: '-6px 0 10px' }}>{suggest}</div>
        )}

        {op === 'loan_repay' && (
          <>
            <div className="field"><label>本金部分（减负债，不算消费）</label>
              <input inputMode="decimal" placeholder="0.00" value={principal} onChange={(e) => setPrincipal(e.target.value)} /></div>
            <div className="field"><label>贷款账户（负债）</label>
              <select value={loanID} onChange={(e) => setLoanID(e.target.value)}>
                <option value="">请选择</option>
                {accounts.filter((a) => ['loan_liability', 'credit_card', 'huabei'].includes(a.type)).map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select></div>
            <div className="field"><label>利息分类（利息 = 总额 - 本金）</label>
              <select value={catID} onChange={(e) => setCatID(e.target.value)}>
                <option value="">请选择</option>
                {expenseCats.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select></div>
          </>
        )}
        {op === 'redeem' && (
          <>
            <div className="field"><label>本金部分（转回不算收入）</label>
              <input inputMode="decimal" placeholder="0.00" value={principal} onChange={(e) => setPrincipal(e.target.value)} /></div>
            <div className="field"><label>理财账户</label>
              <select value={loanID} onChange={(e) => setLoanID(e.target.value)}>
                <option value="">请选择</option>
                {accounts.filter((a) => a.type === 'other_asset').map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select></div>
            <div className="field"><label>已确认收益分类（收益 = 总额 - 本金）</label>
              <select value={catID} onChange={(e) => setCatID(e.target.value)}>
                <option value="">请选择</option>
                {incomeCats.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select></div>
          </>
        )}
        {(op === 'lend' || op === 'borrow') && (
          <div className="field"><label>{op === 'lend' ? '借给谁 / 押金对象' : '向谁借入'}</label>
            <input placeholder="例如：朋友、房东（押金）" value={opCp} onChange={(e) => setOpCp(e.target.value)} /></div>
        )}

        {(type === 'expense' || type === 'transfer' || op === 'lend' || op === 'loan_repay') && op !== 'redeem' && op !== 'borrow' && (
          <div className="field"><label>{op ? '付款账户' : type === 'expense' ? '付款账户' : '转出账户'}</label>
            <select value={fromID} onChange={(e) => setFromID(e.target.value)}>
              <option value="">请选择</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
            </select></div>
        )}
        {(type === 'income' || type === 'transfer' || op === 'borrow' || op === 'redeem') && (
          <div className="field"><label>{op === 'borrow' ? '收至账户' : op === 'redeem' ? '赎回到账账户' : type === 'income' ? '收款账户' : '转入账户'}</label>
            <select value={toID} onChange={(e) => setToID(e.target.value)}>
              <option value="">请选择</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
            </select></div>
        )}

        {!op && type === 'expense' && (
          <>
            <button type="button" className="btn-text" onClick={() => setMore(!more)}>
              {more ? '收起 ▴' : '更多（备注 · 代付 · 优惠）▾'}
            </button>
            {more && (
              <>
                <div className="field"><label>备注（可选）</label>
                  <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="一句话说明" /></div>
                <div className="field"><label>商户（可选，会记住商户 → 分类习惯）</label>
                  <input value={merchant} onChange={(e) => setMerchant(e.target.value)} placeholder="例如：麦当劳、滴滴" /></div>
                <div className="field"><label>支付优惠（如碰一碰立减，应付 = 实付 + 优惠）</label>
                  <input inputMode="decimal" placeholder="0.00" value={discount} onChange={(e) => setDiscount(e.target.value)} /></div>
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
        {(op || type !== 'expense') && (
          <div className="field"><label>备注（可选）</label>
            <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="一句话说明" /></div>
        )}
        <button className="btn" disabled={busy} onClick={() => submit(false)}>{busy ? '保存中…' : '保存'}</button>
        <button className="btn" style={{ background: 'var(--card)', color: 'var(--primary-dark)', marginTop: 8 }} disabled={busy}
          onClick={() => submit(true)}>保存并再记一笔</button>
      </div>
    </>
  )
}
