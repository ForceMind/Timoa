// IndexedDB 本地存储：离线操作队列 + 同步元数据。
// 浏览器缓存不是备份；未同步内容提供本机导出补救（exportOutbox）。

export interface OutboxOp {
  operation_id: string
  type: 'transaction'
  payload: Record<string, unknown>
  created_at: number
  device: string
}

const DB_NAME = 'xiaozhang'
const DB_VERSION = 1

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains('outbox')) {
        db.createObjectStore('outbox', { keyPath: 'operation_id' })
      }
      if (!db.objectStoreNames.contains('meta')) {
        db.createObjectStore('meta', { keyPath: 'key' })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function tx<T>(store: string, mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return open().then((db) => new Promise<T>((resolve, reject) => {
    const t = db.transaction(store, mode)
    const req = fn(t.objectStore(store))
    t.oncomplete = () => resolve(req.result)
    t.onerror = () => reject(t.error)
    t.onabort = () => reject(t.error)
  }))
}

export const outbox = {
  async add(op: OutboxOp): Promise<void> {
    await tx('outbox', 'readwrite', (s) => s.put(op))
  },
  async all(): Promise<OutboxOp[]> {
    const rows = await tx('outbox', 'readonly', (s) => s.getAll() as IDBRequest<OutboxOp[]>)
    return rows.sort((a, b) => a.created_at - b.created_at)
  },
  async remove(operationID: string): Promise<void> {
    await tx('outbox', 'readwrite', (s) => s.delete(operationID))
  },
  async clear(): Promise<void> {
    await tx('outbox', 'readwrite', (s) => s.clear())
  },
}

export const meta = {
  async get(key: string): Promise<string | null> {
    const row = await tx('meta', 'readonly', (s) => s.get(key) as IDBRequest<{ key: string; value: string } | undefined>)
    return row?.value ?? null
  },
  async set(key: string, value: string): Promise<void> {
    await tx('meta', 'readwrite', (s) => s.put({ key, value }))
  },
}

// 未同步内容的本机导出补救（避免靠截图救账）
export async function exportOutbox(): Promise<string> {
  const ops = await outbox.all()
  return JSON.stringify({ exported_at: new Date().toISOString(), ops }, null, 2)
}
