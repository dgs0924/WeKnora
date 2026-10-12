import type { CodexUsageWindow } from './codex'

export function usagePercent(value: number | null): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  return Math.max(0, Math.min(100, Math.round(value)))
}

export function usageWindowDuration(minutes: number | null, locale: string): string | null {
  if (!minutes || minutes <= 0) return null
  if (minutes % 1440 === 0) return new Intl.NumberFormat(locale, { style: 'unit', unit: 'day', unitDisplay: 'long' }).format(minutes / 1440)
  if (minutes % 60 === 0) return new Intl.NumberFormat(locale, { style: 'unit', unit: 'hour', unitDisplay: 'long' }).format(minutes / 60)
  return new Intl.NumberFormat(locale, { style: 'unit', unit: 'minute', unitDisplay: 'long' }).format(minutes)
}

export function usageResetAt(seconds: number | null, locale: string): string | null {
  if (!seconds || seconds <= 0) return null
  const date = new Date(seconds * 1000)
  if (Number.isNaN(date.getTime())) return null
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

export function usageUpdatedAt(value: string | undefined, locale: string): string | null {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

export function usableUsageWindow(window: CodexUsageWindow): boolean {
  return usagePercent(window.used_percent) !== null || usagePercent(window.remaining_percent) !== null
}
