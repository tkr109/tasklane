import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router'
import { JOB_STATUSES, type JobStatus } from '../../api/types'
import type { PriorityBand } from '../../lib/jobs'

export interface JobFilterState {
  search: string
  status: JobStatus | ''
  type: string
  priority: PriorityBand | ''
}

const KEYS: (keyof JobFilterState)[] = ['search', 'status', 'type', 'priority']

const asStatus = (v: string | null): JobStatus | '' => (JOB_STATUSES.includes(v as JobStatus) ? (v as JobStatus) : '')

const asPriority = (v: string | null): PriorityBand | '' =>
  v === 'high' || v === 'normal' || v === 'low' ? v : ''

// Filters live in the URL, so a filtered view can be linked to and survives a reload.
export function useJobFilters() {
  const [params, setParams] = useSearchParams()

  const filters = useMemo<JobFilterState>(
    () => ({
      search: params.get('search') ?? '',
      status: asStatus(params.get('status')),
      type: params.get('type') ?? '',
      priority: asPriority(params.get('priority')),
    }),
    [params],
  )

  const update = useCallback(
    (patch: Partial<JobFilterState>) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          for (const key of KEYS) {
            const value = patch[key]
            if (value === undefined) continue
            if (value) next.set(key, value)
            else next.delete(key)
          }
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  const clear = useCallback(() => setParams({}, { replace: true }), [setParams])

  return { filters, update, clear }
}
