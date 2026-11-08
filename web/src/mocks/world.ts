import type { JobUpdate, LiveEvents, WorkerUpdate } from '../api/live'
import type {
  ApiKey,
  DeadGroup,
  Job,
  JobDetail,
  JobEvent,
  JobFilters,
  JobPage,
  JobSpec,
  JobStatus,
  NewApiKey,
  PlanName,
  ScalerDecision,
  Stats,
  Team,
  TeamSummary,
  Worker,
  WorkerState,
} from '../api/types'
import { capitalize, formatNumber } from '../lib/format'
import { clamp, hex, pick, rand, randInt } from './random'
import {
  BACKGROUND_ARRIVALS_PER_SECOND,
  DEAD_GROUPS,
  errorPattern,
  ADMIN_TOKEN,
  DASHBOARD_TEAM,
  JOB_TYPES,
  JOBS_PER_WORKER_SECOND,
  PLANS,
  QUEUE_CEILING,
  SCALER,
  TEAMS,
  TYPE_NAMES,
  WORKER_CONCURRENCY,
  payloadFor,
  planByName,
  resultFor,
  type TeamSeed,
} from './seed'

export class MockError extends Error {
  readonly status: number
  readonly retryAfter: number | null

  constructor(status: number, message: string, retryAfter: number | null = null) {
    super(message)
    this.status = status
    this.retryAfter = retryAfter
  }
}

interface MockJob extends Job {
  timeline: JobEvent[]
}

interface MockWorker {
  id: string
  host: string
  state: WorkerState
  startedAt: number
  readyAt: number
  lastSeen: number
  heartbeatEvery: number
  done: number
  drainAt: number
}

// The mock keeps each key's secret so it can check Authorization headers.
interface MockKey extends ApiKey {
  secret: string
}

interface MockTeam {
  id: number
  seed: TeamSeed
  plan: PlanName
  createdAt: string
  keys: MockKey[]
  usage: number
  demand: number
  rejected: number
  tokens: number
}

type Listener = <K extends keyof LiveEvents>(event: K, data: LiveEvents[K]) => void

const TICK_MS = 500
const SAMPLE_WINDOW = 150
const MAX_TRACKED_JOBS = 600
const LEASE_MS = 30_000
const TEAM_USAGE_CHUNK = 20
const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE

const iso = (ms: number) => new Date(ms).toISOString()

