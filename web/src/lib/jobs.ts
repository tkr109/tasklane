import type { Job, JobEvent, JobStatus } from '../api/types'
import { formatClock, formatDuration, secondsUntil } from './format'

export const canRetry = (status: JobStatus) => status === 'failed' || status === 'dead' || status === 'cancelled'

export const canCancel = (status: JobStatus) => status === 'queued' || status === 'failed'

export type PriorityBand = 'high' | 'normal' | 'low'

export const priorityBand = (p: number): PriorityBand => (p >= 7 ? 'high' : p >= 3 ? 'normal' : 'low')

export const PRIORITY_BANDS: { value: PriorityBand; label: string }[] = [
  { value: 'high', label: 'Urgent (7 to 9)' },
  { value: 'normal', label: 'Normal (3 to 6)' },
  { value: 'low', label: 'Low (0 to 2)' },
]

export function progressPercent(job: Pick<Job, 'status' | 'progress'>): number {
  if (job.status === 'succeeded') return 100
  if (job.status === 'queued' || job.status === 'cancelled') return 0
  return Math.min(100, job.progress)
}

export function progressLabel(job: Pick<Job, 'status' | 'progress' | 'run_at'>, now: number): string {
  switch (job.status) {
    case 'running':
      return `${Math.min(99, Math.floor(job.progress))}%`
    case 'succeeded':
      return 'done'
    case 'queued': {
      const wait = secondsUntil(job.run_at, now)
      return wait > 0 ? `held ${formatDuration(wait)}` : 'waiting'
    }
    case 'failed':
      return `retry in ${formatDuration(secondsUntil(job.run_at, now))}`
    case 'dead':
      return 'gave up'
    case 'cancelled':
      return 'cancelled'
  }
}

export const EVENT_LABELS: Record<string, string> = {
  created: 'Created',
  started: 'Picked up',
  failed: 'Failed',
  retried: 'Retried',
  succeeded: 'Succeeded',
  dead: 'Moved to dead',
  cancelled: 'Cancelled',
  lease_expired: 'Lease ran out',
  throttled: 'Held by rate limit',
}

const SCHEDULED = /^queued by schedule:(\d+):(\d+)$/

export function eventMessage(event: JobEvent): string | undefined {
  const scheduled = event.kind === 'created' ? SCHEDULED.exec(event.message ?? '') : null
  if (scheduled) return `Queued by schedule ${scheduled[1]} for its ${formatClock(Number(scheduled[2]) * 1000)} run`
  return event.message
}

export interface Attempt {
  number: number
  worker: string
  startedAt: string
  endedAt: string | null
  outcome: 'running' | 'succeeded' | 'failed' | 'lease expired'
  error: string | null
}

// The worker writes "attempt N on <worker id>" when it picks a job up.
const STARTED = /^attempt (\d+) on (\S+)$/

export function attemptsFromTimeline(timeline: JobEvent[]): Attempt[] {
  const attempts: Attempt[] = []
  let open: Attempt | null = null
  const close = (outcome: Attempt['outcome'], at: string, error: string | null = null) => {
    if (!open) return
    Object.assign(open, { outcome, endedAt: at, error })
    open = null
  }

  for (const event of timeline) {
    switch (event.kind) {
      case 'started': {
        const match = STARTED.exec(event.message ?? '')
        open = {
          number: match ? Number(match[1]) : attempts.length + 1,
          worker: match?.[2] ?? 'unknown',
          startedAt: event.at,
          endedAt: null,
          outcome: 'running',
          error: null,
        }
        attempts.push(open)
        break
      }
      case 'succeeded':
        close('succeeded', event.at)
        break
      case 'failed':
      case 'dead':
        close('failed', event.at, event.message || null)
        break
      case 'lease_expired':
        close('lease expired', event.at, event.message || null)
        break
      // The type's rate limit sent the job straight back and gave the attempt back too.
      case 'throttled':
        if (open) attempts.pop()
        open = null
        break
    }
  }
  return attempts
}
