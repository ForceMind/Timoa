import { meta, outbox, type OutboxOp } from './db'

// 同步管理器：本地草稿与操作队列 + 服务器确认。
// - 可信设备由用户主动启用离线缓存（共享设备默认不保存）
// - 网络恢复、回到前台、重新打开、手动触发都会同步
// - 服务端幂等是最终保障；世代不一致时停止推送并提示全量重同步

export type SyncStatus = 'disabled' | 'offline' | 'pending' | 'synced' | 'generation_mismatch' | 'error'

export interface SyncState {
  status: SyncStatus
  pending: number
  lastSync?: string
}

type Listener = (s: SyncState) => void

class SyncManager {
  enabled = false
  deviceID = ''
  listeners = new Set<Listener>()
  state: SyncState = { status: 'disabled', pending: 0 }
  flushing = false

  async init() {
    this.deviceID = (await meta.get('device_id')) ?? ''
    if (!this.deviceID) {
      this.deviceID = crypto.randomUUID()
      await meta.set('device_id', this.deviceID)
    }
    this.enabled = (await meta.get('offline_enabled')) === '1'
    window.addEventListener('online', () => this.flush())
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') this.flush()
    })
    await this.refresh()
    if (this.enabled) this.flush()
  }

  async setEnabled(on: boolean) {
    this.enabled = on
    await meta.set('offline_enabled', on ? '1' : '0')
    if (!on) await outbox.clear() // 关闭时清空本机队列（共享设备不留财务副本）
    await this.refresh()
    if (on) this.flush()
  }

  subscribe(fn: Listener) {
    this.listeners.add(fn)
    fn(this.state)
    return () => { this.listeners.delete(fn) }
  }

  private emit() {
    for (const fn of this.listeners) fn(this.state)
  }

  async refresh() {
    const pending = this.enabled ? (await outbox.all()).length : 0
    const lastSync = (await meta.get('last_sync')) ?? undefined
    let status: SyncStatus = 'disabled'
    if (this.enabled) {
      status = !navigator.onLine ? 'offline' : pending > 0 ? 'pending' : 'synced'
    }
    this.state = { status, pending, lastSync }
    this.emit()
  }

  // 离线保存：只有 IndexedDB 事务成功才算本机已保存
  async queue(op: Omit<OutboxOp, 'created_at' | 'device'>): Promise<void> {
    await outbox.add({ ...op, created_at: Date.now(), device: this.deviceID })
    await this.refresh()
  }

  async flush(): Promise<void> {
    if (!this.enabled || this.flushing || !navigator.onLine) return
    this.flushing = true
    try {
      const ops = await outbox.all()
      if (ops.length === 0) {
        await this.pull()
        await this.refresh()
        return
      }
      const generation = (await meta.get('generation')) ?? ''
      const res = await fetch('/api/v1/sync/push', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ generation, ops }),
      })
      if (res.status === 409) {
        this.state = { ...this.state, status: 'generation_mismatch' }
        this.emit()
        return // 停止推送，等待用户全量重同步
      }
      if (res.status === 401) {
        await this.refresh()
        return // 会话过期：暂停同步，重新认证后再继续
      }
      const data = await res.json()
      for (const r of data.results ?? []) {
        if (r.ok || r.code === 'idempotency_conflict' || r.code?.endsWith('_exceeded') || r.code === 'invalid_input' || r.code === 'invalid_amount' || r.code === 'account_archived' || r.code === 'category_not_found' || r.code === 'account_not_found') {
          // 成功/已入账 或 服务端明确拒绝（重试无意义）→ 出队
          // 明确拒绝的留在结果里由对账处理，不静默丢弃：先出队并记录
          await outbox.remove(r.operation_id)
        }
      }
      if (data.generation) await meta.set('generation', data.generation)
      await this.pull()
      await meta.set('last_sync', new Date().toISOString())
      await this.refresh()
    } catch {
      this.state = { ...this.state, status: navigator.onLine ? 'error' : 'offline' }
      this.emit()
    } finally {
      this.flushing = false
    }
  }

  // 全量重同步（世代不一致后）：清空本地队列，重置游标
  async fullResync(): Promise<void> {
    await outbox.clear()
    await meta.set('cursor', '0')
    await meta.set('generation', '')
    await this.pull()
    await meta.set('last_sync', new Date().toISOString())
    await this.refresh()
  }

  private async pull(): Promise<void> {
    const cursor = (await meta.get('cursor')) ?? '0'
    const res = await fetch(`/api/v1/sync/pull?since=${encodeURIComponent(cursor)}`, { credentials: 'same-origin' })
    if (!res.ok) return
    const data = await res.json()
    if (data.generation) await meta.set('generation', data.generation)
    if (typeof data.cursor === 'number') await meta.set('cursor', String(data.cursor))
  }
}

export const sync = new SyncManager()
