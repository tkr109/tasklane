export type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'dead' | 'cancelled'

export const JOB_STATUSES: JobStatus[] = ['queued', 'running', 'succeeded', 'failed', 'dead', 'cancelled']

export interface Job {
  id: number
  team_id: number
  type: string
  payload: unknown
  priority: number
  status: JobStatus
  attempts: number
  max_attempts: number
  run_at: string
  lease_until?: string
  locked_by?: string
  idempotency_key?: string
  progress: number
  last_error?: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
  // Only on a succeeded job whose handler returned something.
  result?: unknown
}

export interface JobEvent {
  id: number
  job_id: number
  kind: string
  message?: string
  at: string
}

export interface JobDetail extends Job {
  timeline: JobEvent[]
}

export interface JobPage {
  jobs: Job[]
  next_cursor: string
}

export interface JobFilters {
  status?: JobStatus
  type?: string
}

export interface JobSpec {
  type: string
  payload?: unknown
  priority?: number
  max_attempts?: number
}

// A repeating job, owned by the team of the API key.
export interface Schedule {
  id: number
  team_id: number
  cron: string
  timezone: string
  type: string
  payload: unknown
  priority: number
  max_attempts: number
  enabled: boolean
  next_run_at: string
  last_run_at?: string
  created_at: string
  updated_at: string
}

export interface ScheduleSpec {
  cron: string
  timezone?: string
  type: string
  payload?: unknown
  priority?: number
  max_attempts?: number
  enabled?: boolean
}

export interface Sample {
  at: string
  queued: number
  workers: number
  succeeded: number
  failed: number
}

export interface JobTypeStats {
  type: string
  timeout_seconds: number
  outgoing_per_second: number | null
  rate_per_second: number
  held: number
}

export interface ScalerStatus {
  current: number
  target: number
  min: number
  max: number
  jobs_per_worker: number
  interval_seconds: number
  cooldown_seconds: number
  step_down_seconds: number
  scale_down_at: string | null
}

export interface Stats {
  counts: {
    queued: number
    running: number
    succeeded_today: number
    failed: number
    dead: number
  }
  oldest_wait_seconds: number
  runnable: number
  queue_ceiling: number
  dead_groups: number
  workers: { running: number; starting: number; draining: number }
  // Null while no scaler is running, or once one has missed three rounds.
  scaler: ScalerStatus | null
  types: JobTypeStats[]
  // One sample per second, oldest first.
  samples: Sample[]
}

export type WorkerState = 'starting' | 'running' | 'draining'

export interface RunningJob {
  id: number
  type: string
  progress: number
}

export interface Worker {
  id: string
  host: string
  state: WorkerState
  started_at: string
  last_seen_at: string
  jobs_done: number
  // A worker runs several jobs at once.
  running_jobs: RunningJob[]
}

export interface ScalerDecision {
  id: number
  at: string
  action: 'up' | 'down' | 'hold'
  from: number
  to: number
  reason: string
}

export interface WorkersResponse {
  workers: Worker[]
  decisions: ScalerDecision[]
}

export type PlanName = 'free' | 'pro' | 'enterprise'

export interface Plan {
  id: number
  name: PlanName
  share_weight: number
  max_workers: number
  jobs_per_minute: number
  burst: number
}

export interface Team {
  id: number
  name: string
  plan: Plan
  created_at: string
}

export interface TeamSummary extends Team {
  usage: { queued: number; running: number; jobs_last_minute: number }
}

// The full key is only in the response that creates it.
export interface ApiKey {
  id: number
  team_id: number
  prefix: string
  created_at: string
  revoked_at?: string
}

export interface NewApiKey extends ApiKey {
  key: string
}

// Refusals and fair share only come with team_usage events.
export interface TeamRate {
  team_id: number
  jobs_last_minute: number
  rejected_last_minute: number
  fair_share: number
}

export interface DeadGroup {
  // Opaque and URL safe.
  id: string
  type: string
  // The error with ids, numbers and quoted values taken out: what the group's jobs share.
  pattern: string
  // The newest error in the group, as it was.
  error: string
  count: number
  teams: string[]
  first_at: string
  last_at: string
  // Jobs that died in each of the last 24 hours, oldest first.
  hourly: number[]
  sample_ids: number[]
}

export interface DeadGroupsResponse {
  groups: DeadGroup[]
  total: number
}
