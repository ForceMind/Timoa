import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { sync } from './sync'

// 全局错误兜底：渲染期异常显示出来而不是白屏。
window.addEventListener('error', (e) => {
  const root = document.getElementById('root')
  if (root && root.innerHTML.trim() === '') {
    root.innerHTML = `<pre style="padding:24px;color:#b00;white-space:pre-wrap">页面出现错误：${String(e.error?.stack ?? e.message)}</pre>`
  }
})

// PWA：注册应用壳 Service Worker（只缓存公开壳，不缓存 API/财务数据）
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(() => {})
}
// 离线队列初始化（可信设备开关在「我的 → 设置」）
sync.init().catch(() => {})
if (import.meta.env.DEV) {
  ;(window as unknown as { __sync: typeof sync }).__sync = sync
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

