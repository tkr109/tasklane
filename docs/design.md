# Tasklane design

Tasklane is a background job engine. Apps send it jobs over HTTP, workers run them, and a
dashboard shows what is happening live. The headline feature is autoscaling: a scaler process
watches the queue and adds or removes worker containers on its own.

## Goals

- Jobs are never lost and never run twice, even when workers crash mid-job.
- One noisy team can't starve the others.
- Workers scale up under load and back down when the queue drains.
- Everything runs locally with one command (`make up`).

## Non-goals

- Real billing. Plans exist, but switching plan is an admin button, not a payment.
- Kubernetes. The scaler talks to Docker. A k8s driver could come later.
- Running user-uploaded code.

## Stack

- Go 1.25, standard library router (`net/http`), `log/slog` for logs
- PostgreSQL 16 for everything: the queue, rate limits, heartbeats, and live events
  (`LISTEN/NOTIFY`). `pgx` driver, handwritten SQL in repositories. No Redis for now.
- React + TypeScript + Vite for the dashboard
- Docker Compose for local runs, GitHub Actions for CI
- Mailpit as a fake inbox for the email job

## Processes

One binary, three modes:

```
tasklane api      # HTTP API + SSE stream for the dashboard
tasklane worker   # pulls jobs and runs them
tasklane scaler   # watches queue depth, starts/stops worker containers
```

They share code but run as separate containers, so workers can scale on their own.

## Folder layout

```
tasklane/
├── main.go            # reads the mode (api | worker | scaler) and starts it
├── config/            # env vars and defaults
├── routes/            # URL -> controller wiring
├── controllers/       # HTTP handlers, request/response shapes
├── middleware/        # API key auth, request logging
├── services/          # business logic: queue, fairness, scheduler, scaler, events
├── repositories/      # SQL queries, one file per table group
├── models/            # plain data types (Job, Team, Plan, ApiKey...)
├── jobs/              # the real job handlers: webhook, image, email
├── migrations/        # SQL migration files
├── web/               # dashboard
├── deploy/            # Dockerfiles, docker-compose.yml
├── scripts/           # demo flood, load test
└── docs/
```

Import direction is one way only: `controllers -> services -> repositories -> models`.
`models` imports nothing from the project. This keeps Go's no-import-cycles rule happy.
There is no `utils/` folder; a helper lives next to the code that uses it.

## Data model (first cut)

- `plans`: name, share weight, max concurrent workers, jobs per minute, burst
- `teams`: name, plan_id
- `api_keys`: team_id, prefix (for display), sha256 hash (unique, what a key is looked up
  by), revoked_at
