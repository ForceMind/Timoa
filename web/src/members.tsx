import { useEffect, useState } from 'react'
import { api, ApiError, type Invite, type Member } from './api'

// 我的 → 成员：管理员创建一次性邀请（令牌只显示一次）、撤销邀请、
// 撤销成员（服务端拒绝其后续请求）。

export function MembersPanel({ meID, isAdmin, onChanged }: { meID: string; isAdmin: boolean; onChanged: () => void }) {
  const [members, setMembers] = useState<Member[]>([])
  const [invites, setInvites] = useState<Invite[]>([])
  const [newToken, setNewToken] = useState('')
  const [err, setErr] = useState('')

  const load = () => {
    api.members().then((r) => setMembers(r.members ?? [])).catch(() => {})
    if (isAdmin) api.invites().then((r) => setInvites(r.invites ?? [])).catch(() => {})
  }
  useEffect(() => { load() }, [isAdmin])

  async function invite() {
    setErr(''); setNewToken('')
    try {
      const r = await api.createInvite()
      setNewToken(`${location.origin}/?join=${r.token}`)
      load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    }
  }

  return (
    <div className="panel">
      <h2>家庭成员 <span className="more">账本内成员共享可见</span></h2>
      {err && <div className="alert">{err}</div>}
      {members.map((m) => (
        <div className="tx" key={m.user_id}>
          <div className="icon" style={{ background: 'var(--primary-soft)' }}>☺</div>
          <div className="main">
            <div className="title">{m.display_name}{m.archived ? '（已撤销）' : ''}</div>
            <div className="meta">@{m.username} · {m.role === 'admin' ? '管理员' : '成员'}</div>
          </div>
          {isAdmin && !m.archived && m.user_id !== meID && (
            <button className="btn-text" onClick={async () => { await api.revokeMember(m.user_id); load(); onChanged() }}>撤销</button>
          )}
        </div>
      ))}
      {isAdmin && (
        <>
          <button className="btn-text" onClick={invite}>＋ 邀请家人（生成一次性链接，72 小时有效）</button>
          {newToken && (
            <div className="notice" style={{ wordBreak: 'break-all' }}>
              邀请链接（仅显示这一次，请立即发给家人）：<br /><strong>{newToken}</strong>
            </div>
          )}
          {invites.filter((i) => !i.used && !i.revoked).length > 0 && (
            <>
              <div className="date-group">待使用的邀请</div>
              {invites.filter((i) => !i.used && !i.revoked).map((i) => (
                <div className="tx" key={i.id}>
                  <div className="main"><div className="meta">创建于 {i.created_at.slice(0, 10)} · {i.expires_at.slice(0, 10)} 过期</div></div>
                  <button className="btn-text" onClick={async () => { await api.revokeInvite(i.id); load() }}>撤销</button>
                </div>
              ))}
            </>
          )}
        </>
      )}
    </div>
  )
}
