import { useCallback, useEffect, useState, type ReactElement } from 'react'
import { api, ApiError, type Me, type OpsStatus } from './api'

// opspanel.tsx: 独立运营面板（随机路径入口 + 登录 + 平台超管）。
// 通过 location.pathname 命中 /{ops_path} 渲染；未登录先内嵌登录，登录后按超管校验。
// 样式复用现有 shell/panel/stat-grid/admin-user 等 class；Login 由 App 注入以避免循环依赖。

type LoginProps = { onLogin: (m: Me) => void }

export function OpsPanel({ opsPath, LoginView }: { opsPath: string; LoginView: (p: LoginProps) => ReactElement }) {
  const [status, setStatus] = useState<OpsStatus | null>(null)
  const [phase, setPhase] = useState<'loading' | 'login' | 'ready'>('loading')
  const [err, setErr] = useState('')
  const [newAdmin, setNewAdmin] = useState('')
  const [busy, setBusy] = useState(false)

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
        <p className="admin-note">平台视角仅含统计与元数据，不含任何账目明细。入口为固定随机路径，请妥善保管。</p>
        {err && <div className="alert" role="alert">{err}</div>}
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
        <div className="field">
          <input value={newAdmin} onChange={(e) => setNewAdmin(e.target.value)} placeholder="输入用户名" />
        </div>
        <button className="btn" disabled={busy || !newAdmin.trim()} onClick={makeSuper}>提升为超管</button>
        <p className="admin-note">首次部署已自动生成平台超管（见服务器 <code>initial-admin.txt</code> 或 <code>xiaozhang panel</code> 输出）。此处可将其它注册用户一并提升为超管。</p>
      </div>

      <div className="panel">
        <h2>用户列表</h2>
        {status.users.length === 0 && <p className="empty">暂无用户</p>}
        {status.users.map((u) => (
          <div key={u.user_id} className={`admin-user ${u.archived ? 'frozen' : ''}`}>
            <div className="admin-user-main">
              <div className="admin-user-name">
                {u.display_name}
                <span className="admin-user-uid">@{u.username}</span>
                {u.platform_role === 'superadmin' && <span className="badge">超管</span>}
                {u.archived && <span className="badge badge-danger">已冻结</span>}
              </div>
              <div className="admin-user-meta">账本 {u.ledger_count} · 流水 {u.tx_count} · 注册于 {u.created_at.slice(0, 10)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
