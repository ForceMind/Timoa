import { useCallback, useEffect, useState, type ReactElement } from 'react'
import { ACCOUNT_TYPES, api, ApiError, type Me, type OpsAuditItem, type OpsBackup, type OpsStatus, type OpsUserDetail } from './api'
import { formatCents } from './money'

// opspanel.tsx: 运营面板（随机路径 + 登录 + 平台超管）。
// 标签页：概览 / 用户（可点详情：账户+流水+冻结+重置密码）/ 备份 / 审计日志。

type LoginProps = { onLogin: (m: Me) => void }
type Tab = 'overview' | 'users' | 'backups' | 'audit'

export function OpsPanel({ opsPath, LoginView }: { opsPath: string; LoginView: (p: LoginProps) => ReactElement }) {
  const [status, setStatus] = useState<OpsStatus | null>(null)
  const [phase, setPhase] = useState<'loading' | 'login' | 'ready'>('loading')
  const [err, setErr] = useState('')
  const [newAdmin, setNewAdmin] = useState('')
  const [busy, setBusy] = useState(false)
  const [tab, setTab] = useState<Tab>('overview')
  const [selectedUser, setSelectedUser] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const s = await api.opsStatus(opsPath)
      setStatus(s)
      setPhase('ready')
      setErr('')
    } catch (e) {
      const ae = e as ApiError
      if (ae.status === 401) {
        setPhase('login')
      } else if (ae.status === 403) {
        setErr('当前账号不是平台超管，无法访问运营面板。请换用超管账号登录。')
        setPhase('login')
      } else {
        setErr(ae.message || '加载失败')
        setPhase('login')
      }
    }
  }, [opsPath])

  useEffect(() => { void load() }, [load])

  if (phase === 'loading') {
    return <div className="shell"><p className="empty">加载中…</p></div>
  }

  if (phase === 'login') {
    return (
      <div className="shell">
        {err && <div className="alert" role="alert">{err}</div>}
        <LoginView onLogin={() => { setPhase('loading'); void load() }} />
      </div>
    )
  }

  if (!status) return null

  async function regenerate() {
    if (!confirm('重新生成后台路径？当前地址将立即失效，需用新地址访问。')) return
    setBusy(true)
    try {
      const r = await api.opsRegeneratePath(opsPath)
      window.alert('新后台地址：/' + r.ops_path + '\n请保存，即将跳转。')
      location.href = '/' + r.ops_path
    } catch (e) {
      setErr((e as ApiError).message)
      setBusy(false)
    }
  }

  async function makeSuper() {
    const u = newAdmin.trim()
    if (!u) return
    setBusy(true)
    try {
      await api.opsMakeSuperadmin(opsPath, u)
      window.alert(`已将 ${u} 设为平台超管`)
      setNewAdmin('')
      await load()
    } catch (e) {
      setErr((e as ApiError).message)
    } finally {
      setBusy(false)
    }
  }

  async function toggleReg() {
    if (!status) return
    try {
      await api.opsSetRegistration(opsPath, !status.registration_open)
      await load()
    } catch (e) {
      setErr((e as ApiError).message)
    }
  }

  return (
    <div className="shell">
      <div className="panel">
        <h2>运营面板 <span className="admin-user-uid">/{opsPath}</span></h2>
        <p className="admin-note">平台超管后台：可查看用户数据、冻结用户、重置密码、备份与审计。入口为固定随机路径，请妥善保管。</p>
        {err && <div className="alert" role="alert">{err}</div>}
        <div className="ops-tabs" role="tablist">
          {([['overview', '概览'], ['users', '用户'], ['backups', '备份'], ['audit', '审计']] as [Tab, string][]).map(([k, label]) => (
            <button key={k} role="tab" aria-selected={tab === k} className={tab === k ? 'ops-tab on' : 'ops-tab'} onClick={() => { setTab(k); setSelectedUser(null) }}>{label}</button>
          ))}
        </div>
      </div>

      {tab === 'overview' && (
        <>
          <div className="panel">
            <div className="stat-grid">
              <div className="stat-cell"><div className="stat-value">{status.version || 'dev'}</div><div className="stat-label">版本</div></div>
              <div className="stat-cell"><div className="stat-value">{status.stats.user_count}</div><div className="stat-label">注册用户</div></div>
              <div className="stat-cell"><div className="stat-value">{status.stats.ledger_count}</div><div className="stat-label">账本总数</div></div>
              <div className="stat-cell"><div className="stat-value">{status.stats.tx_count}</div><div className="stat-label">流水笔数</div></div>
            </div>
          </div>
          <div className="panel">
            <h2>服务状态</h2>
            <div className="admin-reg"><span>监听地址</span><span>{status.addr}</span></div>
            <div className="admin-reg"><span>数据目录</span><span>{status.data_dir}</span></div>
            <div className="admin-reg">
              <span>公开注册</span>
              <button className="btn-text" onClick={toggleReg}>{status.registration_open ? '已开启（点击关闭）' : '已关闭（点击开启）'}</button>
            </div>
            <div className="admin-reg"><span>后台路径</span><span>/{status.ops_path}</span></div>
            <button className="btn-text" disabled={busy} onClick={regenerate}>重新生成后台路径</button>
          </div>
          <div className="panel">
            <h2>设为平台超管</h2>
            <div className="field"><input value={newAdmin} onChange={(e) => setNewAdmin(e.target.value)} placeholder="输入用户名" /></div>
            <button className="btn" disabled={busy || !newAdmin.trim()} onClick={makeSuper}>提升为超管</button>
            <p className="admin-note">首次部署已自动生成平台超管。此处可将其它注册用户一并提升为超管。</p>
          </div>
        </>
      )}

      {tab === 'users' && (
        <div className="panel">
          {selectedUser ? (
            <UserDetail opsPath={opsPath} userID={selectedUser} onBack={() => setSelectedUser(null)} onChanged={load} />
          ) : (
            <>
              <h2>用户列表 <span className="more">点击查看详情</span></h2>
              {status.users.length === 0 && <p className="empty">暂无用户</p>}
              {status.users.map((u) => (
                <div key={u.user_id} className={`admin-user ${u.archived ? 'frozen' : ''}`} style={{ cursor: 'pointer' }} onClick={() => setSelectedUser(u.user_id)}>
                  <div className="admin-user-main">
                    <div className="admin-user-name">
                      {u.display_name}
                      <span className="admin-user-uid">@{u.username}</span>
                      {u.platform_role === 'superadmin' && <span className="badge">超管</span>}
                      {u.archived && <span className="badge badge-danger">已冻结</span>}
                    </div>
                    <div className="admin-user-meta">账本 {u.ledger_count} · 流水 {u.tx_count} · 注册于 {u.created_at.slice(0, 10)}</div>
                  </div>
                  <span className="btn-text">查看 →</span>
                </div>
              ))}
            </>
          )}
        </div>
      )}

      {tab === 'backups' && <Backups opsPath={opsPath} />}
      {tab === 'audit' && <AuditLog opsPath={opsPath} />}
    </div>
  )
}

