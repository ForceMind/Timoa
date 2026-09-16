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
  parent_id?: string
  sub_kind?: 'current' | 'deposit' | 'investment'
}

export interface Category {
  id: string
  parent_id?: string
  kind: 'expense' | 'income'
  name: string
  icon?: string
  archived: boolean
}

export interface Tx {
  id: string
  type: 'expense' | 'income' | 'transfer'
  status: string
  business_date: string
  amount_cents: string
  category_id?: string
  category_name?: string
  from_account_id?: string
  from_account_name?: string
  to_account_id?: string
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
  createSubAccount: (parentID: string, b: { name: string; sub_kind: string; opening_balance?: string; opening_date?: string; balance_confirmed?: boolean }) =>
    req<Account>(`/api/v1/accounts/${parentID}/sub`, { method: 'POST', body: JSON.stringify(b) }),
  categories: (kind: 'expense' | 'income') => req<{ categories: Category[] }>(`/api/v1/categories?kind=${kind}`),
  transactions: (limit = 30) => req<{ transactions: Tx[] }>(`/api/v1/transactions?limit=${limit}`),
  post: (b: Record<string, unknown>) =>
    req<{ tx_id: string; replayed: boolean }>('/api/v1/transactions', { method: 'POST', body: JSON.stringify(b) }),
  summary: (from: string, to: string) => req<Summary>(`/api/v1/summary?from=${from}&to=${to}`),
  statsDaily: (from: string, to: string) => req<{ days: DailySum[] }>(`/api/v1/stats/daily?from=${from}&to=${to}`),
  txDetail: (id: string) => req<TxDetail>(`/api/v1/transactions/${id}`),
  refund: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/refund`, { method: 'POST', body: JSON.stringify(b) }),
  incomeRefund: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/income-refund`, { method: 'POST', body: JSON.stringify(b) }),
  settle: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/settle`, { method: 'POST', body: JSON.stringify(b) }),
  reclass: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/reclass`, { method: 'POST', body: JSON.stringify(b) }),
  writeoff: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/writeoff`, { method: 'POST', body: JSON.stringify(b) }),
  revise: (id: string, b: Record<string, unknown>) =>
    req<{ tx_id: string }>(`/api/v1/transactions/${id}/revise`, { method: 'POST', body: JSON.stringify(b) }),
  overview: (from: string, to: string) => req<Overview>(`/api/v1/stats/overview?from=${from}&to=${to}`),
  statsCategories: (from: string, to: string, basis: string) =>
    req<{ basis: string; categories: CategoryNet[] }>(`/api/v1/stats/categories?from=${from}&to=${to}&basis=${basis}`),
  receivables: () => req<{ receivables: Receivable[] }>(`/api/v1/receivables`),
  setBudget: (b: Record<string, unknown>) => req('/api/v1/budgets', { method: 'POST', body: JSON.stringify(b) }),
  budgetStatus: (month: string) => req<{ budgets: BudgetStatus[] }>(`/api/v1/budgets/status?month=${month}`),
  deleteBudget: (id: string) => req(`/api/v1/budgets/${id}/delete`, { method: 'POST' }),
  createGoal: (b: Record<string, unknown>) => req('/api/v1/goals', { method: 'POST', body: JSON.stringify(b) }),
  goals: () => req<{ goals: SavingsGoal[] }>('/api/v1/goals'),
  doneGoal: (id: string, done: boolean) => req(`/api/v1/goals/${id}/done`, { method: 'POST', body: JSON.stringify({ done }) }),
  assets: () => req<AssetsOverview>('/api/v1/stats/assets'),
  dismissRecommendation: (kind: string, key: string) => req('/api/v1/recommendations/dismiss', { method: 'POST', body: JSON.stringify({ kind, key }) }),
  merchantSuggest: (q: string) => req<{ suggestion: MerchantSuggestion | null }>(`/api/v1/suggest/merchant?q=${encodeURIComponent(q)}`),
  join: (b: { token: string; username: string; password: string; display_name?: string }) =>
    req<{ user_id: string }>('/api/v1/auth/join', { method: 'POST', body: JSON.stringify(b) }),
  createInvite: () => req<{ token: string; invite: Invite }>('/api/v1/invites', { method: 'POST' }),
  invites: () => req<{ invites: Invite[] }>('/api/v1/invites'),
  revokeInvite: (id: string) => req(`/api/v1/invites/${id}/revoke`, { method: 'POST' }),
  members: () => req<{ members: Member[] }>('/api/v1/members'),
  revokeMember: (id: string) => req(`/api/v1/members/${id}/revoke`, { method: 'POST' }),
  forecast: () => req<Forecast>('/api/v1/stats/forecast'),
  calendar: (month: string) => req<{ days: CalendarDay[] }>(`/api/v1/calendar?month=${month}`),
  templates: () => req<{ templates: Template[] }>('/api/v1/templates'),
  pinTemplate: (id: string, pinned: boolean) => req(`/api/v1/templates/${id}/pin`, { method: 'POST', body: JSON.stringify({ pinned }) }),
  enableTemplate: (id: string, enabled: boolean) => req(`/api/v1/templates/${id}/enable`, { method: 'POST', body: JSON.stringify({ enabled }) }),
  rules: () => req<{ rules: RecurrenceRule[] }>('/api/v1/recurrence/rules'),
  createRule: (b: Record<string, unknown>) => req('/api/v1/recurrence/rules', { method: 'POST', body: JSON.stringify(b) }),
  enableRule: (id: string, enabled: boolean, baseVersion = 0) => req(`/api/v1/recurrence/rules/${id}/enable`, { method: 'POST', body: JSON.stringify({ enabled, base_version: baseVersion }) }),
  pending: () => req<{ instances: RecurrenceInstance[] }>('/api/v1/recurrence/pending'),
  skipInstance: (id: string) => req(`/api/v1/recurrence/instances/${id}/skip`, { method: 'POST' }),
  postponeInstance: (id: string, date: string) => req(`/api/v1/recurrence/instances/${id}/postpone`, { method: 'POST', body: JSON.stringify({ date }) }),
  recommendations: () => req<{ recommendations: Recommendation[] }>('/api/v1/recommendations'),
  copyTx: (id: string) => req<{ tx_id: string }>(`/api/v1/transactions/${id}/copy`, { method: 'POST' }),
  search: (q: string) => req<{ transactions: Tx[] }>(`/api/v1/search?q=${encodeURIComponent(q)}`),
  lend: (b: Record<string, unknown>) => req('/api/v1/money/lend', { method: 'POST', body: JSON.stringify(b) }),
  borrow: (b: Record<string, unknown>) => req('/api/v1/money/borrow', { method: 'POST', body: JSON.stringify(b) }),
  repay: (b: Record<string, unknown>) => req('/api/v1/money/repay', { method: 'POST', body: JSON.stringify(b) }),
  loanRepay: (b: Record<string, unknown>) => req('/api/v1/money/loan-repay', { method: 'POST', body: JSON.stringify(b) }),
  redeem: (b: Record<string, unknown>) => req('/api/v1/money/redeem', { method: 'POST', body: JSON.stringify(b) }),
  notes: () => req<{ notes: Note[] }>('/api/v1/notes'),
  createNote: (content: string) => req<Note>('/api/v1/notes', { method: 'POST', body: JSON.stringify({ content }) }),
  updateNote: (id: string, content: string, pinned: boolean) => req(`/api/v1/notes/${id}`, { method: 'POST', body: JSON.stringify({ content, pinned }) }),
  deleteNote: (id: string) => req(`/api/v1/notes/${id}`, { method: 'DELETE' }),
}

export interface Note {
  id: string
  content: string
  pinned: boolean
  created_by: string
  created_at: string
  updated_at: string
}

export interface Template {
  id: string
  name: string
  icon?: string
  tx_type: string
  category_id?: string
  category_name?: string
  default_account_id?: string
  amount_policy: string
  fixed_amount_cents?: string
  pinned: boolean
  enabled: boolean
  is_seed: boolean
}

export interface RecurrenceRule {
  id: string
  name: string
  tx_type: string
  category_id?: string
  category_name?: string
  account_id?: string
  frequency: string
  interval_days?: number
  by_weekday?: number
  month_day?: number
  anchor_date: string
  start_date: string
  amount_policy: string
  fixed_amount_cents?: string
  enabled: boolean
  version?: number
}

export interface RecurrenceInstance {
  id: string
  rule_id: string
  rule_name?: string
  period_key: string
  planned_date: string
  planned_amount_cents?: string
  status: string
  confirmed_amount_cents: string
  tx_type?: string
  category_id?: string
  account_id?: string
}

export interface Recommendation {
  kind: 'recurrence' | 'habit' | 'recent' | 'pinned' | 'common'
  reason: string
  instance_id?: string
  rule_id?: string
  rule_name?: string
  category_id?: string
  category_name?: string
  icon?: string
  tx_type: string
  amount_hint_cents?: string
  account_id?: string
}

export interface BudgetStatus {
  id: string
  month: string
  category_id?: string
  category_name?: string
  amount_cents: string
  spent_cents: string
  remaining_cents: string
  percent: number
  over: boolean
  covers_children: boolean
}

export interface SavingsGoal {
  id: string
  name: string
  target_cents: string
  target_date?: string
  account_id?: string
  account_name?: string
  note?: string
  done: boolean
  saved_cents: string
  percent: number
}

export interface AssetsOverview {
  asset_cents: string
  liability_cents: string
  receivable_cents: string
  payable_cents: string
  net_worth_cents: string
  unconfirmed_count: number
  scope: string
}

export interface MerchantSuggestion {
  merchant: string
  category_id: string
  category_name: string
  use_count: number
  amount_hint_cents?: string
}

export interface Invite { id: string; expires_at: string; used: boolean; revoked: boolean; created_at: string }
export interface Member { user_id: string; username: string; display_name: string; role: string; archived: boolean; created_at: string }

export interface Forecast {
  month: string
  elapsed_days: number
  total_days: number
  spent_cents: string
  remaining_fixed_cents: string
  variable_estimate_cents: string
  expected_end_expense_cents: string
  available_funds_cents: string
  expected_income_cents: string
  expected_end_funds_cents: string
  gap: boolean
  insufficient: boolean
  basis: string
}

export interface CalendarDay {
  date: string
  expense_cents: string
  income_cents: string
  events?: { kind: string; name: string; status?: string; amount_cents?: string }[]
}

export interface DailySum {
  date: string
  income_cents: string
  expense_cents: string
}

export interface SplitView {
  id: string
  part_type: 'expense' | 'income' | 'receivable'
  category_id?: string
  category_name?: string
  counterparty?: string
  amount_cents: string
  refunded_cents: string
}

export interface RefundView {
  tx_id: string
  business_date: string
  amount_cents: string
  account_name?: string
  effective: boolean
}

export interface Revision {
  original_tx_id: string
  reversal_tx_id: string
  replacement_tx_id?: string
  reason: string
  created_at: string
}

export interface Receivable {
  original_tx_id: string
  counterparty: string
  business_date: string
  created_cents: string
  settled_cents: string
  written_off_cents: string
  refunded_cents: string
  outstanding_cents: string
  age_days: number
  fully_settled: boolean
}

export interface TxDetail extends Tx {
  splits?: SplitView[]
  refunds?: RefundView[]
  refunded_total_cents: string
  receivable?: Receivable
  reversed: boolean
  revision?: Revision
}

export interface Overview {
  gross_income_cents: string
  income_returns_cents: string
  net_income_cents: string
  gross_expense_cents: string
  refunds_cents: string
  net_expense_cents: string
  balance_cents: string
}

export interface CategoryNet {
  category_id: string
  category_name: string
  parent_id?: string
  net_cents: string
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
