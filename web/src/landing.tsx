import { useEffect, useState } from 'react'
import { api, ApiError, type Me } from './api'
import { brand } from './brand'
import { InstallPromptOnce } from './installprompt'

// landing.tsx: 官网落地页（未登录首屏）+ 公开注册。
// 落地页是产品介绍；注册/登录从这里进入。账目数据完全按账本隔离，
// 本页不触碰任何财务数据。

export function Landing({ onLogin, onGoLogin }: { onLogin: (m: Me) => void; onGoLogin: () => void }) {
  const [regOpen, setRegOpen] = useState(false)
  const [showRegister, setShowRegister] = useState(false)

  useEffect(() => {
    api.registrationStatus().then((s) => setRegOpen(s.registration_open)).catch(() => {})
  }, [])

  if (showRegister && regOpen) {
    return <Register onRegistered={onLogin} onBack={() => setShowRegister(false)} />
  }

  return (
    <div className="landing">
      <header className="landing-hero">
        <img src="/icons/icon-192.png" alt="Timoa" className="landing-logo" />
        <h1>{brand.name}</h1>
        <p className="landing-slogan">{brand.slogan}</p>
        <p className="landing-sub">{brand.sloganSub}</p>
        <div className="landing-actions">
          {regOpen && (
            <button className="btn" onClick={() => setShowRegister(true)}>免费注册</button>
          )}
          <button className="btn btn-ghost" onClick={onGoLogin}>登录</button>
        </div>
      </header>

      <section className="landing-features">
        <Feature icon="💰" title="复式记账" desc="借贷平衡、不可变留痕，每一笔钱都有据可查" />
        <Feature icon="👨‍👩‍👧" title="家庭共享" desc="邀请家人进同一账本，共同记账、各自视角" />
        <Feature icon="📊" title="预算与统计" desc="分类预算、收支趋势、资产负债一目了然" />
        <Feature icon="📥" title="导入导出" desc="微信/支付宝账单导入，CSV/Excel 导出备份" />
        <Feature icon="🔁" title="周期与分摊" desc="房租定投自动提醒，大额支出按期分摊" />
        <Feature icon="🔒" title="私密自托管" desc="数据在你自己的服务器，离线也能记" />
      </section>

      <footer className="landing-foot">
        <p>{brand.en}</p>
      </footer>
    </div>
  )
}

function Feature({ icon, title, desc }: { icon: string; title: string; desc: string }) {
  return (
    <div className="feature">
      <div className="feature-icon">{icon}</div>
      <div className="feature-title">{title}</div>
      <div className="feature-desc">{desc}</div>
    </div>
  )
}

export function Register({ onRegistered, onBack }: { onRegistered: (m: Me) => void; onBack: () => void }) {
  const [username, setUsername] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [password2, setPassword2] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [newUser, setNewUser] = useState<Me | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (password !== password2) { setErr('两次输入的密码不一致'); return }
    setBusy(true); setErr('')
    try {
      await api.register({ username, password, display_name: name || undefined })
      // 注册成功 → 先弹一次性 PWA 安装引导，关闭后再进入应用
      setNewUser(await api.me())
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '注册失败，请稍后重试')
    } finally { setBusy(false) }
  }

  if (newUser) {
    return <InstallPromptOnce onDone={() => onRegistered(newUser)} />
  }

  return (
    <div className="login-wrap">
      <img src="/icons/icon-192.png" alt="Timoa" style={{ width: 72, height: 72, borderRadius: 18, marginBottom: 14 }} />
      <h1>注册 {brand.shortName}</h1>
      <p className="slogan">创建你的专属账本，数据完全属于你。</p>
      {err && <div className="alert" role="alert">{err}</div>}
      <form onSubmit={submit}>
        <div className="field"><label htmlFor="rn">昵称</label>
          <input id="rn" value={name} onChange={(e) => setName(e.target.value)} placeholder="怎么称呼你" /></div>
        <div className="field"><label htmlFor="ru">用户名</label>
          <input id="ru" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required /></div>
        <div className="field"><label htmlFor="rp">密码（至少 8 位）</label>
          <input id="rp" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} /></div>
        <div className="field"><label htmlFor="rp2">确认密码</label>
          <input id="rp2" type="password" value={password2} onChange={(e) => setPassword2(e.target.value)} autoComplete="new-password" required minLength={8} /></div>
        <button className="btn" disabled={busy}>{busy ? '注册中…' : '注册并开始'}</button>
        <button type="button" className="btn-text" onClick={onBack}>返回首页</button>
      </form>
    </div>
  )
}