function UserDetail({ opsPath, userID, onBack, onChanged }: { opsPath: string; userID: string; onBack: () => void; onChanged: () => void }) {
  const [d, setD] = useState<OpsUserDetail | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setD(await api.opsUserDetail(opsPath, userID))
      setErr('')
    } catch (e) {
      setErr((e as ApiError).message)
    }
  }, [opsPath, userID])

  useEffect(() => { void load() }, [load])

  async function freeze(freezeIt: boolean) {
    if (!confirm(freezeIt ? '冻结该用户？其会话将立即失效，无法再登录使用。' : '解冻该用户？')) return
    setBusy(true)
    try {
      await api.opsFreezeUser(opsPath, userID, freezeIt)
      await load(); onChanged()
    } catch (e) { setErr((e as ApiError).message) } finally { setBusy(false) }
  }

  async function resetPassword() {
    if (!confirm('为该用户重置密码？将生成随机新密码并强制其下次登录修改，当前会话全部失效。')) return
    setBusy(true)
    try {
      const r = await api.opsResetPassword(opsPath, userID)
      window.alert('新密码（仅此一次显示，请转交用户）：\n\n' + r.new_password)
      await load()
    } catch (e) { setErr((e as ApiError).message) } finally { setBusy(false) }
  }

  if (!d) return <p className="empty">{err || '加载中…'}</p>

  return (
    <>
      <h2>
        <button className="btn-text" onClick={onBack}>← 返回</button> {d.display_name}
        <span className="admin-user-uid"> @{d.username}</span>
        {d.archived && <span className="badge badge-danger">已冻结</span>}
      </h2>
      {err && <div className="alert" role="alert">{err}</div>}
      <div className="admin-reg"><span>注册时间</span><span>{d.created_at.slice(0, 10)}</span></div>
      <div className="admin-reg"><span>状态</span><span>{d.archived ? '已冻结' : '正常'}</span></div>
      <div style={{ display: 'flex', gap: 8, margin: '10px 0' }}>
        <button className="btn-text" disabled={busy} onClick={() => freeze(!d.archived)}>{d.archived ? '解冻用户' : '冻结用户'}</button>
        <button className="btn-text" disabled={busy} onClick={resetPassword}>重置密码</button>
      </div>

      <h2>账户（{d.accounts.filter((a) => !a.parent_id).length}）</h2>
      {d.accounts.filter((a) => !a.parent_id).length === 0 && <p className="empty">暂无账户</p>}
      {d.accounts.filter((a) => !a.parent_id).map((a) => (
        <div key={a.id} className="tx">
          <div className="icon">{ACCOUNT_TYPES[a.type]?.slice(0, 1) ?? '账'}</div>
          <div className="main">
            <div className="title">{a.name}</div>
            <div className="meta">{ACCOUNT_TYPES[a.type] ?? a.type}{a.archived ? ' · 已归档' : ''}</div>
          </div>
          <div className="amt">{formatCents(a.balance_cents)}</div>
        </div>
      ))}

      <h2 style={{ marginTop: 16 }}>流水（最近 {d.transactions.length} 笔）</h2>
      {d.transactions.length === 0 && <p className="empty">暂无流水</p>}
      {d.transactions.map((t) => (
        <div key={t.id} className="tx">
          <div className="icon">{t.type === 'expense' ? '支' : t.type === 'income' ? '收' : '转'}</div>
          <div className="main">
            <div className="title">{t.category_name ?? (t.type === 'transfer' ? '转账' : '未分类')}</div>
            <div className="meta">{t.business_date}{t.from_account_name ? ` · ${t.from_account_name}` : ''}{t.to_account_name ? ` → ${t.to_account_name}` : ''}{t.note ? ` · ${t.note}` : ''}</div>
          </div>
          <div className={`amt ${t.type === 'income' ? 'income' : ''}`}>{t.type === 'expense' ? '-' : ''}{formatCents(t.amount_cents)}</div>
        </div>
      ))}
    </>
  )
}

