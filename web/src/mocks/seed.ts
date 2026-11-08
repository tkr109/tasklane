import type { Plan, PlanName } from '../api/types'
import { hex, pick, randInt } from './random'

export const SCALER = {
  min: 2,
  max: 10,
  jobsPerWorker: 600,
  intervalSeconds: 2,
  cooldownSeconds: 20,
  stepDownSeconds: 3,
}

export const QUEUE_CEILING = 50_000

// Rough throughput numbers that make the charts move like a real system.
export const JOBS_PER_WORKER_SECOND = 48
export const BACKGROUND_ARRIVALS_PER_SECOND = 26
export const WORKER_CONCURRENCY = 4

export const PLANS: Plan[] = [
  { id: 1, name: 'free', share_weight: 1, max_workers: 2, jobs_per_minute: 100, burst: 200 },
  { id: 2, name: 'pro', share_weight: 3, max_workers: 6, jobs_per_minute: 1_000, burst: 2_000 },
  { id: 3, name: 'enterprise', share_weight: 10, max_workers: 10, jobs_per_minute: 10_000, burst: 20_000 },
]

export const planByName = (name: PlanName) => PLANS.find((p) => p.name === name)!

export interface JobTypeInfo {
  timeoutSeconds: number
  outgoingPerSecond: number | null
  // Progress made per second while running, as a percentage range.
  speed: [number, number]
  errors: string[]
}

// The job types and time limits the API registers, with the default outgoing limits.
export const JOB_TYPES: Record<string, JobTypeInfo> = {
  'email.send': {
    timeoutSeconds: 30,
    outgoingPerSecond: 10,
    speed: [22, 45],
    errors: [
      'email.send: 421 4.3.2 Service not available, closing transmission channel',
      'email.send: 550 5.1.1 mailbox unavailable',
    ],
  },
  flaky: {
    timeoutSeconds: 30,
    outgoingPerSecond: null,
    speed: [30, 60],
    errors: ['flaky: failed on purpose'],
  },
  'image.resize': {
    timeoutSeconds: 60,
    outgoingPerSecond: null,
    speed: [7, 16],
    errors: ['image.resize: fetch source: got 404 Not Found', 'image.resize: source is over 20 MB'],
  },
  sleep: {
    timeoutSeconds: 300,
    outgoingPerSecond: null,
    speed: [4, 9],
    errors: [],
  },
  webhook: {
    timeoutSeconds: 15,
    outgoingPerSecond: 200,
    speed: [28, 60],
    errors: [
      'webhook: Post "https://hooks.example.com/events": context deadline exceeded',
      'webhook: got 503 Service Unavailable',
      'webhook: got 502 Bad Gateway: upstream connect error',
    ],
  },
}

export const TYPE_NAMES = Object.keys(JOB_TYPES)

// The Makefile and compose defaults, so the same .env works against mocks and the real API.
export const DEV_API_KEY = 'tk_live_localdev'
export const ADMIN_TOKEN = 'dev-admin-token'

// The team that owns the dev key. Every tracked job, and the flood, belongs to it.
export const DASHBOARD_TEAM = 'default'

export interface KeySeed {
  secret?: string
  daysAgo: number
}

export interface TeamSeed {
  name: string
  plan: PlanName
  daysOld: number
  types: string[]
  demandPerMinute: number
  keys: KeySeed[]
}

export const TEAMS: TeamSeed[] = [
  {
    name: DASHBOARD_TEAM,
    // The real seed puts this team on Pro; Enterprise's burst lets a 10k flood through.
    plan: 'enterprise',
    daysOld: 30,
    types: ['webhook', 'email.send', 'image.resize'],
    demandPerMinute: 120,
    keys: [{ secret: DEV_API_KEY, daysAgo: 30 }],
  },
  {
    name: 'acme-billing',
    plan: 'pro',
    daysOld: 240,
    types: ['webhook', 'email.send'],
    demandPerMinute: 820,
    keys: [{ daysAgo: 210 }, { daysAgo: 86 }, { daysAgo: 41 }],
  },
  {
    name: 'northwind',
    plan: 'enterprise',
    daysOld: 160,
    types: ['webhook', 'email.send', 'image.resize'],
    demandPerMinute: 3_400,
    keys: [{ daysAgo: 133 }, { daysAgo: 60 }],
  },
  {
    name: 'harbor-photos',
    plan: 'pro',
    daysOld: 25,
    types: ['image.resize'],
    demandPerMinute: 430,
    keys: [{ daysAgo: 19 }],
  },
  {
    name: 'side-project',
    plan: 'free',
    daysOld: 9,
    types: ['email.send', 'webhook'],
    demandPerMinute: 38,
    keys: [{ daysAgo: 7 }],
  },
]

