import { useEffect, useState } from 'react'
import { installState, promptInstall, subscribeInstall, type InstallState } from './pwa'
import { brand } from './brand'

// installprompt.tsx: 注册成功后的一次性 PWA 安装引导弹窗。
// - 只提示一次：localStorage xz_install_prompted 标记，之后不再出现；
// - 用户不安装则告知可在「我的 → 设置」里随时安装；
// - 已安装（standalone）则不弹。
// Chromium 调起原生安装；iOS 引导「分享 → 添加到主屏幕」。

const KEY = 'xz_install_prompted'

export function markInstallPrompted() {
  try { localStorage.setItem(KEY, '1') } catch { /* ignore */ }
}

export function InstallPromptOnce({ onDone }: { onDone: () => void }) {
  const [state, setState] = useState<InstallState>(installState())

  useEffect(() => subscribeInstall(() => setState(installState())), [])

  // 已安装则无需提示
  useEffect(() => {
    if (state === 'installed') { markInstallPrompted(); onDone() }
  }, [state, onDone])

  async function install() {
    if (state === 'promptable') {
      await promptInstall()
    }
    markInstallPrompted()
    onDone()
  }

  function later() {
    markInstallPrompted()
    onDone()
  }

  if (state === 'installed') return null

  return (
    <div className="ip-overlay" role="dialog" aria-modal="true">
      <div className="ip-card">
        <div className="ip-icon">📲</div>
        <h2 className="ip-title">把{brand.shortName}装到手机</h2>
        {state === 'ios' ? (
          <p className="ip-desc">
            点浏览器底部分享按钮，选择「添加到主屏幕」，即可像 App 一样使用，离线也能记账。
          </p>
        ) : (
          <p className="ip-desc">
            安装到主屏幕，像 App 一样打开，离线也能记账。
          </p>
        )}
        {state === 'promptable' ? (
          <button className="btn" onClick={install}>立即安装</button>
        ) : state === 'ios' ? (
          <button className="btn" onClick={later}>我知道了</button>
        ) : (
          <button className="btn" onClick={later}>好的</button>
        )}
        <button className="btn-text" onClick={later}>
          暂不安装（可稍后在「我的 → 设置」里安装）
        </button>
      </div>
    </div>
  )
}