- `jobs`: team_id, type, payload (jsonb), priority, status, attempts, max_attempts,
  run_at, lease_until, locked_by, idempotency_key, progress, last_error, throttled_at
  (when its type's rate limit last pushed it back), timestamps
- `job_events`: job_id, kind, message, at (the timeline)
- `schedules`: team_id, cron expression, timezone, job template (type, payload, priority,
  max_attempts), enabled, next_run_at, last_run_at
- `workers`: id, host, started_at, last_seen_at (heartbeat). There is no current-job column:
  a worker's running jobs are the ones whose `jobs.locked_by` is its id.
- `team_usage`: team_id, running (jobs running now), served (a decaying count of jobs
  started), served_at. Used for fair share.
- `rate_buckets`: key (`team:<id>` or `type:<name>`), tokens, updated_at

Redis can come back later behind the same service interfaces if load tests show Postgres is
the bottleneck for rate limits or events.

Job statuses: `queued -> running -> succeeded | failed -> dead`. `cancelled` from queued.
`failed` means "this attempt failed and the job is waiting out its backoff"; once `run_at`
passes it is runnable again, just like `queued`. `dead` is the final failure.

## Plans

| Plan       | Share weight | Worker cap | Jobs per minute | Burst  |
|------------|-------------:|-----------:|----------------:|-------:|
| Free       | 1            | 2          | 100             | 200    |
| Pro        | 3            | 6          | 1,000           | 2,000  |
| Enterprise | 10           | 10         | 10,000          | 20,000 |

The worker cap is the most jobs a team can have running at once. Burst is twice the
per-minute rate.

## Core behaviour

**Grabbing a job.** A worker reads the backlog, picks whose turn it is (see Fair share),
and claims a job with `FOR UPDATE SKIP LOCKED`, setting `status = running`,
`lease_until = now() + lease`, and `locked_by = worker id`. Workers wake on Postgres
`LISTEN/NOTIFY` instead of polling hard (a slow poll stays as a fallback), and also look
again whenever one of their jobs finishes.

**Leases and crashes.** A running worker extends its lease every few seconds. If a worker
dies, its lease runs out and a reaper puts the job back to `queued`. No job is lost. The
reaper also deletes worker rows that missed 10 heartbeats in a row.

**Duplicates.** Submits may carry an `Idempotency-Key` header. A unique index on
`(team_id, idempotency_key)` means a repeat returns the original job instead of a new one.

**Retries.** An attempt is counted when a worker grabs the job, so a crash mid-job still
uses one up. The one exception is a job its type's rate limit sends straight back (see Rate
limits): that grab gives the attempt back. On failure: `run_at = now() + backoff(attempt)` with exponential growth and
jitter, and the job sits in `failed` until then. After `max_attempts` the job goes to
`dead`. The dashboard can retry dead jobs.

**Timeouts.** Each job type has a time limit. A job past it is cancelled and counts as a
failure.

**Clean shutdown.** On SIGTERM a worker stops taking new jobs, finishes the ones it has (up to
a grace period), then exits. Anything unfinished falls back via the lease.

**Priorities.** 0 (low) to 9 (urgent), 5 by default. Effective priority grows with wait
time (aging) so low jobs still run: `priority + waited / PRIORITY_AGING_STEP`. With the
default step of 30s, a priority 0 job that has waited 4.5 minutes ties a fresh 9.

**Fairness.** Teams take turns, weighted by their plan's share weight. A team at its plan's
worker cap is skipped. See Fair share below.

**Scheduling.** Delayed jobs are just `run_at` in the future. Repeating jobs come from
`schedules`. Only one scheduler fires them: whoever holds a Postgres advisory lock. If that
process dies, the lock frees and another takes over. See Scheduling below.

## Fair share

A grab is two queries with a decision in Go between them.

1. **Backlog**, no locks. For every team under its worker cap: its weight, its served count,
   and the oldest runnable job at each priority level. That is one probe per level on the
   `(team_id, priority, run_at, id)` index, so it stays cheap however long the queue is.
2. **Pick.** A team's share is its served count divided by its plan weight. The lowest share
   is furthest behind, so teams are tried in that order. Inside a team, the level whose
   oldest job has the highest aged priority wins.
3. **Grab** from that team and level, in one statement: lock the oldest job there with
   `SKIP LOCKED`, bump the team's `team_usage` row only if `running < max_workers`, and mark
   the job running only if that bump happened. If the level ran dry or the team filled up in
   the meantime, the next team in line is tried.

Why this holds with many workers grabbing at once:

- `SKIP LOCKED` means no two workers claim the same job and none waits on another's job row.
  Workers that pick the same team and level just take the next oldest job.
- Grabs for the same team queue on its `team_usage` row. After the wait, Postgres rechecks
  `running < max_workers` against the latest committed count, so a team can't pass its cap.
- `running` is kept by a trigger on `jobs.status`, so every way in and out of `running`
  (grab, finish, retry, reaper) updates it in the same transaction.
- `served` decays by a factor of e every `FAIR_SHARE_WINDOW` (a minute), so it roughly counts
  the last minute. Old work doesn't count against a team for long, and a team coming back
  from idle catches up for at most about a window.

