# Tasklane design

Tasklane is a background job engine. Apps send it jobs over HTTP, workers run them, and a
dashboard shows what is happening live. The headline feature is autoscaling: a scaler process
watches the queue and adds or removes worker containers on its own.

## Goals

- Jobs are never lost, even when workers crash mid-job. Delivery is at least once, so a job
  can run again (see Leases and crashes).
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
├── drivers/           # how the scaler starts and stops workers: docker, noop
├── models/            # plain data types (Job, Team, Plan, ApiKey...)
├── jobs/              # the real job handlers: webhook, image, email
├── migrations/        # SQL migration files
├── web/               # dashboard
├── deploy/            # Dockerfiles, docker-compose.yml
├── scripts/           # demo flood, load test
└── docs/
```

Import direction is one way only: `controllers -> services -> repositories -> models`, with
`drivers` beside `repositories` as the other way services reach outside the process.
`models` imports nothing from the project. This keeps Go's no-import-cycles rule happy.
There is no `utils/` folder; a helper lives next to the code that uses it.

## Data model (first cut)

- `plans`: name, share weight, running jobs cap (`max_workers`), jobs per minute, burst
- `teams`: name, plan_id
- `api_keys`: team_id, prefix (for display), sha256 hash (unique, what a key is looked up
  by), revoked_at
- `jobs`: team_id, type, payload (jsonb), priority, status, attempts, max_attempts,
  run_at, ready_at (when its current wait began, for aging), lease_until, locked_by,
  idempotency_key, progress, last_error, throttled_at (when its type's rate limit last
  pushed it back), error_key (the shape of the last error, set when the job dies), result
  (jsonb, what the handler returned when it succeeded), timestamps
- `job_events`: job_id, kind, message, at (the timeline)
- `schedules`: team_id, cron expression, timezone, job template (type, payload, priority,
  max_attempts), enabled, next_run_at, last_run_at
- `workers`: id, host, state (starting, running or draining), started_at, last_seen_at
  (heartbeat), jobs_done (sent with each heartbeat). There is no current-job column: a
  worker's running jobs are the ones whose `jobs.locked_by` is its id.
- `team_usage`: team_id, served (a decaying count of jobs started), served_at. Used for fair
  share, and grabs for a team take turns on its row. Running jobs are counted, not stored.
- `rate_buckets`: key (`team:<id>` or `type:<name>`), tokens, updated_at
- `scaler_decisions`: at, action (up, down, hold), from_workers, to_workers, reason; the
  latest 200
- `scaler_status`: one row, the status the leading scaler last reported, as JSON

Redis can come back later behind the same service interfaces if load tests show Postgres is
the bottleneck for rate limits or events.

Job statuses: `queued -> running -> succeeded | failed -> dead`. `cancelled` from queued.
`failed` means "this attempt failed and the job is waiting out its backoff"; once `run_at`
passes it is runnable again, just like `queued`. `dead` is the final failure.

## Plans

| Plan       | Share weight | Running jobs cap | Jobs per minute | Burst  |
|------------|-------------:|-----------------:|----------------:|-------:|
| Free       | 1            | 2                | 100             | 200    |
| Pro        | 3            | 6                | 1,000           | 2,000  |
| Enterprise | 10           | 10               | 10,000          | 20,000 |

The running jobs cap is the most jobs a team can have running at once, however many workers
there are; the column is still named `plans.max_workers`. Burst is twice the per-minute rate.

## Core behaviour

**Grabbing a job.** A worker reads the backlog, picks whose turn it is (see Fair share),
and claims a job with `FOR UPDATE SKIP LOCKED`, setting `status = running`,
`lease_until = now() + lease`, and `locked_by = worker id`. Workers wake on Postgres
`LISTEN/NOTIFY` instead of polling hard (a slow poll stays as a fallback), and also look
again whenever one of their jobs finishes.

**Leases and crashes.** A running worker extends its lease every few seconds. If a worker
dies, its lease runs out and a reaper puts the job back to `queued`. No job is lost. The
reaper also deletes worker rows that missed 10 heartbeats in a row.

That makes delivery at least once, not exactly once. A worker that dies after a side effect,
or stalls past its lease, leaves a job the reaper hands to someone else, and it runs again.
Idempotency keys stop duplicate submits, not repeat runs, so handlers should be safe to repeat.
A webhook carries no job id, and its signature changes with each attempt's timestamp, so a
receiver that must act once needs an id of its own in the body to dedupe on.

**Duplicates.** Submits may carry an `Idempotency-Key` header. A unique index on
`(team_id, idempotency_key)` means a repeat returns the original job instead of a new one.
The batch endpoint takes no keys, so a batch sent twice queues its jobs twice.

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
default step of 30s, a priority 0 job that has waited 4.5 minutes ties a fresh 9. The wait
counts from `ready_at`, which moves with `run_at` (a delay, a retry's backoff) except when
a rate limit holds the job back, so a hold doesn't cost the job the priority it has built up.

**Fairness.** Teams take turns, weighted by their plan's share weight. A team at its plan's
running jobs cap is skipped. See Fair share below.

**Scheduling.** Delayed jobs are just `run_at` in the future. Repeating jobs come from
`schedules`. Only one scheduler fires them: whoever holds a Postgres advisory lock. If that
process dies, the lock frees and another takes over. See Scheduling below.

## Fair share

A grab is two queries with a decision in Go between them.

1. **Backlog**, no locks. For every team under its running jobs cap: its weight, its served
   count, and the oldest runnable job at each priority level. That is one probe per level on the
   `(team_id, priority, ready_at, id)` index, so it stays cheap however long the queue is.
   Only jobs in a hold have `ready_at` before `run_at`, so a probe skips at most those.
2. **Pick.** A team's share is its served count divided by its plan weight. The lowest share
   is furthest behind, so teams are tried in that order. Inside a team, the level whose
   oldest job has the highest aged priority wins.
3. **Grab** from that team and level, in one round trip of two statements in one
   transaction. The first locks the team's `team_usage` row. The second locks the oldest job
   at that level with `SKIP LOCKED`, bumps the team's served count only if it has fewer
   running jobs than its cap, and marks the job running only if that bump happened. If the
   level ran dry or the team filled up in the meantime, the next team in line is tried.

Why this holds with many workers grabbing at once:

- `SKIP LOCKED` means no two workers claim the same job and none waits on another's job row.
  Workers that pick the same team and level just take the next oldest job.
- Grabs for the same team queue on its `team_usage` row. In read committed each statement
  takes a fresh snapshot, so the second one, run once the lock is held, counts the jobs every
  earlier grab of the team started. A team can't pass its cap. With the lock and the count in
  one statement, the count would come from before the wait and let several grabs through.
- Running jobs are counted with a partial index on `(team_id) where status = 'running'`,
  a handful of entries per team. Nothing that takes a job out of `running` (a finish, a
  retry, the reaper) touches the team's row, so none of them waits behind the team's grabs.
  An earlier version kept `running` in `team_usage` with a trigger; `docs/benchmarks.md` has
  why it went.
- `served` decays by a factor of e every `FAIR_SHARE_WINDOW` (a minute), so it roughly counts
  the last minute. Old work doesn't count against a team for long, and a team coming back
  from idle catches up for at most about a window.

When every team is busy, always serving the lowest served-per-weight keeps those numbers
level, so served counts follow the 1:3:10 weights. Caps still apply on top: a team whose
fair share is more slots than its cap stops at the cap, and the spare slots go to the others.

Lock order: a grab locks its team's row and then a job row, and skips a job row that is
taken rather than waiting for it. A finish, a retry and the reaper lock only job rows. A
rate limit hold locks its own job row and then the team's, to give back the turn. Nothing
waits on a job row while holding a team row, so nothing can deadlock. The reaper still runs
under an advisory transaction lock, as reapers on every worker would only skip each other's
rows: one reaps, the others skip that tick.

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
   and gets `400`. A request is checked in full first, so one that would fail anyway (an
   unknown type, a batch over 500) spends nothing. Over the limit returns `429` with `Retry-After` (whole seconds until
   enough tokens are back). Submit responses carry `X-RateLimit-Limit` (the bucket size,
   i.e. the burst) and `X-RateLimit-Remaining` (whole tokens left). The check sits in the
   jobs controller rather than a middleware because the cost depends on the batch size.
   It runs before the insert, so a repeated submit that returns the original job through its
   `Idempotency-Key` still spends a token.
   The teams admin list shows `usage.jobs_last_minute` next to the plan's jobs per minute. It
   counts every job the team created in the last minute, scheduled ones included, though those
   spend no tokens.
2. **Outgoing.** `TYPE_RATE_LIMITS` (default `email.send=10/s,webhook=200/s`) gives a type a
   bucket shared by all workers, with a burst of a tenth of a second's worth (at least 1).
   After a grab, a job of a limited type needs a token. Without one it goes straight back to
   `queued` until the next token is due, with its attempt and its team's fair share turn
   given back, its `ready_at` kept, a `throttled` event on its timeline, and `throttled_at`
   set. That worker then
   leaves the type alone until the token is due, so a saturated type doesn't spin.
   `HeldByLimit` counts the waiting jobs of every type throttled in a recent window, which
   is what the scaler should leave out.
3. **Overload.** Once `QUEUE_CEILING` (default 100,000) runnable jobs are waiting, submits
   get `503` with `Retry-After: 5` until it drains. The count is shared by every request and
   refreshed at most once a second, so a flood doesn't turn into a flood of counts.

Scheduled jobs skip the incoming limit and the overload check; only submits pay. What
bounds them instead is `MAX_SCHEDULES_PER_TEAM` (50): at one firing a minute at most, a team
can queue 50 jobs a minute through schedules.

## Scheduling

**Delayed jobs.** A submit (or each job in a batch) may give `run_at` (RFC 3339) or `delay`
(a Go duration such as `90s` or `2h30m`), up to a year out. A time in the past means now,
since a backdated job would jump the queue through aging.

**Repeating jobs.** A schedule has a cron expression (five fields, or a descriptor such as
`@hourly` or `@every 2h`), parsed with robfig/cron's parser, and an IANA time zone (UTC by
default). The API refuses anything firing more than once a minute. Changing a schedule
works out its next run again from now. A team can have `MAX_SCHEDULES_PER_TEAM` (50)
schedules; one more gets `409`. A create locks the team's row while it counts, so
concurrent creates can't pass the cap.

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

`tasklane scaler` keeps the number of worker containers in step with the queue. Every scaler
competes for an advisory lock on a connection of its own, as the schedulers do, and only the
holder acts. It checks that connection before each round, so one that lost its session stops
acting before another takes over.

**Each round** (`SCALER_INTERVAL`, 1s) the leader:

1. Lists its workers through the driver and their rows in `workers`. Containers that exited
   or died are removed, with any row they left; a crashed worker's jobs come back when their
   leases run out. Workers that are draining, whoever told them to stop, don't count. The
   rest, running or still starting, are the current count.
2. Measures demand in one query: runnable jobs, the oldest wait, and slots, which is the sum
   over teams of `min(waiting + running, plan cap)`. Types that a rate limit held back in the
   last 10s (`HeldByLimit`) are left out, as more workers wouldn't start them any sooner.
3. Decides. The target is `ceil(runnable / JOBS_PER_WORKER)`, at most
   `ceil(slots / WORKER_CONCURRENCY)`, clamped to `[SCALER_MIN, SCALER_MAX]` (2 and 10).
   Above the current count, it starts the difference at once. Below it, it starts a cool-down
   (`SCALER_COOLDOWN`, 20s); if it still wants fewer when that ends, it stops one worker every
   `SCALER_STEP_DOWN` (3s) until it doesn't. Wanting as many as it has ends a cool-down, so a
   queue that fills and empties doesn't flap. A cool-down that starts records a `hold`, but only
   the first at each pool size, so that queue doesn't fill the log with the same hold either.
   Times are compared with half a round of slack, or a round that ran a few ms late would push
   a 3s step to 4s.
4. Stores an `up`, `down` or `hold` in `scaler_decisions`, publishes it as a `decision` event,
   and writes its status row, which the overview and the `stats` event carry as `scaler`.
   Once the status is three rounds old, and at least 5s, `scaler` is `null`, so a scaler that
   stopped or crashed shows as not running.

**Why caps bound the target.** A team can't run more jobs at once than its plan's cap, however
many workers there are. With one enterprise team flooding (cap 10) and workers running four
jobs each, three workers fill the cap and seven more would sit idle, so the backlog says how
many workers are wanted and the caps say how many are useful. Compose runs workers with
`WORKER_CONCURRENCY=1`: a plan's running jobs cap is then a number of containers, and each
one the scaler adds is another job running. The maths is the same at the default of four, with
fewer containers.

**`JOBS_PER_WORKER` is 500.** Two workers take the everyday trickle; a 10k flood asks for 20,
gets 10, and as it drains below 4,500 the target falls and the cool-down starts. The pool
then steps down soon after the queue empties rather than a full cool-down later.

**Starting and stopping.** The scaler names a worker `tasklane-worker-<hex>` and adds its row
as `starting` before starting it; the container gets the name as `WORKER_ID` and host name,
and its first heartbeat turns the row `running`. A worker that never comes up is reaped as
stale. To scale down it stops the worker running the fewest jobs, the newest of those, in the
background: SIGTERM makes it drain, and Docker waits `WORKER_SHUTDOWN_GRACE` + 10s before it
kills it. On its own way out the scaler waits for the stops it started.

**Drivers.** The scaler sees workers through three methods, so Docker is one driver among
possible others (a Kubernetes one would scale pods behind the same calls):

```go
type Driver interface {
	List(ctx context.Context) ([]Instance, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
}
```

- `docker` talks to the Engine API (v1.41) on `/var/run/docker.sock` with `net/http`. It needs
  six calls (ping, list by label, create, start, stop, remove), too few to pull in the Docker
  SDK and its dependencies. Workers run the scaler's own image as `worker`, labelled
  `tasklane.role=worker`, on the compose network with the `jobdata` volume, and get the
  scaler's settings named in `WORKER_ENV` (database, SMTP, data dir, concurrency, ...).
- `noop` keeps workers in memory and logs what it would do. It is the default outside compose
  and what the tests use.

**Compose.** There is no fixed worker service. The `scaler` service mounts the Docker socket,
which on Docker Desktop is open to root only, so it runs as root. `make up` brings the stack
up and the scaler starts the two minimum workers; `make down` stops the scaler, removes the
labelled containers, then takes the stack down.

**A 10k flood** of `image.resize` jobs from the default team, switched to enterprise, on an
Apple M4 with Docker Desktop (10 CPUs), timed from the first batch:

| What                                  | When  |
|---------------------------------------|-------|
| 20 batches of 500 submitted           | 0.5s  |
| scaler starts eight workers (2→10)    | 1s    |
| 10 workers running                    | 2.1s  |
| target falls under 10, `hold`         | 13.8s |
| queue drained                         | 26s   |
| cool-down over, first step down       | 33.8s |
| back to 2 workers, one step every 3s  | 56s   |

The same flood on two fixed workers took 45s. Ten workers run five times as many jobs at once
but finish only 1.8x sooner. `docs/benchmarks.md` has the measurements: every grab and finish of
one team used to queue on its `team_usage` row and on the order Postgres commits `NOTIFY`s in,
but most of the gap is the image work itself, which takes about 9ms a job on two workers and
22ms on ten, as ten busy containers spill onto the M4's efficiency cores.

## Live updates

Everything the dashboard shows live travels on one Postgres channel, `tasklane_events`, as
`{"event": name, "data": {...}}`. Payloads hold ids and a few fields, with long text cut
short, far under `NOTIFY`'s 8000-byte limit; Go refuses to send one over 4000.

| Event        | Sent by                                               | Data                                                                                                 |
|--------------|-------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| `job`        | a trigger on `jobs`, on any status or progress change | id, team_id, type, status, progress, attempts, run_at, locked_by, last_error (200 chars), updated_at |
| `worker`     | the scaler's start, a worker's heartbeat, drain, exit | id, host, state (starting, running, draining, stopped), started_at, last_seen_at, jobs_done          |
| `rejected`   | the api that answered a submit with `429`             | team_id, jobs                                                                                        |
| `stats`      | the api leading the scheduler, every second           | the overview snapshot with the scaler's status, and one new sample                                   |
| `team_usage` | the same api, every second, 20 teams per event        | team_id, queued, running, jobs_last_minute, rejected_last_minute, fair_share                         |
| `decision`   | the leading scaler, on each decision                  | id, at, action (up, down, hold), from, to, reason                                                    |
| `reset`      | an api, to its own streams                            | `{}`: refetch everything                                                                             |

- **Jobs.** A trigger covers every path that changes a job (submits, grabs, finishes, holds,
  the reaper, the scheduler), and a write that rolls back announces nothing, as notifications
  go out on commit.
- **Progress.** A handler may report as often as it likes. The worker writes at most one
  report every 500ms, and when the interval ends it writes the latest one it held back, so the
  last value always lands. Each write is one `job` event, so that's about 2/s per job. The first
  interval starts with the job, so one that finishes sooner writes no progress at all; its
  outcome sets it. Most jobs in a flood are that short, and every write is a commit that queues
  with the grabs and finishes (see `docs/benchmarks.md`).
- **Relaying.** Every api `LISTEN`s on a connection of its own and fans out to the browsers
  on `GET /api/events` (SSE), so it doesn't matter which api a browser hits. Each event has an
  `id:`, and a `: ping` comment goes out every 15s so proxies keep the stream open. A stream
  that falls 512 events behind is closed, and the browser reconnects.
- **Catching up.** Each api numbers events `<process>-<n>` and keeps the last 2048. A browser
  that reconnects to the same api with `Last-Event-ID` gets what it missed. An api can't replay
  another's events, so a reconnect that lands elsewhere, or reaches back too far, gets a single
  `reset` and should refetch. An api whose listening connection had to be replaced sends
  `reset` too, as it may have missed events meanwhile.
- **Stats.** Only the api leading the scheduler runs the overview query, once a second. Every
  api relays the result and keeps the last 150 samples, so `GET /api/stats` reads memory; if no
  stats have come for 3s, the request counts for itself. Throughput, per second and per type,
  is counted from `job` events and refusals from `rejected` events, so neither needs a query.
  The counts read partial indexes (queued and failed jobs, today's successes, dead jobs) or
  small tables.
- **Cost.** Every job change is a `NOTIFY`, and Postgres serialises the commits of
  transactions that notify, each holding a cluster-wide lock until its WAL is flushed. That caps
  how fast jobs can change state: empty jobs reach about 1,900 a second on a laptop
  (`docs/benchmarks.md`). Redis pub/sub could take over behind the same service if that is ever
  too few.
- **Auth.** The stream takes the admin token. `EventSource` can't send headers, so this one
  route also takes it as `?token=`; the request log records the path without the query.

## Dead jobs

Dead jobs are grouped by type and the shape of their last error. When a job dies, a trigger
stores `normalize_error(last_error)` in `error_key`: UUIDs become `<id>`, quoted values
`<str>`, email addresses `<email>`, any word with a digit in it `<n>`, cut to 200 characters.
So `connect to 10.0.0.5:443: timeout` and `connect to 10.0.0.9:443: timeout` are one group.
An index on `(type, error_key, finished_at, team_id)` for dead jobs answers the groups page,
the counts and a group's retry without reading the table. A group's id is its type and
pattern in base64url, so retrying one needs no table of groups. A retry requeues the group's
jobs in one statement, with fresh attempts; as an admin action it skips the queue ceiling.

## Job types

A handler gets the payload and a way to report progress, and returns a result or an error:

```go
type Handler func(ctx context.Context, payload json.RawMessage, progress Progress) (result any, err error)
```

- **Result.** A non-nil result is stored as JSON in `result` when the job succeeds, and the job
  page shows it.
- **Errors.** A plain error is retried with backoff until `max_attempts`. `jobs.Permanent(err)`
  marks one that retrying won't fix, such as a bad payload or a 4xx answer, and the job goes
  straight to `dead` with the attempts it has left unused.
- **Timeouts.** Each type has its own. Past it the job's context is cancelled, and the attempt
  fails like any other error.

| Type           | Payload                                                     | Result                            | Timeout |
|----------------|-------------------------------------------------------------|-----------------------------------|--------:|
| `webhook`      | `{"url", "body"}`                                           | `{"status", "body"}`              |     15s |
| `image.resize` | `{"source", "sizes": [320, 640], "format"}`                 | `{"source", "format", "outputs"}` |      1m |
| `email.send`   | `{"to", "subject", "text"}` or `{"to", "template", "data"}` | `{"to", "subject"}`               |     30s |
| `sleep`        | `{"seconds": 5}`                                            | none                              |      5m |
| `flaky`        | `{"fail_rate": 0.5}`                                        | none                              |     30s |

`outputs` lists each file written as `{"path", "width", "height"}`.

**Webhook.** POSTs `body` (`{}` if left out) as JSON to `url`, signed the way Stripe signs
its webhooks: `Tasklane-Signature: t=<unix time>,v1=<hex>`, where `v1` is an HMAC-SHA256 of
`<t>.<body>` keyed with `WEBHOOK_SECRET`. A receiver recomputes it over the raw body and turns
away a `t` that is too old, so a captured request can't be replayed later. Redirects aren't
followed, as one would turn the POST into a GET, so a 3xx is the answer. A 2xx succeeds; a 5xx,
408, 429 or network error is retried; anything else is permanent. Up to 256 bytes of the reply,
made valid UTF-8 without NULs since Postgres refuses those in text, go into the result or the
error. Compose runs httpbin as a target: `http://httpbin:8080/post` echoes, `/status/503` fails.

**image.resize.** An http(s) `source` is fetched, up to 20 MB and 40 megapixels; its header is
read before decoding, so a small file can't claim a huge canvas. Any other source draws a
built-in sample (`sample:gradient`, `sample:checker`, `sample:rings`, or one picked by hashing
the source), so demos and the dashboard's flood need no network. The image is scaled with
Catmull-Rom to each of up to 8 widths (1 to 4096, and never past the source's own, since
enlarging only blurs), keeping its aspect ratio and reporting progress after each. Outputs go to
`DATA_DIR/images/<hash of the source>/w<width>.<format>` through a temp file and a rename, so a
retry or a repeat overwrites the same files and no one ever reads half a file. It reads JPEG,
PNG, GIF and WebP and writes JPEG (quality 85) or PNG; `webp` writes JPEG, as Go's image library
can't encode WebP, and the result's `format` says so. A 4xx fetching the source, or a source
that isn't an image, is permanent; a 5xx or 429 is retried. In compose the workers share the
`jobdata` volume at `/data`.

**email.send.** Plain text over SMTP to `SMTP_HOST:SMTP_PORT`, from `SMTP_FROM`. Compose points
it at Mailpit, a fake inbox at http://localhost:8025, so there is no auth or TLS. The subject and
text come from the payload or a built-in template (`welcome`, `receipt`, `report-ready`) filled
in from `data`; whatever the payload spells out wins, and a key the template needs but `data`
lacks is an error rather than a blank. The subject is Q-encoded and the body quoted-printable,
so any UTF-8 gets through. A bad payload (no valid `to`, an empty subject or text, a line break
in the subject) and a 5xx reply, such as an unknown mailbox, are permanent; anything else is
retried. `net/smtp` knows nothing of contexts, so a cancelled job cuts the connection instead.
`TYPE_RATE_LIMITS` holds the type to 10 a second by default.

## Dashboard pages

- **Overview**: queued / running / done / dead counts, queue depth vs worker count chart,
  throughput chart, "Flood 10k jobs" demo button. The queued count and the queue chart include
  delayed jobs whose `run_at` is still in the future; the stats' `runnable`, like the scaler's
  count, leaves them out.
- **Jobs**: table with filters, live progress bars, bulk retry/cancel
- **Job detail**: payload, result, timeline, attempts
- **Workers**: live list, running jobs, heartbeat age, scaler decisions log
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
GET    /api/plans                              admin
GET    /api/teams                              admin: teams with plan and usage
POST   /api/teams                              admin: create {name, plan}
PUT    /api/teams/{id}/plan                    admin: switch plan {plan}
GET    /api/teams/{id}/keys                    admin
POST   /api/teams/{id}/keys                    admin: returns the full key once
POST   /api/teams/{id}/keys/{key}/revoke       admin
GET    /api/admin/jobs/{id}                    admin: any team's job, detail + timeline
POST   /api/admin/jobs/{id}/cancel             admin: cancel any team's job
POST   /api/admin/jobs/{id}/retry              admin: retry any team's job
GET    /api/stats                              admin: counts, rates and the last 150 samples
GET    /api/workers                            admin: live workers, their jobs, scaler decisions
GET    /api/dead-groups                        admin: dead jobs grouped by type and error
GET    /api/dead-groups/{id}/jobs              admin: a group's latest jobs (?limit=, 5)
POST   /api/dead-groups/{id}/retry             admin: requeue the whole group
GET    /api/events                             admin: SSE stream, also takes ?token=
GET    /healthz
```

## Testing

- Unit tests for backoff, fairness picking, scaler decisions, rate limit math, the progress
  throttle, event encoding and replay.
- Integration tests against a real Postgres (testcontainers): double-grab, lease
  expiry, idempotency, starvation, plan weights, running jobs caps, auth, leader failover,
  events crossing api processes, replay after a reconnect, dead job groups, the scaler's
  loop on the noop driver and its leadership.
- The Docker driver against a fake daemon on a unix socket, and a 10k flood by hand.
- CI runs `go vet`, tests, and the web build on pushes to `main` and on pull requests.

## Build order

1. Skeleton: compose (Postgres), config, migrations, `api` with `/healthz`
2. Jobs: submit, grab, leases, retries, dead, timeouts, clean shutdown
3. Teams, plans, API keys, auth middleware
4. Priorities with aging, fairness, running jobs caps
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
