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

export const JOB_TYPES: Record<string, JobTypeInfo> = {
  'webhook.deliver': {
    timeoutSeconds: 30,
    outgoingPerSecond: 200,
    speed: [28, 60],
    errors: ['payment timeout: no answer within 30s', 'upstream returned 503 Service Unavailable', 'connection reset by peer'],
  },
  'image.resize': {
    timeoutSeconds: 120,
    outgoingPerSecond: null,
    speed: [7, 16],
    errors: ['decode error: unsupported format HEIC', 'image exceeds 25 MB limit'],
  },
  'email.send': {
    timeoutSeconds: 20,
    outgoingPerSecond: 10,
    speed: [22, 45],
    errors: ['smtp 421: service not available, try again later', 'smtp 550: mailbox unavailable'],
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
    types: ['webhook.deliver', 'email.send', 'image.resize'],
    demandPerMinute: 120,
    keys: [{ secret: DEV_API_KEY, daysAgo: 30 }],
  },
  {
    name: 'acme-billing',
    plan: 'pro',
    daysOld: 240,
    types: ['webhook.deliver', 'email.send'],
    demandPerMinute: 820,
    keys: [{ daysAgo: 210 }, { daysAgo: 86 }, { daysAgo: 41 }],
  },
  {
    name: 'northwind',
    plan: 'enterprise',
    daysOld: 160,
    types: ['webhook.deliver', 'email.send', 'image.resize'],
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
    types: ['email.send', 'webhook.deliver'],
    demandPerMinute: 38,
    keys: [{ daysAgo: 7 }],
  },
]

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
    error: 'payment timeout: no answer within 30s',
    type: 'webhook.deliver',
    count: 4_200,
    teams: ['default', 'acme-billing'],
    firstHoursAgo: 2.3,
    lastMinutesAgo: 3,
    spiking: true,
  },
  {
    error: 'smtp 550: mailbox unavailable',
    type: 'email.send',
    count: 1_312,
    teams: ['northwind', 'default'],
    firstHoursAgo: 26,
    lastMinutesAgo: 11,
  },
  {
    error: 'image exceeds 25 MB limit',
    type: 'image.resize',
    count: 846,
    teams: ['default', 'harbor-photos'],
    firstHoursAgo: 120,
    lastMinutesAgo: 42,
  },
  {
    error: 'webhook 410 Gone: endpoint was removed by the receiver',
    type: 'webhook.deliver',
    count: 391,
    teams: ['northwind', 'default'],
    firstHoursAgo: 72,
    lastMinutesAgo: 126,
  },
  {
    error: 'decode error: unsupported format HEIC',
    type: 'image.resize',
    count: 172,
    teams: ['default'],
    firstHoursAgo: 216,
    lastMinutesAgo: 330,
  },
]

export function payloadFor(type: string, team: string): unknown {
  if (type === 'webhook.deliver') {
    return {
      url: `https://hooks.${team}.example.com/events`,
      event: pick(['invoice.paid', 'order.shipped', 'customer.updated', 'refund.created']),
      body: { id: `evt_${hex(12)}`, amount: randInt(900, 48_000), currency: 'USD' },
      timeout_s: 30,
    }
  }
  if (type === 'email.send') {
    return {
      to: pick(['m.rivera@example.com', 'ops@example.org', 'j.chen@example.net', 'billing@example.com']),
      template: pick(['invoice-ready', 'password-reset', 'weekly-digest', 'order-confirmation']),
      vars: { name: pick(['Maya', 'Jun', 'Priya', 'Tom', 'Ana']), invoice: `INV-${randInt(20_000, 29_999)}` },
    }
  }
  return {
    source: `s3://${team}/uploads/IMG_${randInt(1000, 9999)}.jpg`,
    sizes: [320, 640, 1280],
    format: 'webp',
    quality: 82,
  }
}

export function resultFor(type: string, team: string, payload: unknown): unknown {
  if (type === 'webhook.deliver') {
    return { http_status: 200, response_ms: randInt(80, 900), body_bytes: randInt(2, 180) }
  }
  if (type === 'email.send') {
    const to = (payload as { to?: string }).to
    return { message_id: `<${hex(16)}@mailpit.local>`, accepted: to ? [to] : [], smtp: '250 OK' }
  }
  const sizes = (payload as { sizes?: number[] }).sizes ?? [320, 640, 1280]
  return {
    outputs: sizes.map((width) => ({
      width,
      key: `${team}/thumbs/${hex(8)}_${width}.webp`,
      bytes: width * randInt(55, 90),
    })),
    source_bytes: randInt(2_400_000, 9_800_000),
  }
}
