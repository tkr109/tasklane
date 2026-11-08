const pad = (n: number) => String(n).padStart(2, '0')

export const formatNumber = (n: number) => Math.round(n).toLocaleString('en-US')

export function formatShort(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1e3) return `${(n / 1e3).toFixed(1).replace(/\.0$/, '')}k`
  return String(Math.round(n))
}

export const formatRate = (n: number) => (n < 10 ? n.toFixed(1) : formatNumber(n))

export function formatClock(at: string | number | Date): string {
  const d = new Date(at)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function formatDuration(seconds: number): string {
  const s = Math.max(0, seconds)
  if (s < 1) return `${Math.max(1, Math.round(s * 1000))}ms`
  if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${pad(Math.floor(s % 60))}s`
  return `${Math.floor(s / 3600)}h ${pad(Math.floor((s % 3600) / 60))}m`
}

export function formatAgo(at: string | number, now = Date.now()): string {
  const s = (now - new Date(at).getTime()) / 1000
  if (s < 5) return 'just now'
  if (s < 60) return `${Math.floor(s)}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

export const secondsUntil = (at: string, now = Date.now()) => (new Date(at).getTime() - now) / 1000

export const plural = (n: number, word: string) => `${formatNumber(n)} ${word}${n === 1 ? '' : 's'}`

export const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
