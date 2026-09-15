import { useCallback, useEffect, useMemo, useState } from 'react'
import { ACCOUNT_TYPES, api, ApiError, type Account, type Category, type Me, type Summary, type Tx } from './api'
import { formatCents, parseYuan, parseYuanAllowZero } from './money'

export default function App() {
  const [me, setMe] = useState<Me | null>(null)
  const [booting, setBooting] = useState(true)

  useEffect(() => {
    api.me()
      .then(setMe)
      .catch(() => setMe(null))
      .finally(() => setBooting(false))
  }, [])

  if (booting) return <div className="shell"><p className="empty">加载中…</p></div>
  if (!me) return <Login onLogin={setMe} />
  return <Home me={me} onLogout={() => setMe(null)} />
}

function Login({ onLogin }: { onLogin: (m: Me) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [needsInit, setNeedsInit] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.setupStatus().then((s) => setNeedsInit(s.needs_init)).catch(() => {})
  }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.login(username, password)
      onLogin(await api.me())
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '网络错误')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <h1>小账</h1>
      <p className="slogan">日常小账，心里有数。</p>
      {needsInit && (
        <div className="notice">尚未创建管理员。请在服务器本机执行：xiaozhang init-admin -username &lt;用户名&gt;</div>
      )}
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field">
          <label htmlFor="u">用户名</label>
          <input id="u" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required />
        </div>
        <div className="field">
          <label htmlFor="p">密码</label>
          <input id="p" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </div>
        <button className="btn" disabled={busy}>{busy ? '登录中…' : '登录'}</button>
      </form>
    </div>
  )
}

function monthRange(): { from: string; to: string; label: string } {
  const now = new Date()
  const from = new Date(now.getFullYear(), now.getMonth(), 1)
  const to = new Date(now.getFullYear(), now.getMonth() + 1, 1)
  const fmt = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return { from: fmt(from), to: fmt(to), label: `${now.getMonth() + 1}月` }
}

