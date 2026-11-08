import { useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { ApiError, describeError } from '../api/client'
import { cancelJob, retryJob } from '../api/jobs'
import { queryKeys } from '../api/keys'
import type { Job } from '../api/types'
import { canCancel, canRetry } from '../lib/jobs'
import { formatNumber, plural } from '../lib/format'
import { useToast } from './useToast'

type Target = Pick<Job, 'id' | 'status'>

interface Outcome {
  done: number
  skipped: number
  error: unknown
}

// The API has no bulk endpoints, so each job gets its own request. A 409
// means the job moved on since the list was loaded, which counts as skipped.
async function runEach(jobs: Target[], eligible: (job: Target) => boolean, action: (id: number) => Promise<Job>) {
  const targets = jobs.filter(eligible)
  const results = await Promise.allSettled(targets.map((job) => action(job.id)))
  const outcome: Outcome = { done: 0, skipped: jobs.length - targets.length, error: null }
  for (const result of results) {
    if (result.status === 'fulfilled') outcome.done++
    else if (result.reason instanceof ApiError && result.reason.status === 409) outcome.skipped++
    else outcome.error ??= result.reason
  }
  return outcome
}

export function useJobActions() {
  const queryClient = useQueryClient()
  const toast = useToast()

  const report = useCallback(
    ({ done, skipped, error }: Outcome, doneText: string, skipText: string) => {
      let message = `${plural(done, 'job')} ${doneText}.`
      if (skipped) message += ` ${formatNumber(skipped)} skipped: ${skipText}.`
      if (error) message += ` ${describeError(error)}`
      toast(message)
      void queryClient.invalidateQueries({ queryKey: queryKeys.jobs })
    },
    [queryClient, toast],
  )

  const retry = useCallback(
    async (jobs: Target[]) => {
      const outcome = await runEach(jobs, (job) => canRetry(job.status), retryJob)
      report(outcome, 'sent back to the queue', 'only failed, dead or cancelled jobs can be retried')
    },
    [report],
  )

  const cancel = useCallback(
    async (jobs: Target[]) => {
      const outcome = await runEach(jobs, (job) => canCancel(job.status), cancelJob)
      report(outcome, 'cancelled', 'only queued or retrying jobs can be cancelled')
    },
    [report],
  )

  return { retry, cancel }
}