function Backups({ opsPath }: { opsPath: string }) {
  const [backups, setBackups] = useState<OpsBackup[]>([])
  const [dir, setDir] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await api.opsBackups(opsPath)
      setBackups(r.backups); setDir(r.dir); setErr('')
    } catch (e) { setErr((e as ApiError).message) }
  }, [opsPath])

  useEffect(() => { void load() }, [load])

  async function create() {
    setBusy(true)
    try {
      const r = await api.opsCreateBackup(opsPath)
      window.alert(`备份完成：${r.name}`)
      await load()
    } catch (e) { setErr((e as ApiError).message) } finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h2>数据备份</h2>
      {err && <div className="alert" role="alert">{err}</div>}
      <p className="admin-note">备份目录：<code>{dir}</code>。点击「立即备份」用 SQLite VACUUM INTO 生成一致快照。</p>
      <button className="btn" disabled={busy} onClick={create}>{busy ? '备份中…' : '立即备份'}</button>
      <div style={{ marginTop: 12 }}>
        {backups.length === 0 && <p className="empty">暂无备份</p>}
        {[...backups].reverse().map((b) => (
          <div key={b.name} className="admin-reg">
            <span>{b.name}</span>
            <span>{(b.size / 1024).toFixed(0)} KB · {b.created_at.slice(0, 19).replace('T', ' ')}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function AuditLog({ opsPath }: { opsPath: string }) {
  const [audit, setAudit] = useState<OpsAuditItem[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    (async () => {
      try {
        const r = await api.opsAudit(opsPath)
        setAudit(r.audit)
      } catch (e) { setErr((e as ApiError).message) }
    })()
  }, [opsPath])

  const label: Record<string, string> = {
    'ops.view_user': '查看用户数据', 'ops.freeze_user': '冻结用户', 'ops.unfreeze_user': '解冻用户',
    'ops.reset_password': '重置密码', 'ops.backup': '创建备份',
  }

  return (
    <div className="panel">
      <h2>审计日志 <span className="more">平台运维操作留痕</span></h2>
      {err && <div className="alert" role="alert">{err}</div>}
      {audit.length === 0 && <p className="empty">暂无审计记录</p>}
      {audit.map((a, i) => (
        <div key={i} className="admin-reg">
          <span><span className="badge">{label[a.action] ?? a.action}</span> {a.actor && `@${a.actor}`} {a.entity_type === 'user' ? `· 目标 ${a.entity_id.slice(0, 8)}` : ''}</span>
          <span>{a.created_at.slice(0, 19).replace('T', ' ')}</span>
        </div>
      ))}
    </div>
  )
}
