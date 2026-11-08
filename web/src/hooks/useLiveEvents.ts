import { useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { adminToken, ApiError } from '../api/client'
import { queryKeys } from '../api/keys'
import {
  LIVE_EVENT_NAMES,
  type JobUpdate,
  type LiveEvents,
  type StatsTick,
  type TeamUsageUpdate,
  type WorkerUpdate,
} from '../api/live'
import type { JobDetail, JobPage, Stats, TeamRate, TeamSummary, WorkersResponse } from '../api/types'

export type LiveStatus = 'connecting' | 'live' | 'offline'

const SAMPLE_WINDOW = 150
const DECISION_LIMIT = 50
const POLL_MS = 5000

let streamDown = false

// The default refetchInterval: poll only while the stream is unavailable, and
// leave alone endpoints the API doesn't have yet.
export function pollWhileStreamDown(query: { state: { error: unknown } }) {
  const { error } = query.state
  if (!streamDown || (error instanceof ApiError && error.status === 404)) return false
  return POLL_MS
}

// Keeps the query cache in step with the /api/events stream, so pages update without polling.
export function useLiveEvents(): LiveStatus {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<LiveStatus>('connecting')

  useEffect(() => {
    // EventSource can't send an Authorization header, so the token rides in the query string.
    const source = new EventSource(`/api/events?token=${encodeURIComponent(adminToken)}`)
    const refetchSoon = throttledInvalidate(queryClient)
    const handlers: { [K in keyof LiveEvents]: (data: LiveEvents[K]) => void } = {
      job: (update) => applyJobUpdate(queryClient, update, refetchSoon),
      stats: (tick) => applyStatsTick(queryClient, tick, refetchSoon),
      worker: (update) => applyWorkerUpdate(queryClient, update),
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
    source.onopen = () => {
      streamDown = false
      setStatus('live')
    }
    // EventSource retries on its own; CLOSED means it gave up, e.g. the endpoint isn't there.
    source.onerror = () => {
      if (source.readyState !== EventSource.CLOSED) {
        setStatus('connecting')
        return
      }
      streamDown = true
      setStatus('offline')
      // Refetching makes every query re-read its interval and start polling.
      void queryClient.invalidateQueries()
    }

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
  applyRunningJob(queryClient, update, refetchSoon)
}

// Workers list their running jobs only on a fetch, so job events keep them current in between.
function applyRunningJob(queryClient: QueryClient, update: JobUpdate, refetchSoon: RefetchSoon) {
  const data = queryClient.getQueryData<WorkersResponse>(queryKeys.workers)
  if (!data) return
  const owner = update.status === 'running' ? update.locked_by : null
  const listed = data.workers.some((w) => w.running_jobs.some((job) => job.id === update.id))
  if (!owner && !listed) return
  if (owner && !data.workers.some((w) => w.id === owner)) {
    refetchSoon(queryKeys.workers)
    return
  }

  const running = { id: update.id, type: update.type, progress: update.progress }
  const workers = data.workers.map((worker) => {
    const others = worker.running_jobs.filter((job) => job.id !== update.id)
    if (worker.id === owner) return { ...worker, running_jobs: [...others, running] }
    return others.length === worker.running_jobs.length ? worker : { ...worker, running_jobs: others }
  })
  queryClient.setQueryData<WorkersResponse>(queryKeys.workers, { ...data, workers })
}

function applyWorkerUpdate(queryClient: QueryClient, { state, ...update }: WorkerUpdate) {
  queryClient.setQueryData<WorkersResponse>(queryKeys.workers, (prev) => {
    if (!prev) return prev
    const rest = prev.workers.filter((w) => w.id !== update.id)
    if (state === 'stopped') return { ...prev, workers: rest }
    const known = prev.workers.find((w) => w.id === update.id)
    return { ...prev, workers: [...rest, { running_jobs: [], ...known, ...update, state }] }
  })
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

function applyTeamUsage(queryClient: QueryClient, updates: TeamUsageUpdate[]) {
  const byTeam = new Map(updates.map((u) => [u.team_id, u]))
  queryClient.setQueryData<{ teams: TeamSummary[] }>(queryKeys.teams, (prev) => {
    if (!prev) return prev
    const teams = prev.teams.map((team) => {
      const u = byTeam.get(team.id)
      return u ? { ...team, usage: { queued: u.queued, running: u.running } } : team
    })
    return { teams }
  })
  const rates: Record<number, TeamRate> = {}
  for (const u of updates) {
    rates[u.team_id] = {
      team_id: u.team_id,
      jobs_this_minute: u.jobs_this_minute,
      rejected_this_minute: u.rejected_this_minute,
      fair_share: u.fair_share,
    }
  }
  queryClient.setQueryData(queryKeys.teamRates, rates)
}
