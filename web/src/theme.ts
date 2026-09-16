// 外观偏好：主题（跟随系统/浅色/深色）与字号（标准/大字）。
// 偏好存 localStorage（纯展示设置，不含财务数据）；实际主题在
// <html data-theme> 上体现，CSS 变量随之切换；系统深色变化时
// 「跟随系统」自动跟随。

export type ThemePref = 'system' | 'light' | 'dark'
export type FontSizePref = 'standard' | 'large'

const THEME_KEY = 'xz-theme'
const FONT_KEY = 'xz-font-size'

const media = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null

function currentPref(): ThemePref {
  const v = localStorage.getItem(THEME_KEY)
  return v === 'light' || v === 'dark' ? v : 'system'
}

export function isDarkNow(): boolean {
  return document.documentElement.dataset.theme === 'dark'
}

function applyTheme(): void {
  const pref = currentPref()
  const dark = pref === 'dark' || (pref === 'system' && !!media?.matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', dark ? '#0f1713' : '#2fa87c')
  listeners.forEach((fn) => fn())
}

function applyFontSize(): void {
  const v = localStorage.getItem(FONT_KEY)
  document.documentElement.dataset.fontsize = v === 'large' ? 'large' : 'standard'
}

export function getThemePref(): ThemePref { return currentPref() }
export function getFontSizePref(): FontSizePref {
  return localStorage.getItem(FONT_KEY) === 'large' ? 'large' : 'standard'
}

export function setThemePref(pref: ThemePref): void {
  localStorage.setItem(THEME_KEY, pref)
  applyTheme()
}

export function setFontSizePref(pref: FontSizePref): void {
  localStorage.setItem(FONT_KEY, pref)
  applyFontSize()
}

// initTheme 在应用挂载前调用（见 index.html 内联脚本的同款逻辑，
// 这里兜底一次）；并监听系统主题变化。
export function initTheme(): void {
  applyFontSize()
  applyTheme()
  media?.addEventListener('change', () => {
    if (currentPref() === 'system') applyTheme()
  })
}

type Listener = () => void
const listeners = new Set<Listener>()

// subscribeTheme 返回当前是否深色；供 ECharts 等 canvas 组件取色。
export function subscribeTheme(fn: Listener): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