When every team is busy, always serving the lowest served-per-weight keeps those numbers
level, so served counts follow the 1:3:10 weights. Caps still apply on top: a team whose
fair share is more slots than its cap stops at the cap, and the spare slots go to the others.

Lock order: a grab locks a job row and then its team's row; a finish does the same through
the trigger. Nothing waits on a job row while holding a team row, so grabs and finishes
can't deadlock. Two reapers freeing jobs of several teams at once could, so the reaper runs
under an advisory transaction lock: one reaps, the others skip that tick.

## Auth

Apps call the API with `Authorization: Bearer tk_live_...`. Only a sha256 hash of each key
and a short display prefix are stored; the full key is shown once, when it is created. The
middleware resolves the key to its team and plan and caches that for `AUTH_CACHE_TTL` (5s),
so a plan switch shows within that time. A revoke shows at once on the API process that
handled it, which drops that team's cached keys, and within the TTL on the others.
Scheduling reads plans straight from Postgres, so a plan switch changes weights and caps on
the next grab.

The admin endpoints the dashboard uses take a single shared `ADMIN_TOKEN` the same way. It is
a demo, so there are no users or logins. Local runs set `DEV_API_KEY`, which the api makes a
live key of the default team on startup, adding it again or taking back a revoke.

## Rate limits

All in Postgres, in the `rate_buckets` table. A bucket is refilled from its `updated_at` and
spent from in one atomic upsert (`INSERT ... ON CONFLICT DO UPDATE ... WHERE` enough tokens,
`RETURNING` what is left), so API processes can't overspend between them.

1. **Incoming.** A bucket per team, refilled at the plan's jobs per minute up to its burst.
   A submit costs 1 and a batch of N costs N; a batch bigger than the burst could never fit
   and gets `400`. Over the limit returns `429` with `Retry-After` (whole seconds until
   enough tokens are back). Submit responses carry `X-RateLimit-Limit` (the bucket size,
   i.e. the burst) and `X-RateLimit-Remaining` (whole tokens left). The check sits in the
   jobs controller rather than a middleware because the cost depends on the batch size.
   The teams admin list shows `usage.jobs_last_minute` next to the plan's jobs per minute.
2. **Outgoing.** `TYPE_RATE_LIMITS` (default `email=10/s,webhook=200/s`) gives a type a
   bucket shared by all workers, with a burst of a tenth of a second's worth (at least 1).
   After a grab, a job of a limited type needs a token. Without one it goes straight back to
   `queued` until the next token is due, with its attempt and its team's fair share turn
   given back, a `throttled` event on its timeline, and `throttled_at` set. That worker then
   leaves the type alone until the token is due, so a saturated type doesn't spin.
   `HeldByLimit` counts the waiting jobs of every type throttled in a recent window, which
   is what the scaler should leave out.
3. **Overload.** Once `QUEUE_CEILING` (default 100,000) runnable jobs are waiting, submits
   get `503` with `Retry-After: 5` until it drains. The count is shared by every request and
   refreshed at most once a second, so a flood doesn't turn into a flood of counts.

Scheduled jobs skip the incoming limit and the overload check; only submits pay.

## Scheduling

**Delayed jobs.** A submit (or each job in a batch) may give `run_at` (RFC 3339) or `delay`
(a Go duration such as `90s` or `2h30m`), up to a year out. A time in the past means now,
since a backdated job would jump the queue through aging.

**Repeating jobs.** A schedule has a cron expression (five fields, or a descriptor such as
`@hourly` or `@every 2h`), parsed with robfig/cron's parser, and an IANA time zone (UTC by
default). The API refuses anything firing more than once a minute. Changing a schedule
works out its next run again from now.

**One leader.** Every api process runs a scheduler on a connection of its own. Each tick
(`SCHEDULER_TICK`, 1s) it tries `pg_try_advisory_lock`; the one that gets it leads and fires
through that same connection, so a process whose connection died can't fire. When the
leader exits or its session drops, the lock goes with the session and another api takes
over on its next tick.