function Home({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [expenseCats, setExpenseCats] = useState<Category[]>([])
  const [incomeCats, setIncomeCats] = useState<Category[]>([])
  const [txs, setTxs] = useState<Tx[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [err, setErr] = useState('')
  const mr = useMemo(monthRange, [])

  const reload = useCallback(async () => {
    try {
      const [a, ec, ic, t, s] = await Promise.all([
        api.accounts(),
        api.categories('expense'),
        api.categories('income'),
        api.transactions(30),
        api.summary(mr.from, mr.to),
      ])
      setAccounts(a.accounts.filter((x) => !x.archived))
      setExpenseCats(ec.categories.filter((x) => !x.archived))
      setIncomeCats(ic.categories.filter((x) => !x.archived))
      setTxs(t.transactions)
      setSummary(s)
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [mr.from, mr.to])

  useEffect(() => { reload() }, [reload])

  return (
    <div className="shell">
      <div className="topbar">
        <div>
          <h1>{me.ledger_name}</h1>
          <div className="sub">{mr.label} · {me.display_name}</div>
        </div>
        <button className="btn-text" onClick={async () => { await api.logout(); onLogout() }}>退出</button>
      </div>

      {err && <div className="alert" role="alert">{err}</div>}

      <div className="cards">
        <div className="card"><div className="label">净收入</div><div className="value income">{summary ? formatCents(summary.income_cents) : '—'}</div></div>
        <div className="card"><div className="label">净支出</div><div className="value expense">{summary ? formatCents(summary.expense_cents) : '—'}</div></div>
        <div className="card"><div className="label">收支结余</div><div className="value">{summary ? formatCents(summary.net_cents) : '—'}</div></div>
      </div>

      {accounts.length === 0 ? (
        <FirstAccount onCreated={reload} />
      ) : (
        <QuickEntry accounts={accounts} expenseCats={expenseCats} incomeCats={incomeCats} onSaved={reload} />
      )}

      <div className="panel">
        <h2>资金账户</h2>
        {accounts.map((a) => (
          <div className="tx" key={a.id}>
            <div className="main">
              <div className="title">{a.name}</div>
              <div className="meta">{ACCOUNT_TYPES[a.type] ?? a.type}{a.balance_unconfirmed ? ' · 余额未确认' : ''}</div>
            </div>
            <div className="amt">{formatCents(a.balance_cents)}</div>
          </div>
        ))}
      </div>

      <div className="panel">
        <h2>最近流水</h2>
        {txs.length === 0 && <div className="empty">还没有账单，记一笔吧。</div>}
        {txs.map((t) => (
          <div className="tx" key={t.id}>
            <div className="main">
              <div className="title">{t.type === 'transfer' ? '转账' : t.category_name}</div>
              <div className="meta">
                {t.business_date.slice(0, 10)}
                {' · '}
                {t.type === 'expense' && `${t.from_account_name} 支出`}
                {t.type === 'income' && `${t.to_account_name} 收入`}
                {t.type === 'transfer' && `${t.from_account_name} → ${t.to_account_name}`}
                {t.note ? ` · ${t.note}` : ''}
              </div>
            </div>
            <div className={`amt ${t.type === 'expense' ? 'expense' : t.type === 'income' ? 'income' : ''}`}>
              {t.type === 'expense' ? '-' : t.type === 'income' ? '+' : ''}{formatCents(t.amount_cents)}
            </div>
          </div>
        ))}
      </div>
    </div>
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
    setBusy(true)
    setErr('')
    try {
      await api.createAccount({
        name, type,
        opening_balance: (BigInt(cents) / 100n).toString() + '.' + (BigInt(cents) % 100n).toString().padStart(2, '0'),
        balance_confirmed: opening.trim() !== '',
      })
      onCreated()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="panel">
      <h2>建立第一个资金账户</h2>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field">
          <label htmlFor="an">账户名称</label>
          <input id="an" value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：招商银行卡" required />
        </div>
        <div className="field">
          <label htmlFor="at">类型</label>
          <select id="at" value={type} onChange={(e) => setType(e.target.value)}>
            {Object.entries(ACCOUNT_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ao">期初余额（可暂不填写）</label>
          <input id="ao" inputMode="decimal" value={opening} onChange={(e) => setOpening(e.target.value)} placeholder="0.00" />
        </div>
        <button className="btn" disabled={busy}>{busy ? '创建中…' : '创建账户'}</button>
      </form>
    </div>
  )
}

function QuickEntry({ accounts, expenseCats, incomeCats, onSaved }: {
  accounts: Account[]
  expenseCats: Category[]
  incomeCats: Category[]
  onSaved: () => void
}) {
  const [type, setType] = useState<'expense' | 'income' | 'transfer'>('expense')
  const [amount, setAmount] = useState('')
  const [catID, setCatID] = useState('')
  const [fromID, setFromID] = useState('')
  const [toID, setToID] = useState('')
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')
  const [ok, setOk] = useState('')
  const [busy, setBusy] = useState(false)

  const cats = type === 'income' ? incomeCats : expenseCats
  const usableFrom = accounts
  const usableTo = accounts

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setOk('')
    const cents = parseYuan(amount)
    if (cents === null) { setErr('请输入正确金额（最多两位小数，不能为 0）'); return }
    if (type !== 'transfer' && !catID) { setErr('请选择分类'); return }
    if ((type === 'expense' || type === 'transfer') && !fromID) { setErr('请选择付款账户'); return }
    if ((type === 'income' || type === 'transfer') && !toID) { setErr('请选择收款账户'); return }
    if (type === 'transfer' && fromID === toID) { setErr('转账账户不能相同'); return }
    setBusy(true)
    try {
      const [i, f = ''] = amount.trim().split('.')
      const yuan = `${i}.${f.padEnd(2, '0')}`
      await api.post({
        type,
        business_date: new Date().toISOString(),
        amount: yuan,
        category_id: type === 'transfer' ? undefined : catID,
        from_account_id: fromID || undefined,
        to_account_id: toID || undefined,
        note: note || undefined,
        operation_id: crypto.randomUUID(),
      })
      setAmount('')
      setNote('')
      setOk('已保存')
      onSaved()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="panel">
      <h2>记一笔</h2>
      <div className="seg" role="tablist">
        <button type="button" className={type === 'expense' ? 'on' : ''} onClick={() => { setType('expense'); setCatID('') }}>支出</button>
        <button type="button" className={type === 'income' ? 'on' : ''} onClick={() => { setType('income'); setCatID('') }}>收入</button>
        <button type="button" className={type === 'transfer' ? 'on' : ''} onClick={() => setType('transfer')}>转账</button>
      </div>
      {err && <div className="alert" role="alert">{err}</div>}
      {ok && <div className="notice">{ok}</div>}
      <form onSubmit={submit}>
        <div className="amount-row">
          <span>¥</span>
          <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} aria-label="金额" />
        </div>
        {type !== 'transfer' && (
          <div className="chips">
            {cats.map((c) => (
              <button type="button" key={c.id} className={`chip ${catID === c.id ? 'on' : ''}`} onClick={() => setCatID(c.id)}>{c.name}</button>
            ))}
          </div>
        )}
        {(type === 'expense' || type === 'transfer') && (
          <div className="field">
            <label>{type === 'expense' ? '付款账户' : '转出账户'}</label>
            <select value={fromID} onChange={(e) => setFromID(e.target.value)}>
              <option value="">请选择</option>
              {usableFrom.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
            </select>
          </div>
        )}
        {(type === 'income' || type === 'transfer') && (
          <div className="field">
            <label>{type === 'income' ? '收款账户' : '转入账户'}</label>
            <select value={toID} onChange={(e) => setToID(e.target.value)}>
              <option value="">请选择</option>
              {usableTo.map((a) => <option key={a.id} value={a.id}>{a.name}（{formatCents(a.balance_cents)}）</option>)}
            </select>
          </div>
        )}
        <div className="field">
          <label>备注（可选）</label>
          <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="一句话说明" />
        </div>
        <button className="btn" disabled={busy}>{busy ? '保存中…' : '保存'}</button>
      </form>
    </div>
  )
}
