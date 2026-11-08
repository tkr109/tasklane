import { delay, http, HttpResponse } from 'msw'
import { JOB_STATUSES, type JobSpec, type JobStatus, type PlanName } from '../api/types'
import { MockError, World } from './world'

const world = new World()

// Turns a MockError into the same JSON error the Go API writes.
async function respond<T>(work: () => T, status = 200) {
  await delay(120)
  try {
    return HttpResponse.json(work() as object, { status })
  } catch (err) {
    if (!(err instanceof MockError)) throw err
    const headers = err.retryAfter ? { 'Retry-After': String(err.retryAfter) } : undefined
    return HttpResponse.json({ error: err.message }, { status: err.status, headers })
  }
}

const idParam = (value: string | readonly string[] | undefined) => Number(value)

export const handlers = [
  http.get('/healthz', () => HttpResponse.json({ status: 'ok' })),

  http.get('/api/jobs', ({ request }) => {
    const params = new URL(request.url).searchParams
    const status = params.get('status') ?? ''
    const cursor = params.get('cursor')
    const limit = Math.min(Number(params.get('limit')) || 50, 200)
    return respond(() => {
      if (status && !JOB_STATUSES.includes(status as JobStatus)) {
        throw new MockError(400, `invalid request: unknown status "${status}"`)
      }
      const filters = { status: (status || undefined) as JobStatus | undefined, type: params.get('type') || undefined }
      return world.listJobs(filters, cursor ? Number(cursor) : null, limit)
    })
  }),

  http.post('/api/jobs/batch', async ({ request }) => {
    const body = (await request.json()) as { jobs?: JobSpec[] }
    return respond(() => ({ jobs: world.submitBatch(body.jobs ?? []) }), 201)
  }),

  http.get('/api/jobs/:id', ({ params }) => respond(() => world.getJob(idParam(params.id)))),

  http.post('/api/jobs/:id/cancel', ({ params }) => respond(() => world.cancelJob(idParam(params.id)))),

  http.post('/api/jobs/:id/retry', ({ params }) => respond(() => world.retryJob(idParam(params.id)))),

  http.get('/api/stats', () => respond(() => world.stats())),

  http.get('/api/workers', () => respond(() => world.workersResponse())),

  http.get('/api/teams', () => respond(() => world.teamsResponse())),

  http.patch('/api/teams/:id', async ({ params, request }) => {
    const body = (await request.json()) as { plan: PlanName }
    return respond(() => world.switchPlan(idParam(params.id), body.plan))
  }),

  http.post('/api/teams/:id/keys', async ({ params, request }) => {
    const body = (await request.json()) as { name?: string }
    return respond(() => world.createKey(idParam(params.id), body.name ?? ''), 201)
  }),

  http.post('/api/teams/:id/keys/:keyId/revoke', ({ params }) =>
    respond(() => world.revokeKey(idParam(params.id), idParam(params.keyId))),
  ),

  http.get('/api/dead-groups', () => respond(() => world.deadGroupsResponse())),

  http.get('/api/dead-groups/:id/jobs', ({ params, request }) => {
    const limit = Number(new URL(request.url).searchParams.get('limit')) || 5
    return respond(() => ({ jobs: world.deadGroupJobs(String(params.id), limit) }))
  }),

  http.post('/api/dead-groups/:id/retry', ({ params }) => respond(() => world.retryDeadGroup(String(params.id)))),

  http.get('/api/events', () => {
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