// Roughly the API's normalize_error: quoted values and any word with a digit in it are blanked out.
export const errorPattern = (error: string) =>
  error
    .replace(/"[^"]*"/g, '<str>')
    .replace(/[^\s:]*\d[^\s:]*/g, '<n>')
    .slice(0, 200)

export interface DeadGroupSeed {
  error: string
  type: string
  count: number
  teams: string[]
  firstHoursAgo: number
  lastMinutesAgo: number
  // Mark a group that's still growing so its sparkline ends in a spike.
  spiking?: boolean
}

export const DEAD_GROUPS: DeadGroupSeed[] = [
  {
    error: 'webhook: Post "https://hooks.acme.example.com/events": context deadline exceeded',
    type: 'webhook',
    count: 4_200,
    teams: ['default', 'acme-billing'],
    firstHoursAgo: 2.3,
    lastMinutesAgo: 3,
    spiking: true,
  },
  {
    error: 'email.send: 550 5.1.1 mailbox unavailable',
    type: 'email.send',
    count: 1_312,
    teams: ['northwind', 'default'],
    firstHoursAgo: 26,
    lastMinutesAgo: 11,
  },
  {
    error: 'image.resize: source is over 20 MB',
    type: 'image.resize',
    count: 846,
    teams: ['default', 'harbor-photos'],
    firstHoursAgo: 120,
    lastMinutesAgo: 42,
  },
  {
    error: 'webhook: dial tcp 10.0.4.17:443: connect: connection refused',
    type: 'webhook',
    count: 391,
    teams: ['northwind', 'default'],
    firstHoursAgo: 72,
    lastMinutesAgo: 126,
  },
  {
    error: 'image.resize: fetch source: got 404 Not Found',
    type: 'image.resize',
    count: 172,
    teams: ['default'],
    firstHoursAgo: 216,
    lastMinutesAgo: 330,
  },
]

export function payloadFor(type: string, team: string): unknown {
  switch (type) {
    case 'webhook':
      return {
        url: `https://hooks.${team}.example.com/events`,
        body: {
          id: `evt_${hex(12)}`,
          type: pick(['invoice.paid', 'order.shipped', 'customer.updated', 'refund.created']),
          amount: randInt(900, 48_000),
        },
      }
    case 'email.send':
      return {
        to: pick(['m.rivera@example.com', 'ops@example.org', 'j.chen@example.net', 'billing@example.com']),
        template: pick(['welcome', 'password-reset', 'invoice-ready']),
        data: { name: pick(['Maya', 'Jun', 'Priya', 'Tom', 'Ana']) },
      }
    case 'image.resize':
      return {
        source: `https://cdn.${team}.example.com/uploads/IMG_${randInt(1000, 9999)}.jpg`,
        sizes: [320, 640, 1280],
        format: 'jpeg',
      }
    default:
      return { seconds: randInt(2, 20) }
  }
}

const SUBJECTS: Record<string, string> = {
  welcome: 'Welcome to Tasklane',
  'password-reset': 'Reset your password',
  'invoice-ready': 'Your invoice is ready',
}

export function resultFor(type: string, payload: unknown): unknown {
  switch (type) {
    case 'webhook':
      return { status: 200, body: '{"received":true}' }
    case 'email.send': {
      const p = payload as { to?: string; template?: string }
      return { to: p.to ?? '', subject: SUBJECTS[p.template ?? ''] ?? 'Hello' }
    }
    case 'image.resize': {
      const p = payload as { source?: string; sizes?: number[]; format?: string }
      const format = p.format ?? 'jpeg'
      const dir = `/data/images/${hex(16)}`
      return {
        source: p.source ?? '',
        format,
        outputs: (p.sizes ?? [320]).map((width) => ({
          path: `${dir}/w${width}.${format === 'jpeg' ? 'jpg' : format}`,
          width,
          height: Math.round((width * 2) / 3),
        })),
      }
    }
    default:
      return undefined
  }
}
