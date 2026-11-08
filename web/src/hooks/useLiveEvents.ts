import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { queryKeys } from '../api/keys'
import { LIVE_EVENT_NAMES, type JobUpdate, type LiveEvents, type StatsTick } from '../api/live'
import type { JobDetail, JobPage, Stats, TeamsResponse, TeamUsage, WorkersResponse } from '../api/types'

export type LiveStatus = 'connecting' | 'live' | 'offline'

const SAMPLE_WINDOW = 150
const DECISION_LIMIT = 50

// Keeps the query cache in step with the /api/events stream, so pages update without polling.
export function useLiveEvents(): LiveStatus {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<LiveStatus>('connecting')

  useEffect(() => {
    const source = new EventSource('/api/events')
    const refetchSoon = throttledInvalidate(queryClient)
    const handlers: { [K in keyof LiveEvents]: (data: LiveEvents[K]) => void } = {
      job: (update) => applyJobUpdate(queryClient, update, refetchSoon),
      stats: (tick) => applyStatsTick(queryClient, tick, refetchSoon),
      workers: (workers) =>
        queryClient.setQueryData<WorkersResponse>(queryKeys.workers, (prev) => prev && { ...prev, workers }),
      decision: (decision) =>
        queryClient.setQueryData<WorkersResponse>(
          queryKeys.workers,
          (prev) => prev && { ...prev, decisions: [decision, ...prev.decisions].slice(0, DECISION_LIMIT) },
        ),
      team_usage: (usage) => applyTeamUsage(queryClient, usage),
    }

    for (const name of LIVE_EVENT_NAMES) {
      source.addEventListener(name, (event) => {
        const handle = handlers[name] as (data: unknown) => void
        handle(JSON.parse((event as MessageEvent<string>).data))
      })
    }
    source.onopen = () => setStatus('live')
    // EventSource retries on its own; CLOSED means it gave up, e.g. the endpoint isn't there.
    source.onerror = () => setStatus(source.readyState === EventSource.CLOSED ? 'offline' : 'connecting')

    return () => {
      source.close()
      refetchSoon.cancel()
    }
  }, [queryClient])

  return status
}

type RefetchSoon = ((key: readonly unknown[]) => void) & { cancel: () => void }

// Many events can arrive in a burst; refetch each affected query at most once a second.
function throttledInvalidate(queryClient: QueryClient): RefetchSoon {
  const pending = new Map<string, number>()
  const refetch = (key: readonly unknown[]) => {
    const id = JSON.stringify(key)
    if (pending.has(id)) return
    pending.set(
      id,
      window.setTimeout(() => {
        pending.delete(id)
        void queryClient.invalidateQueries({ queryKey: key })
      }, 1000),
    )
  }
  refetch.cancel = () => {
    pending.forEach((timer) => window.clearTimeout(timer))
    pending.clear()
  }
  return refetch
}

function applyJobUpdate(queryClient: QueryClient, update: JobUpdate, refetchSoon: RefetchSoon) {
  let seen = false
  queryClient.setQueriesData<InfiniteData<JobPage>>({ queryKey: [...queryKeys.jobs, 'list'] }, (data) => {
    if (!data) return data
    const pages = data.pages.map((page) => {
      const index = page.jobs.findIndex((job) => job.id === update.id)
      if (index === -1) return page
      seen = true
      const jobs = [...page.jobs]
      jobs[index] = { ...jobs[index], ...update }
      return { ...page, jobs }
    })
    return { ...data, pages }
  })
  if (!seen && update.status === 'queued' && update.attempts === 0) {
    refetchSoon([...queryKeys.jobs, 'list'])
  }

  const detailKey = queryKeys.job(update.id)
  const detail = queryClient.getQueryData<JobDetail>(detailKey)
  if (detail) {
    queryClient.setQueryData<JobDetail>(detailKey, { ...detail, ...update })
    // Progress can be patched in place, but a new status or attempt adds timeline events.
    if (detail.status !== update.status || detail.attempts !== update.attempts) refetchSoon(detailKey)
  }

  if (update.status === 'dead') refetchSoon(queryKeys.deadGroups)
}

function applyStatsTick(queryClient: QueryClient, tick: StatsTick, refetchSoon: RefetchSoon) {
  const { sample, ...snapshot } = tick
  const prev = queryClient.getQueryData<Stats>(queryKeys.stats)
  if (!prev) return
  queryClient.setQueryData<Stats>(queryKeys.stats, {
    ...snapshot,
    samples: [...prev.samples, sample].slice(-SAMPLE_WINDOW),
  })
  if (prev.dead_groups !== snapshot.dead_groups) refetchSoon(queryKeys.deadGroups)
}

function applyTeamUsage(queryClient: QueryClient, usage: TeamUsage[]) {
  const byTeam = new Map(usage.map((u) => [u.team_id, u]))
  queryClient.setQueryData<TeamsResponse>(queryKeys.teams, (prev) => {
    if (!prev) return prev
    const teams = prev.teams.map((team) => {
      const u = byTeam.get(team.id)
      if (!u) return team
      return {
        ...team,
        jobs_this_minute: u.jobs_this_minute,
        rejected_this_minute: u.rejected_this_minute,
        fair_share: u.fair_share,
        workers_in_use: u.workers_in_use,
      }
    })
    return { ...prev, teams }
  })
}
