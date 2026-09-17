import { useState } from 'react'
import { ACCOUNT_TYPES, api, ApiError } from './api'
import { parseYuanAllowZero } from './money'

// AccountForm 用于已有账户后的「新增账户」。账户不设数量限制：可创建多张
// 银行卡/信用卡，也可分别建立股票、基金账户；余额均由复式记账持续计算。
export function AccountForm({ onCreated }: { onCreated: () => void }) {
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
        name: name.trim(), type,
        opening_balance: `${c / 100n}.${(c % 100n).toString().padStart(2, '0')}`,
        balance_confirmed: opening.trim() !== '',
      })
      onCreated()
    } catch (e2) {
      setErr(e2 instanceof ApiError ? e2.message : '创建失败')
    } finally { setBusy(false) }
  }

  return (
    <form onSubmit={submit} className="account-form">
      {err && <div className="alert" role="alert">{err}</div>}
      <div className="field"><label htmlFor="new-account-name">账户名称</label>
        <input id="new-account-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={ACCOUNT_TYPES[type] === '股票' ? '例如：东方财富股票账户' : ACCOUNT_TYPES[type] === '基金' ? '例如：支付宝基金' : '例如：招商银行工资卡'} required /></div>
      <div className="field"><label htmlFor="new-account-type">账户类型</label>
        <select id="new-account-type" value={type} onChange={(e) => setType(e.target.value)}>
          {Object.entries(ACCOUNT_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select></div>
      <div className="field"><label htmlFor="new-account-opening">当前余额 / 期初余额（可暂不填写）</label>
        <input id="new-account-opening" inputMode="decimal" value={opening} onChange={(e) => setOpening(e.target.value)} placeholder="0.00" /></div>
      <button className="btn" disabled={busy}>{busy ? '创建中…' : `添加${ACCOUNT_TYPES[type] ?? '账户'}`}</button>
    </form>
  )
}
