import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type Account, type TxDetail } from './api'
import { AttachmentsPanel } from './attachments'
import { formatCents, parseYuan } from './money'

// 账单详情（二级页面）：退款 / 收入退回 / 回款 / 核销 / 转待报销 / 更正 / 作废。
// 详情内行内展开处理，不叠加第三级页面。

const TYPE_LABEL: Record<string, string> = {
  expense: '支出', income: '收入', transfer: '转账', refund: '退款',
  income_refund: '收入退回', settlement: '回款', reclass: '重分类', writeoff: '核销',
}

export function TxDetailView({ id, accounts, onBack, onChanged }: {
  id: string
  accounts: Account[]
  onBack: () => void
  onChanged: () => void
}) {
  const [d, setD] = useState<TxDetail | null>(null)
  const [err, setErr] = useState('')
  const [action, setAction] = useState<string | null>(null)
  const [copyErr, setCopyErr] = useState('')

  const load = useCallback(async () => {
    try {
      setD(await api.txDetail(id))
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [id])

  useEffect(() => { load() }, [load])

  const done = () => { setAction(null); load(); onChanged() }
  const run2 = async (fn: () => Promise<unknown>) => {
    setCopyErr('')
    try { await fn(); onBack(); onChanged() } catch (e) {
      setCopyErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }

  if (err) return <div className="shell"><button className="btn-text" onClick={onBack}>‹ 返回</button><div className="alert">{err}</div></div>
  if (!d) return <div className="shell"><p className="empty">加载中…</p></div>

  const isPlainExpense = d.type === 'expense' && !d.reversed && (!d.splits || d.splits.length === 0)
  const hasSplits = (d.splits?.length ?? 0) > 0
  const refundable = d.type === 'expense' && !d.reversed
  const incomeRefundable = d.type === 'income' && !d.reversed
  const outstanding = d.receivable && BigInt(d.receivable.outstanding_cents) > 0n ? d.receivable : null

  return (
    <>
      <div className="greet" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h1>账单详情</h1>
        <button className="btn-text" onClick={onBack}>‹ 返回</button>
      </div>

      <div className="panel">
        <div className="tx" style={{ borderBottom: 'none' }}>
          <div className="main">
            <div className="title">{d.type === 'transfer' ? '转账' : d.category_name || TYPE_LABEL[d.type] || d.type}</div>
            <div className="meta">
              {TYPE_LABEL[d.type] ?? d.type} · {d.business_date.slice(0, 10)}
              {d.merchant ? ` · ${d.merchant}` : ''}{d.note ? ` · ${d.note}` : ''}
            </div>
          </div>
          <div className={`amt ${d.type}`}>
            {d.type === 'expense' ? '-' : d.type === 'income' ? '+' : ''}¥{formatCents(d.amount_cents)}
          </div>
        </div>
        {d.reversed && <div className="alert">此账单已被更正/作废（保留原始记录与冲正链）。</div>}
        {d.revision && (
          <div className="notice">修订：{d.revision.reason}（{d.revision.created_at.slice(0, 10)}）</div>
        )}
      </div>

      {hasSplits && (
        <div className="panel">
          <h2>拆分</h2>
          {d.splits!.map((sp) => (
            <div className="tx" key={sp.id}>
              <div className="main">
                <div className="title">{sp.part_type === 'receivable' ? `代付 · ${sp.counterparty}` : sp.category_name}</div>
                {BigInt(sp.refunded_cents) > 0n && <div className="meta">已退 ¥{formatCents(sp.refunded_cents)}</div>}
              </div>
              <div className="amt">¥{formatCents(sp.amount_cents)}</div>
            </div>
          ))}
        </div>
      )}

      {(d.refunds?.length ?? 0) > 0 && (
        <div className="panel">
          <h2>退款记录 <span className="more">累计 ¥{formatCents(d.refunded_total_cents)}</span></h2>
          {d.refunds!.map((r) => (
            <div className="tx" key={r.tx_id}>
              <div className="main">
                <div className="title">¥{formatCents(r.amount_cents)}{r.effective ? '' : '（已冲正）'}</div>
                <div className="meta">{r.business_date.slice(0, 10)}{r.account_name ? ` · 退至 ${r.account_name}` : ''}</div>
              </div>
            </div>
          ))}
        </div>
      )}

      {d.receivable && (
        <div className="panel">
          <h2>应收往来 {d.receivable.counterparty ? `· ${d.receivable.counterparty}` : ''}</h2>
          <div className="tx"><div className="main"><div className="title">形成应收</div></div><div className="amt">¥{formatCents(d.receivable.created_cents)}</div></div>
          <div className="tx"><div className="main"><div className="title">已回款</div></div><div className="amt">¥{formatCents(d.receivable.settled_cents)}</div></div>
          {BigInt(d.receivable.refunded_cents) > 0n && <div className="tx"><div className="main"><div className="title">商户退款冲减</div></div><div className="amt">¥{formatCents(d.receivable.refunded_cents)}</div></div>}
          {BigInt(d.receivable.written_off_cents) > 0n && <div className="tx"><div className="main"><div className="title">已核销</div></div><div className="amt">¥{formatCents(d.receivable.written_off_cents)}</div></div>}
          <div className="tx"><div className="main"><div className="title">待收余额</div></div>
            <div className="amt" style={{ color: outstanding ? 'var(--expense)' : 'var(--income)' }}>¥{formatCents(d.receivable.outstanding_cents)}</div></div>
        </div>
      )}

      <AttachmentsPanel txID={d.id} />

      {!d.reversed && action === null && (
        <div className="panel">
          <h2>操作</h2>
          <div className="chips" style={{ gridTemplateColumns: 'repeat(3, 1fr)' }}>
            {refundable && <ActionBtn label="退款" onClick={() => setAction('refund')} />}
            {incomeRefundable && <ActionBtn label="收入退回" onClick={() => setAction('income-refund')} />}
            {outstanding && <ActionBtn label="登记回款" onClick={() => setAction('settle')} />}
            {outstanding && <ActionBtn label="核销" onClick={() => setAction('writeoff')} />}
            {isPlainExpense && <ActionBtn label="转为待报销" onClick={() => setAction('reclass')} />}
            {(d.type === 'expense' || d.type === 'income' || d.type === 'transfer') && (
              <ActionBtn label="复制" onClick={() => run2(async () => { await api.copyTx(d.id) })} />
            )}
            {!d.reversed && <ActionBtn label="更正" onClick={() => setAction('correct')} />}
            {!d.reversed && <ActionBtn label="作废" onClick={() => setAction('void')} danger />}
          </div>
          {copyErr && <div className="alert">{copyErr}</div>}
        </div>
      )}

      {action === 'refund' && <RefundForm d={d} accounts={accounts} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'income-refund' && <IncomeRefundForm d={d} accounts={accounts} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'settle' && outstanding && <SettleForm d={d} accounts={accounts} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'writeoff' && outstanding && <WriteoffForm d={d} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'reclass' && <ReclassForm d={d} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'correct' && <CorrectForm d={d} onCancel={() => setAction(null)} onDone={done} />}
      {action === 'void' && <VoidForm d={d} onCancel={() => setAction(null)} onDone={done} />}
    </>
  )
}

function ActionBtn({ label, onClick, danger }: { label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button className="chip" onClick={onClick}>
      <span className="ic" style={{ background: danger ? '#fdecea' : 'var(--primary-soft)' }}>{danger ? '⚠️' : '›'}</span>
      <span>{label}</span>
    </button>
  )
}

function useActionForm(onDone: () => void) {
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true); setErr('')
    try { await fn(); onDone() } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败'); setBusy(false)
    }
  }
  return { err, busy, run }
}

