// Integer-safe money helpers: the UI never accumulates floats.

// formatCents renders a cent string like "1234" as "12.34".
export function formatCents(cents: string | number): string {
  let c = BigInt(cents)
  let sign = ''
  if (c < 0n) {
    sign = '-'
    c = -c
  }
  const yuan = c / 100n
  const frac = (c % 100n).toString().padStart(2, '0')
  return `${sign}${yuan}.${frac}`
}

// parseYuan validates user input ("12.34") and returns cents as a string,
// or null when invalid (non-numeric, >2 decimals, zero, negative, range).
export function parseYuan(input: string): string | null {
  const s = input.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(s)) return null
  const [i, f = ''] = s.split('.')
  if (i.length > 12) return null
  const cents = BigInt(i) * 100n + BigInt(f.padEnd(2, '0') || '0')
  if (cents <= 0n) return null
  if (cents > 999_999_999_999n) return null
  return cents.toString()
}

export function parseYuanAllowZero(input: string): string | null {
  const s = input.trim()
  if (s === '') return '0'
  if (!/^\d+(\.\d{1,2})?$/.test(s)) return null
  const [i, f = ''] = s.split('.')
  if (i.length > 12) return null
  const cents = BigInt(i) * 100n + BigInt(f.padEnd(2, '0') || '0')
  if (cents > 999_999_999_999n) return null
  return cents.toString()
}