// A dead group's id is its type and pattern in base64url, as the API makes it.
const groupId = (type: string, pattern: string) =>
  btoa(String.fromCharCode(...new TextEncoder().encode(`${type}\n${pattern}`)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

function parseGroupId(id: string): boolean {
  try {
    return atob(id.replace(/-/g, '+').replace(/_/g, '/')).includes('\n')
  } catch {
    return false
  }
}

// Times in the event stream come from Postgres, which writes UTC as +00:00.
const pgTime = (at: string) => at.replace(/Z$/, '+00:00')

const bearer = (authorization: string | null) => authorization?.match(/^Bearer (.+)$/)?.[1].trim() ?? ''

const newSecret = () => `tk_live_${hex(32)}`

const displayPrefix = (secret: string) => secret.slice(0, 'tk_live_'.length + 6)

const publicKey = ({ secret, ...key }: MockKey): ApiKey => key

const backoffSeconds = (attempt: number) => Math.min(300, 2 ** attempt) * rand(0.8, 1.25)

// A small simulation of a busy Tasklane install. It keeps whole-system numbers
// (queue depth, throughput) as plain counters, and tracks a few hundred real
// job records so tables, details and timelines have something true to show.
export class World {
  private listeners = new Set<Listener>()
  private jobs = new Map<number, MockJob>()
  private workers: MockWorker[] = []
  private teams: MockTeam[]
  private decisions: ScalerDecision[] = []
  private deadGroups: DeadGroup[]
  private samples: Stats['samples'] = []

  private queued = 46
  private running = 6
  private succeededToday = 1_284_220
  private retrying = 31
  private dead = 0
  private rateHeld = 40
  private oldestWait = 0.4
  private okThisSecond = 0
  private failedThisSecond = 0
  private target = SCALER.min
  private belowTargetSince: number | null = null
  private lastStepDown = 0
  private nextHost = 1
  private nextJobId = 482_113
  private nextEventId = 1
  private nextDecisionId = 1
  private nextKeyId = 1
  private ticks = 0
  private submitted: { at: number; count: number }[] = []
  private rejected: { at: number; count: number }[] = []

  constructor() {
    const now = Date.now()
    this.teams = TEAMS.map((seed, i) => ({
      id: i + 1,
      seed,
      plan: seed.plan,
      createdAt: iso(now - seed.daysOld * 24 * HOUR),
      keys: seed.keys.map((k) => this.newKey(i + 1, k.secret ?? newSecret(), now - k.daysAgo * 24 * HOUR)),
      usage: Math.min(seed.demandPerMinute, planByName(seed.plan).jobs_per_minute),
      demand: seed.demandPerMinute,
      rejected: 0,
      tokens: planByName(seed.plan).burst,
    }))
    this.deadGroups = DEAD_GROUPS.map((g) => ({
      id: groupId(g.type, errorPattern(g.error)),
      type: g.type,
      pattern: errorPattern(g.error),
      error: g.error,
      count: g.count,
      teams: g.teams,
      first_at: iso(now - g.firstHoursAgo * HOUR),
      last_at: iso(now - g.lastMinutesAgo * MINUTE),
      hourly: Array.from({ length: 24 }, (_, h) =>
        Math.round(Math.max(0, (g.spiking && h > 18 ? 9 : 1.5) + Math.sin(h / 2 + g.count) * 1.2 + rand(0, 2)) * (g.count / 400)),
      ),
      sample_ids: [],
    }))
    this.dead = this.deadGroups.reduce((sum, g) => sum + g.count, 0)
    this.seedWorkers(now)
    this.seedHistory(now)
    this.seedJobs()
    this.seedDeadSamples()
    window.setInterval(() => this.tick(), TICK_MS)
  }

  subscribe(listener: Listener) {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private emit<K extends keyof LiveEvents>(event: K, data: LiveEvents[K]) {
    this.listeners.forEach((listener) => listener(event, data))
  }

  // Same answers as the API's auth middleware.
  teamForKey(authorization: string | null): number {
    const secret = bearer(authorization)
    for (const team of this.teams) {
      if (team.keys.some((k) => k.secret === secret && !k.revoked_at)) return team.id
    }
    throw new MockError(401, 'missing or invalid API key')
  }

  checkAdmin(authorization: string | null) {
    if (bearer(authorization) !== ADMIN_TOKEN) throw new MockError(401, 'missing or invalid admin token')
  }

  listJobs(teamId: number, filters: JobFilters, cursor: number | null, limit: number): JobPage {
    const list = [...this.jobs.values()]
      .filter(
        (job) =>
          job.team_id === teamId &&
          (!filters.status || job.status === filters.status) &&
          (!filters.type || job.type === filters.type) &&
          (cursor === null || job.id < cursor),
      )
      .sort((a, b) => b.id - a.id)
      .slice(0, limit)
      .map((job) => this.toJob(job))
    const next = list.length === limit ? String(list[list.length - 1].id) : ''
    return { jobs: list, next_cursor: next }
  }

  getJob(teamId: number, id: number): JobDetail {
    const job = this.ownJob(teamId, id)
    return { ...this.toJob(job), timeline: [...job.timeline] }
  }

  submitBatch(teamId: number, specs: JobSpec[]): Job[] {
    if (specs.length === 0 || specs.length > 500) throw new MockError(400, 'invalid request: a batch holds 1 to 500 jobs')
    specs.forEach((spec, i) => {
      if (!JOB_TYPES[spec.type]) {
        throw new MockError(400, `job ${i}: invalid request: unknown job type "${spec.type}", expected one of [${TYPE_NAMES.join(' ')}]`)
      }
    })
    if (this.queued > QUEUE_CEILING) {
      throw new MockError(503, `more than ${formatNumber(QUEUE_CEILING)} jobs are queued, try again once it drains`, 30)
    }

    const team = this.teamById(teamId)
    const plan = planByName(team.plan)
    const now = Date.now()
    if (team.tokens < specs.length) {
      this.rejected.push({ at: now, count: specs.length })
      const wait = Math.ceil((specs.length - team.tokens) / (plan.jobs_per_minute / 60))
      throw new MockError(
        429,
        `rate limit reached: ${team.seed.name} is on ${capitalize(team.plan)} (${formatNumber(plan.jobs_per_minute)} jobs per minute, burst ${formatNumber(plan.burst)})`,
        wait,
      )
    }
    team.tokens -= specs.length
    this.submitted.push({ at: now, count: specs.length })
    this.queued += specs.length

    // Every job gets an id, but only a handful are tracked in full to keep the mock light.
    return specs.map((spec, i) => {
      const job = this.createJob(team, spec.type, now, {
        priority: spec.priority,
        maxAttempts: spec.max_attempts,
        payload: spec.payload,
        track: i < 6,
      })
      return this.toJob(job)
    })
  }

  cancelJob(teamId: number, id: number): Job {
    const job = this.ownJob(teamId, id)
    if (job.status !== 'queued' && job.status !== 'failed') {
      throw new MockError(409, `conflict: can't cancel a ${job.status} job`)
    }
    const now = Date.now()
    Object.assign(job, { status: 'cancelled', finished_at: iso(now), updated_at: iso(now) })
    this.addEvent(job, 'cancelled', now)
    this.emitJob(job)
    return this.toJob(job)
  }

  retryJob(teamId: number, id: number): Job {
    const job = this.ownJob(teamId, id)
    if (job.status !== 'dead' && job.status !== 'failed' && job.status !== 'cancelled') {
      throw new MockError(409, `conflict: can't retry a ${job.status} job`)
    }
    const now = Date.now()
    if (job.status === 'dead') this.dead = Math.max(0, this.dead - 1)
    Object.assign(job, {
      status: 'queued',
      attempts: 0,
      progress: 0,
      run_at: iso(now),
      finished_at: undefined,
      updated_at: iso(now),
    })
    this.addEvent(job, 'retried', now)
    this.emitJob(job)
    return this.toJob(job)
  }

  stats(): Stats {
    return { ...this.snapshot(), samples: [...this.samples] }
  }

  workersResponse() {
    return { workers: this.workerList(), decisions: this.decisions.slice(0, 50) }
  }

  plansResponse() {
    return { plans: PLANS }
  }

  teamsResponse(): { teams: TeamSummary[] } {
    return {
      teams: this.teams.map((t) => ({
        ...this.toTeam(t),
        usage: { ...this.queuedAndRunning(t), jobs_last_minute: Math.round(t.usage) },
      })),
    }
  }

  switchPlan(teamId: number, plan: PlanName): Team {
    const team = this.teamById(teamId)
    if (!PLANS.some((p) => p.name === plan)) throw new MockError(400, `invalid request: unknown plan "${plan}"`)
    team.plan = plan
    team.tokens = Math.min(team.tokens, planByName(plan).burst)
    return this.toTeam(team)
  }

  listKeys(teamId: number): { keys: ApiKey[] } {
    return { keys: this.teamById(teamId).keys.map(publicKey) }
  }

  createKey(teamId: number): NewApiKey {
    const team = this.teamById(teamId)
    const key = this.newKey(team.id, newSecret(), Date.now())
    team.keys.push(key)
    return { ...publicKey(key), key: key.secret }
  }

  revokeKey(teamId: number, keyId: number): ApiKey {
    const key = this.teamById(teamId).keys.find((k) => k.id === keyId)
    if (!key) throw new MockError(404, 'API key not found')
    key.revoked_at ??= iso(Date.now())
    return publicKey(key)
  }

  deadGroupsResponse() {
    const groups = this.deadGroups
    return { groups, total: groups.reduce((sum, g) => sum + g.count, 0) }
  }

  deadGroupJobs(groupId: string, limit: number): Job[] {
    const group = this.deadGroups.find((g) => g.id === groupId)
    if (!group && !parseGroupId(groupId)) throw new MockError(404, 'dead job group not found')
    return (group?.sample_ids ?? [])
      .map((id) => this.jobs.get(id))
      .filter((job): job is MockJob => job?.status === 'dead')
      .slice(0, limit)
      .map((job) => this.toJob(job))
  }

  retryDeadGroup(groupId: string) {
    const group = this.deadGroups.find((g) => g.id === groupId)
    // Like the API, an id that names a group with nothing left in it retries nothing.
    if (!group) {
      if (!parseGroupId(groupId)) throw new MockError(404, 'dead job group not found')
      return { retried: 0 }
    }
    this.deadGroups = this.deadGroups.filter((g) => g !== group)
    this.dead = Math.max(0, this.dead - group.count)
    this.queued += group.count
    const now = Date.now()
    for (const id of group.sample_ids) {
      const job = this.jobs.get(id)
      if (job?.status !== 'dead') continue
      Object.assign(job, {
        status: 'queued',
        attempts: 0,
        progress: 0,
        run_at: iso(now),
        finished_at: undefined,
        updated_at: iso(now),
      })
      this.addEvent(job, 'retried', now, 'retried with its dead job group')
      this.emitJob(job)
    }
    return { retried: group.count }
  }

  private tick() {
    const now = Date.now()
    const dt = TICK_MS / SECOND
    this.ticks++
    this.stepWorkers(now)
    this.stepQueue(dt, now)
    this.stepTeams(dt, now)
    this.stepJobs(dt, now)

    if (this.ticks % 2 === 0) {
      const sample = {
        at: iso(now),
        queued: Math.round(this.queued),
        workers: this.countWorkers('running'),
        succeeded: Math.round(this.okThisSecond),
        failed: Math.round(this.failedThisSecond * 10) / 10,
      }
      this.okThisSecond = 0
      this.failedThisSecond = 0
      this.samples = [...this.samples, sample].slice(-SAMPLE_WINDOW)
      this.emit('stats', { ...this.snapshot(), sample })
      const usage = this.teams.map((t) => ({ team_id: t.id, ...this.queuedAndRunning(t), ...this.rateOf(t) }))
      for (let i = 0; i < usage.length; i += TEAM_USAGE_CHUNK) {
        this.emit('team_usage', usage.slice(i, i + TEAM_USAGE_CHUNK))
      }
    }
    if (this.ticks % ((SCALER.intervalSeconds * SECOND) / TICK_MS) === 0) this.scale(now)
  }

  private stepWorkers(now: number) {
    for (const w of this.workers) {
      if (w.state === 'starting' && now >= w.readyAt) {
        Object.assign(w, { state: 'running', startedAt: now, lastSeen: now })
        this.emitWorker(w)
      }
      if (w.state !== 'starting' && (now - w.lastSeen) / SECOND >= w.heartbeatEvery) {
        w.lastSeen = now
        w.heartbeatEvery = rand(2.6, 3.3)
        this.emitWorker(w)
      }
    }

    const gone = new Set(this.workers.filter((w) => w.state === 'draining' && now >= w.drainAt).map((w) => w.id))
    if (gone.size === 0) return
    for (const job of this.jobs.values()) {
      if (job.status !== 'running' || !gone.has(job.locked_by ?? '')) continue
      const worker = job.locked_by
      Object.assign(job, {
        status: 'queued',
        locked_by: undefined,
        lease_until: undefined,
        progress: 0,
        run_at: iso(now),
        last_error: 'lease expired',
        updated_at: iso(now),
      })
      this.addEvent(job, 'lease_expired', now, `${worker} stopped renewing the lease, job is queued`)
      this.emitJob(job)
    }
    for (const w of this.workers) {
      if (gone.has(w.id)) this.emitWorker(w, 'stopped')
    }
    this.workers = this.workers.filter((w) => !gone.has(w.id))
  }

  private stepQueue(dt: number, now: number) {
    this.queued += BACKGROUND_ARRIVALS_PER_SECOND * rand(0.6, 1.4) * dt
    const up = this.countWorkers('running')
    const draining = this.countWorkers('draining')
    const capacity = (up + draining * 0.4) * JOBS_PER_WORKER_SECOND * dt * rand(0.9, 1.08)
    const runnable = Math.max(0, this.queued - this.rateHeld)
    const processed = Math.min(runnable + Math.min(this.rateHeld, 10 * dt), capacity)
    this.queued -= processed

    const failed = processed * rand(0.006, 0.02)
    this.succeededToday += processed - failed
    this.okThisSecond += processed - failed
    this.failedThisSecond += failed
    this.retrying += failed
    const backToQueue = this.retrying * 0.18 * dt
    this.retrying -= backToQueue
    this.queued += backToQueue * 0.995
    this.dead += backToQueue * 0.005

    const heldTarget = this.queued > 400 ? 120 + rand(0, 60) : 30 + rand(0, 30)
    this.rateHeld = Math.min(this.rateHeld + (heldTarget - this.rateHeld) * 0.2, this.queued)
    const slots = up * WORKER_CONCURRENCY * 2
    this.running = Math.round(
      Math.min(slots, this.queued > slots ? slots : (processed / dt) * 0.16 + rand(1, 4)),
    )
    if (runnable > Math.max(1, up) * JOBS_PER_WORKER_SECOND * 1.6) this.oldestWait += dt
    else this.oldestWait = Math.max(rand(0.2, 0.7), this.oldestWait * 0.55)

    const perWorker = processed / Math.max(1, up)
    this.workers.forEach((w) => {
      if (w.state === 'running') w.done += perWorker
    })
    this.submitted = this.submitted.filter((s) => now - s.at < MINUTE)
    this.rejected = this.rejected.filter((s) => now - s.at < MINUTE)
  }

  private stepTeams(dt: number, now: number) {
    for (const team of this.teams) {
      const plan = planByName(team.plan)
      team.tokens = Math.min(plan.burst, team.tokens + (plan.jobs_per_minute / 60) * dt)
      if (team.seed.name === DASHBOARD_TEAM) {
        const flooded = this.submitted.reduce((sum, s) => sum + s.count, 0)
        team.demand = team.usage = team.seed.demandPerMinute * rand(0.92, 1.08) + flooded
        team.rejected = this.rejected.reduce((sum, s) => sum + s.count, 0)
        continue
      }
      team.demand =
        team.seed.name === 'acme-billing'
          ? 860 + 130 * Math.sin(now / 9000) + rand(-30, 30)
          : team.seed.demandPerMinute * rand(0.92, 1.08)
      team.usage += (Math.min(team.demand, plan.jobs_per_minute) - team.usage) * 0.3
      team.rejected = Math.max(0, Math.round(team.demand - plan.jobs_per_minute))
    }
  }

  private stepJobs(dt: number, now: number) {
    for (const job of this.jobs.values()) {
      if (job.status !== 'running') continue
      const [min, max] = JOB_TYPES[job.type].speed
      job.progress = Math.min(100, job.progress + rand(min, max) * dt)
      job.lease_until = iso(now + LEASE_MS)
      if (job.progress >= 100) {
        const { errors } = JOB_TYPES[job.type]
        if (errors.length > 0 && Math.random() < 0.07) this.fail(job, pick(errors), now)
        else this.succeed(job, now)
      }
      this.emitJob(job)
    }

    for (const worker of this.workers) {
      if (worker.state !== 'running' || Math.random() > 0.45) continue
      if (this.runningOn(worker.id).length >= WORKER_CONCURRENCY) continue
      const job = this.nextRunnable(now)
      if (!job) break
      if (job.type === 'email.send' && Math.random() < 0.18) {
        const wait = rand(0.5, 1.6)
        // Like the API: the grab happens, then the type's limit sends the job back and returns the attempt.
        this.start(job, worker.id, now)
        Object.assign(job, {
          status: 'queued',
          attempts: job.attempts - 1,
          run_at: iso(now + wait * SECOND),
          locked_by: undefined,
          lease_until: undefined,
        })
        this.addEvent(job, 'throttled', now, `email.send is at its limit of 10/s, back in ${Math.round(wait * 1000)}ms`)
        this.emitJob(job)
        continue
      }
      this.start(job, worker.id, now)
      this.emitJob(job)
    }

    if (Math.random() < 0.32) {
      const team = this.teamByName(DASHBOARD_TEAM)
      const scheduled = Math.random() < 0.1
      this.createJob(team, scheduled ? 'email.send' : pick(team.seed.types), now, {
        track: true,
        scheduleId: scheduled ? 3 : undefined,
      })
    }
    this.pruneJobs()
  }

  private scale(now: number) {
    const active = this.workers.filter((w) => w.state !== 'draining')
    const current = active.length
    const runnable = Math.max(0, Math.round(this.queued - this.rateHeld))
    const want = clamp(Math.ceil(runnable / SCALER.jobsPerWorker), SCALER.min, SCALER.max)
    const held = Math.round(Math.min(this.rateHeld, this.queued))
    this.target = want

    if (want > current) {
      for (let i = 0; i < want - current; i++) {
        const worker = this.newWorker('starting', now, now + 1200 + i * 550 + rand(0, 300))
        this.workers.push(worker)
        this.emitWorker(worker)
      }
      const heldNote = held > 20 ? `, ${formatNumber(held)} held by email limit not counted` : ''
      this.decide('up', current, want, `${formatNumber(runnable)} runnable, oldest wait ${Math.round(this.oldestWait)}s${heldNote}`)
      this.belowTargetSince = null
    } else if (want < current) {
      if (this.belowTargetSince === null) {
        this.belowTargetSince = now
        this.decide('hold', current, current, `wants ${want}: waiting out the ${SCALER.cooldownSeconds}s cool-down before scaling down`)
      } else if (
        now - this.belowTargetSince >= SCALER.cooldownSeconds * SECOND &&
        now - this.lastStepDown >= SCALER.stepDownSeconds * SECOND
      ) {
        const victim = active.filter((w) => w.state === 'running').sort((a, b) => b.startedAt - a.startedAt)[0] ?? active.at(-1)
        if (victim) {
          Object.assign(victim, { state: 'draining', drainAt: now + 2600 })
          this.emitWorker(victim)
        }
        this.lastStepDown = now
        const below = Math.round((now - this.belowTargetSince) / SECOND)
        this.decide(
          'down',
          current,
          current - 1,
          runnable < 5 ? 'queue empty, cool-down passed' : `${formatNumber(runnable)} runnable, below target for ${below}s`,
        )
      }
    } else {
      this.belowTargetSince = null
    }
  }

  private decide(action: ScalerDecision['action'], from: number, to: number, reason: string, at = Date.now()) {
    const decision: ScalerDecision = { id: this.nextDecisionId++, at: iso(at), action, from, to, reason }
    this.decisions = [decision, ...this.decisions].slice(0, 60)
    this.emit('decision', decision)
  }

  private createJob(
    team: MockTeam,
    type: string,
    createdAt: number,
    opts: { priority?: number; maxAttempts?: number; payload?: unknown; track: boolean; scheduleId?: number },
  ): MockJob {
    const job: MockJob = {
      id: this.nextJobId++,
      team_id: team.id,
      type,
      payload: opts.payload ?? payloadFor(type, team.seed.name),
      priority: opts.priority ?? this.randomPriority(),
      status: 'queued',
      attempts: 0,
      max_attempts: opts.maxAttempts ?? 5,
      run_at: iso(createdAt),
      progress: 0,
      created_at: iso(createdAt),
      updated_at: iso(createdAt),
      timeline: [],
    }
    if (opts.scheduleId) {
      // A schedule's job is keyed by its id and the tick it fired for.
      job.idempotency_key = `schedule:${opts.scheduleId}:${Math.floor(createdAt / MINUTE) * 60}`
      this.addEvent(job, 'created', createdAt, `queued by ${job.idempotency_key}`)
    } else {
      if (Math.random() < 0.55) job.idempotency_key = `${type.split('.')[0]}-${hex(8)}`
      this.addEvent(job, 'created', createdAt)
    }
    if (opts.track) {
      this.jobs.set(job.id, job)
      this.emitJob(job)
    }
    return job
  }

  private start(job: MockJob, workerId: string, at: number) {
    job.attempts++
    Object.assign(job, {
      status: 'running',
      locked_by: workerId,
      lease_until: iso(at + LEASE_MS),
      progress: 0,
      started_at: iso(at),
      updated_at: iso(at),
    })
    this.addEvent(job, 'started', at, `attempt ${job.attempts} on ${workerId}`)
  }

  private succeed(job: MockJob, at: number) {
    Object.assign(job, {
      status: 'succeeded',
      progress: 100,
      last_error: undefined,
      lease_until: undefined,
      locked_by: undefined,
      finished_at: iso(at),
      updated_at: iso(at),
      result: resultFor(job.type, job.payload),
    })
    this.addEvent(job, 'succeeded', at)
  }

  private fail(job: MockJob, error: string, at: number, retryAt?: number) {
    const final = job.attempts >= job.max_attempts
    Object.assign(job, {
      status: final ? 'dead' : 'failed',
      last_error: error,
      lease_until: undefined,
      locked_by: undefined,
      updated_at: iso(at),
    })
    if (final) {
      job.finished_at = iso(at)
      this.addEvent(job, 'dead', at, error)
    } else {
      job.run_at = iso(retryAt ?? at + backoffSeconds(job.attempts) * SECOND)
      this.addEvent(job, 'failed', at, error)
    }
  }

  private addEvent(job: MockJob, kind: string, at: number, message?: string) {
    job.timeline.push({ id: this.nextEventId++, job_id: job.id, kind, message, at: iso(at) })
  }

  private emitJob(job: MockJob) {
    const update: JobUpdate = {
      id: job.id,
      team_id: job.team_id,
      type: job.type,
      status: job.status,
      progress: Math.floor(job.progress),
      attempts: job.attempts,
      run_at: pgTime(job.run_at),
      locked_by: job.locked_by ?? null,
      last_error: job.last_error?.slice(0, 200) ?? null,
      updated_at: pgTime(job.updated_at),
    }
    this.emit('job', update)
  }

  private emitWorker(w: MockWorker, state: WorkerUpdate['state'] = w.state) {
    const { running_jobs, ...worker } = this.toWorker(w)
    this.emit('worker', { ...worker, state })
  }

  private nextRunnable(now: number): MockJob | undefined {
    let best: MockJob | undefined
    for (const job of this.jobs.values()) {
      if (job.status !== 'queued' && job.status !== 'failed') continue
      if (new Date(job.run_at).getTime() > now) continue
      if (!best || job.priority > best.priority || (job.priority === best.priority && job.id < best.id)) best = job
    }
    return best
  }

  private pruneJobs() {
    if (this.jobs.size <= MAX_TRACKED_JOBS) return
    const finished: JobStatus[] = ['succeeded', 'cancelled', 'dead']
    const protectedIds = new Set(this.deadGroups.flatMap((g) => g.sample_ids))
    for (const job of [...this.jobs.values()].sort((a, b) => a.id - b.id)) {
      if (this.jobs.size <= MAX_TRACKED_JOBS - 40) break
      if (finished.includes(job.status) && !protectedIds.has(job.id)) this.jobs.delete(job.id)
    }
  }

  private toJob({ timeline, ...job }: MockJob): Job {
    return { ...job, progress: Math.floor(job.progress) }
  }

  private seedWorkers(now: number) {
    const a = this.newWorker('running', now - 3 * HOUR - 1_200_000)
    const b = this.newWorker('running', now - 3 * HOUR - 1_100_000)
    a.done = 141_220
    b.done = 139_807
    this.workers.push(a, b)
  }

  private seedHistory(now: number) {
    for (let i = SAMPLE_WINDOW; i > 0; i--) {
      const ok = BACKGROUND_ARRIVALS_PER_SECOND * rand(0.75, 1.2)
      this.samples.push({
        at: iso(now - i * SECOND),
        queued: Math.round(30 + Math.sin(i / 9) * 14 + rand(0, 18)),
        workers: 2,
        succeeded: Math.round(ok),
        failed: Math.round(ok * rand(0, 0.03) * 10) / 10,
      })
    }
    const history: [number, ScalerDecision['action'], number, number, string][] = [
      [25e5, 'up', 2, 4, '1,950 runnable, oldest wait 12s'],
      [22e5, 'up', 4, 6, '3,200 runnable, oldest wait 41s'],
      [19e5, 'hold', 6, 6, 'wants 2: waiting out the 20s cool-down before scaling down'],
      [18e5, 'down', 6, 5, '120 runnable, below target for 20s'],
      [17.9e5, 'down', 5, 4, '64 runnable, below target for 23s'],
      [17.8e5, 'down', 4, 3, '51 runnable, below target for 26s'],
      [17.7e5, 'down', 3, 2, 'queue empty, cool-down passed'],
    ]
    for (const [msAgo, action, from, to, reason] of history) {
      this.decide(action, from, to, reason, now - 3 * HOUR - msAgo)
    }
  }

  private seedJobs() {
    const plan: [JobStatus, number][] = [
      ['running', 6],
      ['queued', 8],
      ['failed', 4],
      ['dead', 3],
      ['cancelled', 2],
      ['succeeded', 52],
    ]
    let seeded = false
    // Seed oldest first so ids rise with creation time, like the real identity column.
    const seeds = plan
      .flatMap(([status, count]) => Array.from({ length: count }, () => status))
      .map((status) => ({
        status,
        age: status === 'running' ? rand(4, 40) : status === 'queued' ? rand(1, 30) : rand(100, 3200),
      }))
      .sort((a, b) => b.age - a.age)
    for (const { status, age } of seeds) {
      if (age < 95 && !seeded) {
        this.seedRetriedExample()
        seeded = true
      }
      this.seedJob(this.teamByName(DASHBOARD_TEAM), status, age)
    }
  }

  private seedDeadSamples() {
    const team = this.teamByName(DASHBOARD_TEAM)
    for (const group of this.deadGroups) {
      group.sample_ids = Array.from({ length: 5 }, () => {
        const ageSeconds = (Date.now() - new Date(group.last_at).getTime()) / SECOND + rand(30, 900)
        return this.seedJob(team, 'dead', ageSeconds, { type: group.type, error: group.error }).id
      })
    }
  }

  // A job that failed once and then went through, to show a retry in the timeline.
  private seedRetriedExample() {
    this.seedJob(this.teamByName(DASHBOARD_TEAM), 'succeeded', 95, {
      type: 'webhook',
      priority: 7,
      failures: 1,
      error: 'webhook: got 503 Service Unavailable',
    })
  }

  private seedJob(
    team: MockTeam,
    target: JobStatus,
    ageSeconds: number,
    opts: { type?: string; priority?: number; failures?: number; error?: string } = {},
  ): MockJob {
    const now = Date.now()
    const type = opts.type ?? pick(team.seed.types)
    const job = this.createJob(team, type, now - ageSeconds * SECOND, { priority: opts.priority, track: true })
    const pastWorker = () => `w-${hex(4)}`
    const errorFor = () => opts.error ?? pick(JOB_TYPES[type].errors)
    let t = new Date(job.created_at).getTime() + rand(150, 1500)

    if (target === 'queued') return job
    if (target === 'cancelled') {
      Object.assign(job, { status: 'cancelled', finished_at: iso(t + 4000), updated_at: iso(t + 4000) })
      this.addEvent(job, 'cancelled', t + 4000)
      return job
    }
    if (target === 'running') {
      const live = this.workers.filter((w) => w.state === 'running')
      const worker = live.length ? live[this.runningCount() % live.length].id : pastWorker()
      this.start(job, worker, Math.min(t, now - 300))
      job.progress = rand(4, 70)
      return job
    }

    const failures =
      target === 'dead'
        ? job.max_attempts
        : target === 'failed'
          ? randInt(1, 2)
          : (opts.failures ?? (Math.random() < 0.25 ? randInt(1, 2) : 0))
    for (let i = 0; i < failures; i++) {
      this.start(job, pastWorker(), t)
      const error = errorFor()
      t += (error.includes('deadline exceeded') ? 15 : rand(0.3, 4)) * SECOND
      if (target === 'failed' && i === failures - 1) {
        this.fail(job, error, t, now + rand(4, 25) * SECOND)
        return job
      }
      this.fail(job, error, t)
      if (job.status === 'dead') return job
      t = new Date(job.run_at).getTime() + rand(50, 500)
    }
    this.start(job, pastWorker(), t)
    t += rand(0.3, 6) * SECOND
    this.succeed(job, Math.min(t, now - SECOND))
    return job
  }

  private snapshot(): Omit<Stats, 'samples'> {
    const tail = this.samples.slice(-5)
    const total = tail.reduce((sum, s) => sum + s.succeeded, 0) / Math.max(1, tail.length)
    const busy = this.queued > 1000 || this.submitted.some((s) => Date.now() - s.at < 10 * SECOND)
    const imageShare = busy ? 0.86 : 0.3
    const held = Math.round(Math.min(this.rateHeld, this.queued))
    const rates: Record<string, number> = {
      webhook: total * (1 - imageShare) * 0.7,
      'image.resize': total * imageShare,
      'email.send': Math.min(10, total * (1 - imageShare) * 0.3 + (held > 0 ? 6 : 0)),
    }
    return {
      counts: {
        queued: Math.round(this.queued),
        running: this.running,
        succeeded_today: Math.round(this.succeededToday),
        failed: Math.round(this.retrying),
        dead: Math.round(this.dead),
      },
      oldest_wait_seconds: this.oldestWait,
      runnable: Math.max(0, Math.round(this.queued - this.rateHeld)),
      queue_ceiling: QUEUE_CEILING,
      dead_groups: this.deadGroups.length,
      workers: {
        running: this.countWorkers('running'),
        starting: this.countWorkers('starting'),
        draining: this.countWorkers('draining'),
      },
      scaler: {
        current: this.workers.filter((w) => w.state !== 'draining').length,
        target: this.target,
        min: SCALER.min,
        max: SCALER.max,
        jobs_per_worker: SCALER.jobsPerWorker,
        interval_seconds: SCALER.intervalSeconds,
        cooldown_seconds: SCALER.cooldownSeconds,
        step_down_seconds: SCALER.stepDownSeconds,
        scale_down_at:
          this.belowTargetSince === null ? null : iso(this.belowTargetSince + SCALER.cooldownSeconds * SECOND),
      },
      types: TYPE_NAMES.map((type) => ({
        type,
        timeout_seconds: JOB_TYPES[type].timeoutSeconds,
        outgoing_per_second: JOB_TYPES[type].outgoingPerSecond,
        rate_per_second: rates[type] ?? 0,
        held: type === 'email.send' ? held : 0,
      })),
    }
  }

  private workerList(): Worker[] {
    return this.workers.map((w) => this.toWorker(w))
  }

  private toWorker(w: MockWorker): Worker {
    return {
      id: w.id,
      host: w.host,
      state: w.state,
      started_at: iso(w.startedAt),
      last_seen_at: iso(w.lastSeen),
      jobs_done: Math.round(w.done),
      running_jobs: this.runningOn(w.id).map((job) => ({ id: job.id, type: job.type, progress: Math.floor(job.progress) })),
    }
  }

  private shareOf(team: MockTeam) {
    const active = this.teams.filter((t) => t.demand > 1 || t.usage > 1)
    if (!active.includes(team)) return 0
    const weights = active.reduce((sum, t) => sum + planByName(t.plan).share_weight, 0)
    return planByName(team.plan).share_weight / weights
  }

  private rateOf(team: MockTeam) {
    return {
      jobs_last_minute: Math.round(team.usage),
      rejected_last_minute: Math.round(team.rejected),
      fair_share: this.shareOf(team),
    }
  }

  // The dashboard's team owns the queue the charts show; the others keep a small backlog.
  private queuedAndRunning(team: MockTeam) {
    const share = this.shareOf(team)
    const running = Math.min(planByName(team.plan).max_workers, Math.max(share ? 1 : 0, Math.round(share * this.running)))
    if (team.seed.name !== DASHBOARD_TEAM) {
      return { queued: Math.round((team.demand / 60) * rand(0.5, 2)), running }
    }
    return { queued: Math.round(this.queued), running }
  }

  private toTeam(team: MockTeam): Team {
    return { id: team.id, name: team.seed.name, plan: planByName(team.plan), created_at: team.createdAt }
  }

  private newKey(teamId: number, secret: string, createdAt: number): MockKey {
    return { id: this.nextKeyId++, team_id: teamId, prefix: displayPrefix(secret), created_at: iso(createdAt), secret }
  }

  private ownJob(teamId: number, id: number) {
    const job = this.jobs.get(id)
    if (!job || job.team_id !== teamId) throw new MockError(404, 'job not found')
    return job
  }

  private newWorker(state: WorkerState, startedAt: number, readyAt = startedAt): MockWorker {
    return {
      id: `w-${hex(4)}`,
      host: `tasklane-worker-${this.nextHost++}`,
      state,
      startedAt,
      readyAt,
      lastSeen: startedAt,
      heartbeatEvery: rand(2.6, 3.2),
      done: 0,
      drainAt: 0,
    }
  }

  private countWorkers(state: WorkerState) {
    return this.workers.filter((w) => w.state === state).length
  }

  private runningOn(workerId: string) {
    return [...this.jobs.values()].filter((j) => j.status === 'running' && j.locked_by === workerId)
  }

  private runningCount() {
    return [...this.jobs.values()].filter((j) => j.status === 'running').length
  }

  private randomPriority() {
    const roll = Math.random()
    return roll < 0.12 ? randInt(7, 9) : roll < 0.75 ? randInt(3, 6) : randInt(0, 2)
  }

  private teamById(id: number) {
    const team = this.teams.find((t) => t.id === id)
    if (!team) throw new MockError(404, 'team not found')
    return team
  }

  private teamByName(name: string) {
    return this.teams.find((t) => t.seed.name === name)!
  }
}
