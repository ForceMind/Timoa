import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type PlatformOverview } from './api'

// admin.tsx: 平台超管后台。只看跨用户统计与元数据（用户列表、账本数、
// 交易笔数、注册时间），不看任何账本明细。管理操作：冻结/解冻、重置密码、
// 开关公开注册。所有权限由服务端 requireSuperadmin 强制。

export function AdminPanel({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<PlatformOverview | null>(null)
  const [regOpen, setRegOpen] = useState<boolean | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')

  const load = useCallback(async () => {
    try {
      const [ov, rs] = await Promise.all([api.platformOverview(), api.registrationStatus()])
      setData(ov)
      setRegOpen(rs.registration_open)
      setErr('')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function toggleArchive(userID: string, archived: boolean) {
    setBusy(userID)
    try {
      await api.platformSetArchived(userID, archived)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    } finally { setBusy('') }
  }

  async function resetPassword(userID: string, username: string) {
    const pwd = window.prompt(`为用户 ${username} 设置新密码（至少 8 位）：`)
    if (!pwd) return
    if (pwd.length < 8) { setErr('密码至少 8 位'); return }
    setBusy(userID)
    try {
      await api.platformResetPassword(userID, pwd)
      setErr('')
      window.alert(`已重置 ${username} 的密码，其所有会话已失效`)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '重置失败')
    } finally { setBusy('') }
  }

  async function toggleRegistration() {
    if (regOpen === null) return
    try {
      const r = await api.platformSetRegistration(!regOpen)
      setRegOpen(r.registration_open)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }

  return (
    <div className="shell">
      <div className="panel">
        <h2>平台管理 <button className="more btn-text" onClick={onClose}>返回</button></h2>
        <p className="admin-note">平台视角仅含统计与元数据，不含任何用户账目明细。</p>
        {err && <div className="alert" role="alert">{err}</div>}
        {data && (
          <div className="stat-grid">
            <Stat label="注册用户" value={data.stats.user_count} />
            <Stat label="账本总数" value={data.stats.ledger_count} />
            <Stat label="流水笔数" value={data.stats.tx_count} />
          </div>
        )}
        <div className="admin-reg">
          <span>公开注册</span>
          <button className="btn-text" onClick={toggleRegistration} disabled={regOpen === null}>
            {regOpen === null ? '…' : regOpen ? '已开启（点击关闭）' : '已关闭（点击开启）'}
          </button>
        </div>
      </div>

      <div className="panel">
        <h2>用户列表</h2>
        {!data && <p className="empty">加载中…</p>}
        {data && data.users.length === 0 && <p className="empty">暂无用户</p>}
        {data?.users.map((u) => (
          <div key={u.user_id} className={`admin-user ${u.archived ? 'frozen' : ''}`}>
            <div className="admin-user-main">
              <div className="admin-user-name">
                {u.display_name}
                <span className="admin-user-uid">@{u.username}</span>
                {u.platform_role === 'superadmin' && <span className="badge">超管</span>}
                {u.archived && <span className="badge badge-danger">已冻结</span>}
              </div>
              <div className="admin-user-meta">
                账本 {u.ledger_count} · 流水 {u.tx_count} · 注册于 {u.created_at.slice(0, 10)}
              </div>
            </div>
            <div className="admin-user-ops">
              <button className="btn-text" disabled={busy === u.user_id} onClick={() => resetPassword(u.user_id, u.username)}>重置密码</button>
              <button className="btn-text" disabled={busy === u.user_id} onClick={() => toggleArchive(u.user_id, !u.archived)}>
                {u.archived ? '解冻' : '冻结'}
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat-cell">
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}
