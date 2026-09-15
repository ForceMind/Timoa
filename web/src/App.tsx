import { useEffect, useState } from 'react'

interface Meta {
  name: string
  version: string
  api: string
}

// Stage-0 shell: proves the front-end builds, renders and reaches the API.
// The real two-level information architecture lands in later stages.
export default function App() {
  const [meta, setMeta] = useState<Meta | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/v1/meta')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then(setMeta)
      .catch((e) => setError(String(e)))
  }, [])

  return (
    <main style={{ fontFamily: 'system-ui, sans-serif', padding: 24, maxWidth: 480, margin: '0 auto' }}>
      <h1>小账</h1>
      <p>日常小账，心里有数。</p>
      {meta && (
        <p>
          后端已连接：{meta.name} · API {meta.api} · 版本 {meta.version}
        </p>
      )}
      {error && <p role="alert">后端未连接：{error}</p>}
    </main>
  )
}
