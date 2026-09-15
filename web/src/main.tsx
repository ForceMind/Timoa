import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

// 全局错误兜底：渲染期异常显示出来而不是白屏。
window.addEventListener('error', (e) => {
  const root = document.getElementById('root')
  if (root && root.innerHTML.trim() === '') {
    root.innerHTML = `<pre style="padding:24px;color:#b00;white-space:pre-wrap">页面出现错误：${String(e.error?.stack ?? e.message)}</pre>`
  }
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
