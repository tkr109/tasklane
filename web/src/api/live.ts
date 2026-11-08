import type { Job, ScalerDecision, Stats, TeamRate, TeamSummary, Worker } from './types'

// Unlike the REST API, the stream sends null for an empty field, cuts last_error to
// 200 characters and writes UTC times as +00:00.
export type JobUpdate = Pick<Job, 'id' | 'team_id' | 'type' | 'status' | 'progress' | 'attempts' | 'run_at' | 'updated_at'> & {
  locked_by: string | null
  last_error: string | null
}

// Sent with each heartbeat and state change; running jobs come from job events.
export type WorkerUpdate = Omit<Worker, 'state' | 'running_jobs'> & { state: Worker['state'] | 'stopped' }

// Sent for at most 20 teams at a time.
export type TeamUsageUpdate = TeamRate & TeamSummary['usage']

export type StatsTick = Omit<Stats, 'samples'> & { sample: Stats['samples'][number] }

// Named events on the /api/events stream and the data each one carries.
export interface LiveEvents {
  job: JobUpdate
  stats: StatsTick
  worker: WorkerUpdate
  decision: ScalerDecision
  team_usage: TeamUsageUpdate[]
  // Tells the browser it may have missed events and should refetch everything.
  reset: Record<string, never>
}

export const LIVE_EVENT_NAMES: (keyof LiveEvents)[] = ['job', 'worker', 'stats', 'decision', 'team_usage', 'reset']
