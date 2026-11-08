import { delay, http, HttpResponse } from 'msw'
import { JOB_STATUSES, type JobSpec, type JobStatus, type PlanName } from '../api/types'
import { ADMIN_TOKEN } from './seed'
import { MockError, World } from './world'

const world = new World()

// Turns a MockError into the same JSON error the Go API writes.
async function respond<T>(work: () => T, status = 200) {
  await delay(120)
  try {
    return HttpResponse.json(work() as object, { status })
  } catch (err) {
    if (!(err instanceof MockError)) throw err
    const headers: Record<string, string> = {}
    if (err.retryAfter) headers['Retry-After'] = String(err.retryAfter)
    if (err.status === 401) headers['WWW-Authenticate'] = 'Bearer'
    return HttpResponse.json({ error: err.message }, { status: err.status, headers })
  }
}

// Job routes run as the team that owns the request's API key.
const withTeam = <T>(request: Request, work: (teamId: number) => T, status?: number) =>
  respond(() => work(world.teamForKey(request.headers.get('Authorization'))), status)

const withAdmin = <T>(request: Request, work: () => T, status?: number) =>
  respond(() => {
    world.checkAdmin(request.headers.get('Authorization'))
    return work()
  }, status)

const idParam = (value: string | readonly string[] | undefined) => Number(value)

export const handlers = [
  http.get('/healthz', () => HttpResponse.json({ status: 'ok' })),

  http.get('/api/jobs', ({ request }) => {
    const params = new URL(request.url).searchParams
    const status = params.get('status') ?? ''
    const cursor = params.get('cursor')
    const limit = Math.min(Number(params.get('limit')) || 50, 200)
    return withTeam(request, (team) => {
      if (status && !JOB_STATUSES.includes(status as JobStatus)) {
        throw new MockError(400, `invalid request: unknown status "${status}"`)
      }
      const filters = { status: (status || undefined) as JobStatus | undefined, type: params.get('type') || undefined }
      return world.listJobs(team, filters, cursor ? Number(cursor) : null, limit)
    })
  }),

  http.post('/api/jobs/batch', async ({ request }) => {
    const body = (await request.json()) as { jobs?: JobSpec[] }
    return withTeam(request, (team) => ({ jobs: world.submitBatch(team, body.jobs ?? []) }), 201)
  }),

  http.get('/api/jobs/:id', ({ params, request }) =>
    withTeam(request, (team) => world.getJob(team, idParam(params.id))),
  ),

  http.post('/api/jobs/:id/cancel', ({ params, request }) =>
    withTeam(request, (team) => world.cancelJob(team, idParam(params.id))),
  ),

  http.post('/api/jobs/:id/retry', ({ params, request }) =>
    withTeam(request, (team) => world.retryJob(team, idParam(params.id))),
  ),

  http.get('/api/plans', ({ request }) => withAdmin(request, () => world.plansResponse())),

  http.get('/api/teams', ({ request }) => withAdmin(request, () => world.teamsResponse())),

  http.put('/api/teams/:id/plan', async ({ params, request }) => {
    const body = (await request.json()) as { plan: PlanName }
    return withAdmin(request, () => world.switchPlan(idParam(params.id), body.plan))
  }),

  http.get('/api/teams/:id/keys', ({ params, request }) =>
    withAdmin(request, () => world.listKeys(idParam(params.id))),
  ),

  http.post('/api/teams/:id/keys', ({ params, request }) =>
    withAdmin(request, () => world.createKey(idParam(params.id)), 201),
  ),

  http.post('/api/teams/:id/keys/:keyId/revoke', ({ params, request }) =>
    withAdmin(request, () => world.revokeKey(idParam(params.id), idParam(params.keyId))),
  ),

  // The routes below aren't in the backend yet; their shapes follow the design doc.
  http.get('/api/stats', ({ request }) => withAdmin(request, () => world.stats())),

  http.get('/api/workers', ({ request }) => withAdmin(request, () => world.workersResponse())),

  http.get('/api/dead-groups', ({ request }) => withAdmin(request, () => world.deadGroupsResponse())),

  http.get('/api/dead-groups/:id/jobs', ({ params, request }) => {
    const limit = Number(new URL(request.url).searchParams.get('limit')) || 5
    return withAdmin(request, () => ({ jobs: world.deadGroupJobs(String(params.id), limit) }))
  }),

  http.post('/api/dead-groups/:id/retry', ({ params, request }) =>
    withAdmin(request, () => world.retryDeadGroup(String(params.id))),
  ),

  http.get('/api/events', ({ request }) => {
    if (new URL(request.url).searchParams.get('token') !== ADMIN_TOKEN) {
      return HttpResponse.json({ error: 'missing or invalid admin token' }, { status: 401 })
    }
    const encoder = new TextEncoder()
    let unsubscribe = () => {}
    // A plain stream rather than msw's sse(), so a closed EventSource unsubscribes cleanly.
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        unsubscribe = world.subscribe((event, data) => {
          try {
            controller.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`))
          } catch {
            unsubscribe()
          }
        })
      },
      cancel() {
        unsubscribe()
      },
    })
    return new HttpResponse(stream, {
      headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
    })
  }),
]