**Exactly once per tick.** Firing is one transaction: lock the due schedules
(`FOR UPDATE SKIP LOCKED`), insert each one's job with the idempotency key
`schedule:<id>:<tick>` (`ON CONFLICT DO NOTHING`), and move `next_run_at` on. Even two
processes that both believe they lead can't fire a tick twice. The next run counts from now,
not from the tick that fired, so after an outage a schedule runs one catch-up, not one per
missed tick.

## Autoscaling

The scaler loop runs every few seconds:

- Reads queued count, oldest wait time, and running workers.
- Ignores jobs that are only waiting on an outgoing rate limit (more workers won't help).
- Wants `ceil(runnable / jobs_per_worker)` workers, clamped to `[min, max]`.
- Scales up right away, scales down only after a cool-down, one step at a time.
- Starts and stops worker containers through the Docker Engine API.

Every decision is logged and published so the dashboard can chart it.

## Live updates

Workers and the API publish events (job status, progress, worker up/down, scaler decision)
with Postgres `NOTIFY`. Every API instance `LISTEN`s and relays them to browsers over SSE, so
it doesn't matter which instance a browser hits. Payloads stay small (ids + a few fields). Progress events are throttled to ~2/s per job. SSE event ids
let a reconnecting browser catch up.

## Dashboard pages

- **Overview**: queued / running / done / dead counts, queue depth vs worker count chart,
  throughput chart, "Flood 10k jobs" demo button
- **Jobs**: table with filters, live progress bars, bulk retry/cancel
- **Job detail**: payload, result, timeline, attempts
- **Workers**: live list, current job, heartbeat age, scaler decisions log
- **Teams**: plan, usage vs limit, API keys, one-click plan switch
- **Dead jobs**: grouped by error, retry group

## API (first cut)

```
POST   /api/jobs                               submit (Idempotency-Key, run_at or delay)
POST   /api/jobs/batch                         submit many
GET    /api/jobs                               list, cursor pagination, filters
GET    /api/jobs/{id}                          detail + timeline
POST   /api/jobs/{id}/cancel
POST   /api/jobs/{id}/retry
GET    /api/schedules                          the team's schedules
POST   /api/schedules                          create {cron, timezone, type, payload, ...}
GET    /api/schedules/{id}
PUT    /api/schedules/{id}                     replace, enabled: false pauses it
DELETE /api/schedules/{id}
GET    /api/workers
GET    /api/stats
GET    /api/plans                              admin
GET    /api/teams                              admin: teams with plan and usage
POST   /api/teams                              admin: create {name, plan}
PUT    /api/teams/{id}/plan                    admin: switch plan {plan}
GET    /api/teams/{id}/keys                    admin
POST   /api/teams/{id}/keys                    admin: returns the full key once
POST   /api/teams/{id}/keys/{key}/revoke       admin
GET    /api/events                             SSE stream
GET    /healthz
```

## Testing

- Unit tests for backoff, fairness picking, scaler decisions, rate limit math.
- Integration tests against a real Postgres (testcontainers): double-grab, lease
  expiry, idempotency, starvation, plan weights, worker caps, auth, leader failover.
- CI runs `go vet`, tests, and the web build on every push.

## Build order

1. Skeleton: compose (Postgres), config, migrations, `api` with `/healthz`
2. Jobs: submit, grab, leases, retries, dead, timeouts, clean shutdown
3. Teams, plans, API keys, auth middleware
4. Priorities with aging, fairness, worker caps
5. Rate limits (incoming, outgoing, overload)
6. Delayed + repeating jobs, scheduler leader lock
7. Events over Postgres `NOTIFY` + SSE, progress
8. Scaler with Docker
9. Real jobs: webhook, image resize, email
10. Dashboard
11. README, diagram, benchmarks, CI

## Conventions

- Comments only where the code can't say why. One line, two at most.
- Small commits. Short title, a few lines of body, around 30-40 words.
