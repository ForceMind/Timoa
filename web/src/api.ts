// API client. All ledger amounts travel as decimal integer-cent strings;
// the UI converts at the edges with integer-safe parsing only.

export class ApiError extends Error {
  code: string
  status: number
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function req<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    ...options,
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    const e = (body as { error?: { code?: string; message?: string } }).error
    throw new ApiError(res.status, e?.code ?? 'unknown', e?.message ?? `HTTP ${res.status}`)
  }
  return body as T
}

export interface Me {
  user_id: string
  username: string
  display_name: string
  ledger_id: string
  ledger_name: string
  role: 'admin' | 'member'
}

export interface Account {
  id: string
  name: string
  type: string
  opening_balance_cents: string
  balance_cents: string
  balance_confirmed: boolean
  balance_unconfirmed: boolean
  archived: boolean
}

export interface Category {
  id: string
  parent_id?: string
  kind: 'expense' | 'income'
  name: string
  archived: boolean
}

export interface Tx {
  id: string
  type: 'expense' | 'income' | 'transfer'
  status: string
  business_date: string
  amount_cents: string
  category_name?: string
  from_account_name?: string
  to_account_name?: string
  note?: string
  merchant?: string
}

export interface Summary {
  income_cents: string
  expense_cents: string
  net_cents: string
  as_of: string
}

export const api = {
  setupStatus: () => req<{ needs_init: boolean }>('/api/v1/setup/status'),
  login: (username: string, password: string) =>
    req<{ user_id: string }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  logout: () => req<{ ok: boolean }>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => req<Me>('/api/v1/auth/me'),
  accounts: () => req<{ accounts: Account[] }>('/api/v1/accounts'),
  createAccount: (b: { name: string; type: string; opening_balance?: string; opening_date?: string; balance_confirmed?: boolean }) =>
    req<Account>('/api/v1/accounts', { method: 'POST', body: JSON.stringify(b) }),
  categories: (kind: 'expense' | 'income') => req<{ categories: Category[] }>(`/api/v1/categories?kind=${kind}`),
  transactions: (limit = 30) => req<{ transactions: Tx[] }>(`/api/v1/transactions?limit=${limit}`),
  post: (b: Record<string, unknown>) =>
    req<{ tx_id: string; replayed: boolean }>('/api/v1/transactions', { method: 'POST', body: JSON.stringify(b) }),
  summary: (from: string, to: string) => req<Summary>(`/api/v1/summary?from=${from}&to=${to}`),
}

export const ACCOUNT_TYPES: Record<string, string> = {
  cash: '现金',
  bank_card: '银行卡',
  wechat_change: '微信零钱',
  alipay_balance: '支付宝余额',
  stored_value: '储值卡',
  credit_card: '信用卡',
  huabei: '花呗',
  other_asset: '其他资产',
  loan_liability: '借款负债',
}
