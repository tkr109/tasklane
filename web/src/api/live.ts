import type { Job, ScalerDecision, Stats, TeamRate, TeamSummary, Worker } from './types'

export type JobUpdate = Pick<
  Job,
  'id' | 'team_id' | 'type' | 'status' | 'progress' | 'attempts' | 'run_at' | 'locked_by' | 'last_error' | 'updated_at'
>

export type TeamUsageUpdate = TeamRate & TeamSummary['usage']

export type StatsTick = Omit<Stats, 'samples'> & { sample: Stats['samples'][number] }

// Named events on the /api/events stream and the data each one carries.
export interface LiveEvents {
  job: JobUpdate
  stats: StatsTick
  workers: Worker[]
  decision: ScalerDecision
  team_usage: TeamUsageUpdate[]
}

export const LIVE_EVENT_NAMES: (keyof LiveEvents)[] = ['job', 'stats', 'workers', 'decision', 'team_usage']