const today = () => new Date().toISOString().slice(0, 10)

function RefundForm({ d, accounts, onCancel, onDone }: { d: TxDetail; accounts: Account[]; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const hasSplits = (d.splits?.length ?? 0) > 0
  const [amounts, setAmounts] = useState<Record<string, string>>({})
  const [whole, setWhole] = useState('')
  const [accountID, setAccountID] = useState(d.from_account_id || '')

  const submit = () => run(async () => {
    let allocations: { split_id?: string; amount: string }[]
    if (hasSplits) {
      allocations = d.splits!
        .filter((sp) => (amounts[sp.id] ?? '').trim() !== '')
        .map((sp) => {
          const c = parseYuan(amounts[sp.id])
          if (c === null) throw new ApiError(400, 'invalid_amount', '退款金额格式不正确')
          return { split_id: sp.id, amount: (Number(c) / 100).toFixed(2) }
        })
      if (allocations.length === 0) throw new ApiError(400, 'invalid_input', '请填写至少一项退款金额')
    } else {
      const c = parseYuan(whole)
      if (c === null) throw new ApiError(400, 'invalid_amount', '退款金额格式不正确')
      allocations = [{ amount: (Number(c) / 100).toFixed(2) }]
    }
    if (!accountID) throw new ApiError(400, 'invalid_input', '请选择退至账户')
    await api.refund(d.id, { business_date: today(), account_id: accountID, allocations, operation_id: crypto.randomUUID() })
  })

  const maxHint = hasSplits ? null : `剩余可退 ¥${formatCents((BigInt(d.amount_cents) - BigInt(d.refunded_total_cents)).toString())}`

  return (
    <div className="panel">
      <h2>退款 {maxHint && <span className="more">{maxHint}</span>}</h2>
      {err && <div className="alert">{err}</div>}
      {hasSplits ? d.splits!.map((sp) => (
        <div className="field" key={sp.id}>
          <label>{sp.part_type === 'receivable' ? `代付 · ${sp.counterparty}` : sp.category_name}
            （可退 ¥{formatCents((BigInt(sp.amount_cents) - BigInt(sp.refunded_cents)).toString())}）</label>
          <input inputMode="decimal" placeholder="0.00" value={amounts[sp.id] ?? ''}
            onChange={(e) => setAmounts({ ...amounts, [sp.id]: e.target.value })} />
        </div>
      )) : (
        <div className="field"><label>退款金额</label>
          <input inputMode="decimal" placeholder="0.00" value={whole} onChange={(e) => setWhole(e.target.value)} /></div>
      )}
      <div className="field"><label>退至账户</label>
        <select value={accountID} onChange={(e) => setAccountID(e.target.value)}>
          <option value="">请选择</option>
          {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select></div>
      <button className="btn" disabled={busy} onClick={submit}>{busy ? '提交中…' : '确认退款'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function IncomeRefundForm({ d, accounts, onCancel, onDone }: { d: TxDetail; accounts: Account[]; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [amount, setAmount] = useState('')
  const [accountID, setAccountID] = useState(d.to_account_id || '')
  return (
    <div className="panel">
      <h2>收入退回</h2>
      {err && <div className="alert">{err}</div>}
      <div className="field"><label>退回金额</label>
        <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
      <div className="field"><label>从账户退回</label>
        <select value={accountID} onChange={(e) => setAccountID(e.target.value)}>
          <option value="">请选择</option>
          {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select></div>
      <button className="btn" disabled={busy}
        onClick={() => run(async () => {
          const c = parseYuan(amount)
          if (c === null) throw new ApiError(400, 'invalid_amount', '金额格式不正确')
          await api.incomeRefund(d.id, { business_date: today(), account_id: accountID, amount: (Number(c) / 100).toFixed(2), operation_id: crypto.randomUUID() })
        })}>{busy ? '提交中…' : '确认退回'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function SettleForm({ d, accounts, onCancel, onDone }: { d: TxDetail; accounts: Account[]; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [amount, setAmount] = useState('')
  const [accountID, setAccountID] = useState('')
  return (
    <div className="panel">
      <h2>登记回款 <span className="more">待收 ¥{formatCents(d.receivable!.outstanding_cents)}</span></h2>
      {err && <div className="alert">{err}</div>}
      <div className="field"><label>回款金额（回款不是收入）</label>
        <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
      <div className="field"><label>收至账户</label>
        <select value={accountID} onChange={(e) => setAccountID(e.target.value)}>
          <option value="">请选择</option>
          {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select></div>
      <button className="btn" disabled={busy}
        onClick={() => run(async () => {
          const c = parseYuan(amount)
          if (c === null) throw new ApiError(400, 'invalid_amount', '金额格式不正确')
          await api.settle(d.id, { business_date: today(), account_id: accountID, amount: (Number(c) / 100).toFixed(2), operation_id: crypto.randomUUID() })
        })}>{busy ? '提交中…' : '确认回款'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function WriteoffForm({ d, onCancel, onDone }: { d: TxDetail; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  return (
    <div className="panel">
      <h2>核销应收 <span className="more">转为自己承担的费用，需说明原因</span></h2>
      {err && <div className="alert">{err}</div>}
      <div className="field"><label>核销金额</label>
        <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
      <div className="field"><label>原因（必填）</label>
        <input placeholder="例如：多次催要无果" value={reason} onChange={(e) => setReason(e.target.value)} /></div>
      <button className="btn" disabled={busy}
        onClick={() => run(async () => {
          const c = parseYuan(amount)
          if (c === null) throw new ApiError(400, 'invalid_amount', '金额格式不正确')
          if (!reason.trim()) throw new ApiError(400, 'invalid_input', '请填写核销原因')
          await api.writeoff(d.id, { business_date: today(), amount: (Number(c) / 100).toFixed(2), reason, operation_id: crypto.randomUUID() })
        })}>{busy ? '提交中…' : '确认核销'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function ReclassForm({ d, onCancel, onDone }: { d: TxDetail; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [amount, setAmount] = useState('')
  const [cp, setCp] = useState('')
  return (
    <div className="panel">
      <h2>转为待报销 <span className="more">费用转应收，不产生现金流</span></h2>
      {err && <div className="alert">{err}</div>}
      <div className="field"><label>金额</label>
        <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
      <div className="field"><label>报销/代付对方（如：公司、朋友）</label>
        <input placeholder="公司" value={cp} onChange={(e) => setCp(e.target.value)} /></div>
      <button className="btn" disabled={busy}
        onClick={() => run(async () => {
          const c = parseYuan(amount)
          if (c === null) throw new ApiError(400, 'invalid_amount', '金额格式不正确')
          if (!cp.trim()) throw new ApiError(400, 'invalid_input', '请填写对方')
          await api.reclass(d.id, { business_date: today(), amount: (Number(c) / 100).toFixed(2), counterparty: cp, operation_id: crypto.randomUUID() })
        })}>{busy ? '提交中…' : '确认转换'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function CorrectForm({ d, onCancel, onDone }: { d: TxDetail; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [amount, setAmount] = useState((Number(d.amount_cents) / 100).toFixed(2))
  const [reason, setReason] = useState('')
  const accountID = d.from_account_id || d.to_account_id || ''
  return (
    <div className="panel">
      <h2>错账更正 <span className="more">保留原记录与冲正链</span></h2>
      {err && <div className="alert">{err}</div>}
      <div className="notice">将原单技术冲正（不产生虚假退款现金流），并以正确金额重新入账，业务日期保持 {d.business_date.slice(0, 10)}。</div>
      <div className="field"><label>正确金额</label>
        <input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} /></div>
      <div className="field"><label>更正原因（必填）</label>
        <input placeholder="例如：金额误记" value={reason} onChange={(e) => setReason(e.target.value)} /></div>
      <button className="btn" disabled={busy}
        onClick={() => run(async () => {
          const c = parseYuan(amount)
          if (c === null) throw new ApiError(400, 'invalid_amount', '金额格式不正确')
          if (!reason.trim()) throw new ApiError(400, 'invalid_input', '请填写更正原因')
          await api.revise(d.id, {
            reason,
            replacement: {
              type: d.type, business_date: d.business_date, amount: (Number(c) / 100).toFixed(2),
              category_id: d.category_id, from_account_id: d.type === 'income' ? undefined : accountID || undefined,
              to_account_id: d.type === 'income' ? accountID || undefined : d.type === 'transfer' ? d.to_account_id : undefined,
            },
          })
        })}>{busy ? '提交中…' : '确认更正'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}

function VoidForm({ d, onCancel, onDone }: { d: TxDetail; onCancel: () => void; onDone: () => void }) {
  const { err, busy, run } = useActionForm(onDone)
  const [reason, setReason] = useState('')
  return (
    <div className="panel">
      <h2>作废账单</h2>
      {err && <div className="alert">{err}</div>}
      <div className="notice">作废通过留痕冲正完成，不删除记录；有退款或结算的账单不能作废。</div>
      <div className="field"><label>作废原因（必填）</label>
        <input placeholder="例如：重复录入" value={reason} onChange={(e) => setReason(e.target.value)} /></div>
      <button className="btn" style={{ background: 'var(--expense)' }} disabled={busy}
        onClick={() => run(async () => {
          if (!reason.trim()) throw new ApiError(400, 'invalid_input', '请填写作废原因')
          await api.revise(d.id, { reason })
        })}>{busy ? '提交中…' : '确认作废'}</button>
      <button className="btn-text" onClick={onCancel}>取消</button>
    </div>
  )
}
