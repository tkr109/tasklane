import type { Job, ScalerDecision, Stats, TeamRate, TeamSummary, Worker } from './types'

export type JobUpdate = Pick<
  Job,
  'id' | 'team_id' | 'type' | 'status' | 'progress' | 'attempts' | 'run_at' | 'locked_by' | 'last_error' | 'updated_at'
>

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
}

export const LIVE_EVENT_NAMES: (keyof LiveEvents)[] = ['job', 'worker', 'stats', 'decision', 'team_usage']
