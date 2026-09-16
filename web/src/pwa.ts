// pwa.ts: PWA 安装引导（T47）。
//
// 按浏览器能力给出引导，不做假开关：
// - Chromium（Android/桌面）：监听 beforeinstallprompt，可一键调起原生安装；
//   已安装（standalone）或未触发事件时给出对应说明。
// - iOS/iPadOS Safari：没有安装事件，只能引导「分享 → 添加到主屏幕」；
//   是真机体验差异，浏览器模拟不能冒充（真机验证待交付）。
// 事件由 main.tsx 尽早注册监听捕获，模块级持有。

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

let deferredPrompt: BeforeInstallPromptEvent | null = null
const listeners = new Set<() => void>()

function notify() {
  for (const fn of listeners) fn()
}

// initPWAInstall 尽早注册监听（beforeinstallprompt 可能在 React 挂载前触发）。
export function initPWAInstall() {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt = e as BeforeInstallPromptEvent
    notify()
  })
  window.addEventListener('appinstalled', () => {
    deferredPrompt = null
    notify()
  })
}

export function subscribeInstall(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export type InstallState = 'installed' | 'promptable' | 'ios' | 'unavailable'

export function installState(): InstallState {
  if (window.matchMedia('(display-mode: standalone)').matches
    || (navigator as unknown as { standalone?: boolean }).standalone === true) {
    return 'installed'
  }
  if (deferredPrompt) return 'promptable'
  const ua = navigator.userAgent
  if (/iP(hone|ad|od)/.test(ua)
    || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) {
    return 'ios'
  }
  return 'unavailable'
}

// promptInstall 调起原生安装提示；返回用户是否接受。
export async function promptInstall(): Promise<boolean> {
  if (!deferredPrompt) return false
  await deferredPrompt.prompt()
  const { outcome } = await deferredPrompt.userChoice
  if (outcome === 'accepted') {
    deferredPrompt = null
  }
  notify()
  return outcome === 'accepted'
}
