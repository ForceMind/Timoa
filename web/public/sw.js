// 小账 Service Worker：只缓存公开应用壳（HTML/JS/CSS/图标），
// 不缓存 API 响应、凭据或任何财务数据。版本升级时更新缓存，
// 但不清空 IndexedDB 待同步队列。
const VERSION = 'xz-shell-v1'
const SHELL = ['/', '/manifest.webmanifest', '/icons/icon-192.png', '/icons/icon-512.png']

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(VERSION).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== VERSION).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url)
  if (url.origin !== location.origin) return

  // API 一律走网络（不缓存），失败由应用层离线队列处理
  if (url.pathname.startsWith('/api/') || url.pathname === '/healthz' || url.pathname === '/readyz') {
    return
  }

  // 导航：网络优先，离线回退到缓存的应用壳
  if (e.request.mode === 'navigate') {
    e.respondWith(
      fetch(e.request)
        .then((res) => {
          const copy = res.clone()
          caches.open(VERSION).then((c) => c.put('/', copy))
          return res
        })
        .catch(() => caches.match('/'))
    )
    return
  }

  // 静态资源（带指纹）：缓存优先，后台更新
  e.respondWith(
    caches.match(e.request).then((hit) => {
      const fetching = fetch(e.request).then((res) => {
        if (res.ok) {
          const copy = res.clone()
          caches.open(VERSION).then((c) => c.put(e.request, copy))
        }
        return res
      })
      return hit || fetching
    })
  )
})
