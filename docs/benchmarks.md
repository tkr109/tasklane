# Benchmarks

Why a 10k `image.resize` flood ran only 1.8x faster on ten workers than on two, what the
database was waiting on, the two changes that came out of it, and what is still unmeasured.

## Setup

- **Machine.** Apple M4 (4 performance and 6 efficiency cores), 16 GB, Docker Desktop 28.3.2
  with a 10 CPU, 8 GB VM. Postgres 16 from compose with its defaults (`fsync` and
  `synchronous_commit` on).
- **Stack.** `make up`: one api, the scaler with the Docker driver, workers running one job each
  (`WORKER_CONCURRENCY=1`). Two fixed workers is `SCALER_MIN=2 SCALER_MAX=2 make up`.
- **Each run** starts from a fresh stack (`make down`, volumes removed, `make up`, 10s to
  settle), then runs `go run ./scripts/bench -jobs 10000 -teams N`. The bench puts `default`
  and up to two more teams on Enterprise (cap 10, burst 20,000), submits 500 jobs a batch, four
  batches at a time, and waits for the queue to drain. A fresh stack matters for the three team
  runs: a team that has just had a flood is behind on fair share, so a reused stack serves the
  other teams first.
- **Jobs.** `image.resize` with the dashboard's payload (the 640x480 built-in sample to widths 320
  and 640 as JPEG), and an empty job (`sleep` for 1ns) to see the queue's own ceiling.
- **Measures.** Throughput is jobs over the time from the first job created to the last one
  finished. Queue wait is `started_at - created_at`. The bench samples `pg_stat_activity` every
  100ms and counts what busy connections wait on, and reads `pg_stat_wal` for WAL flushes.
  Profiling runs also loaded `pg_stat_statements`.

## Where the time went

The first run, before any change, one team, autoscaled to ten:

```
10000 image.resize jobs from 1 team(s), submitted in 0.4s
drained in 29.7s: 337 jobs/s, up to 10 workers
busy connections, 297 samples 100ms apart, 4.7 busy on average:
  Lock:transactionid            29.7%
  CPU                           24.4%
  Lock:tuple                    16.3%
  Lock:object                   13.3%
  IO:WALSync                    11.4%
```

- `Lock:transactionid` and `Lock:tuple` are waits for a row someone else has locked. Grabs use
  `SKIP LOCKED` and never wait on a job row, so this was the team's `team_usage` row.
- `Lock:object` is Postgres serialising transactions that `NOTIFY`: one holds a cluster-wide
  lock from the start of its commit until its WAL is flushed, so they commit one at a time and
  can't share a flush.
- `pg_stat_statements` put numbers on it: a grab took 3.2ms and a finish 3.3ms, and 2.97ms of the
  finish was the `track_running` trigger's `update team_usage set running = running - 1`, waiting
  for the row.

So each job took its team's row twice, once in the grab (the served count and `running + 1`) and
once in the finish (`running - 1`), and each held it until its commit had flushed, behind
whichever notifying commit was ahead of it. A third commit per job, the first progress report,
didn't touch the row but queued for the `NOTIFY` lock with the rest. The backlog query took
0.11ms and the grab, less its wait, a fraction of a millisecond, so query plans weren't the
problem; I didn't get to `EXPLAIN` them.

Three teams spread the row waits over three rows, and the run got only a little faster (397
jobs/s, on a reused stack): `Lock:object` doubled to 27%. The row was the first limit and the
commit order right behind it.

## The changes

**1. Short jobs write no progress** (`ecd71cd`). The first report used to be written at once, so
even a 20ms job wrote one. Now the first write also waits out the 500ms interval, and a job done
sooner lets its outcome set the progress. That is one notifying commit fewer per job: WAL flushes
went from 2.9 to 1.9 per job, and `Lock:object` from 13 to 15% of waits to about 1% in the one
team run.

**2. Running jobs are counted, not kept** (`61d2928`). The `running` column and its trigger are
gone. A grab sends two statements in one transaction in one round trip: the first locks the team's
`team_usage` row, the second counts the team's running jobs through a partial index on
`(team_id) where status = 'running'` and grabs only under the cap. In read committed the second
statement takes a fresh snapshot after the lock is held, so it sees every earlier grab of the
team and the cap holds; with lock and count in one statement the count is from before the wait,
and a test with 30 racing grabs on a cap of 2 let 8 to 10 through. Finishes, retries and the
reaper no longer touch the team row at all. With `pg_stat_statements`, a finish went from 3.3ms to
0.25ms; grabs still take turns on the row (2.25ms waiting each), but once per job, not twice.

## Results

Single runs. Repeats of the same run varied by up to about 10%, so a difference smaller than that
says nothing.

**Profiling session** (`pg_stat_statements` loaded, fresh stack per run unless noted):

| Code                    | Jobs           | Teams | Workers | Jobs/s | WAL flushes per job |
|-------------------------|----------------|------:|--------:|-------:|--------------------:|
| before                  | image.resize   |     1 |   2→10  |    337 |                   — |
| before                  | image.resize   |     3 |   2→10  |   397¹ |                   — |
| before                  | image.resize   |     1 |   2→10  |    357 |                   — |
| progress only           | image.resize   |     1 |   2→10  |    385 |                   — |
| both changes            | image.resize   |     1 |   2→10  |    426 |                   — |
| both changes            | image.resize   |     3 |   2→10  |    403 |                   — |
| both changes            | empty          |     1 |   2→10  |  1,939 |                   — |

¹ On the stack the run above it had used.

**Matrix** (no `pg_stat_statements`, fresh stack per run):

| Code          | Jobs         | Teams | Workers | Jobs/s | Queue wait p50 / p95 | WAL flushes per job |
|---------------|--------------|------:|--------:|-------:|---------------------:|--------------------:|
| before        | image.resize |     1 |       2 |    208 |        23.1s / 45.2s |                2.94 |
| before        | image.resize |     3 |       2 |    197 |        24.5s / 48.1s |                2.91 |
| before        | image.resize |     3 |   2→10  |    365 |        13.9s / 25.9s |                2.91 |
| before        | empty        |     1 |   2→10  |  1,279 |          4.1s / 7.2s |                2.71 |
| before        | empty        |     3 |   2→10  |  1,429 |          3.7s / 6.5s |                2.69 |
| progress only | image.resize |     1 |   2→10  |    344 |        14.0s / 26.8s |                1.94 |
| progress only | image.resize |     3 |   2→10  |    339 |        14.8s / 27.8s |                1.94 |
| progress only | empty        |     1 |   2→10  |  1,413 |          3.8s / 6.4s |                1.83 |

The matrix was meant to run every case twice for before, progress only and both changes. It was
cut short: the Mac's disk filled while it ran, and Docker Desktop stopped its VM. The four runs
of both changes that finished as the disk was filling (334, 294 and 112 jobs/s for image floods,
1,162 for empty jobs) are left out. Every number in them moved the wrong way at once, including
the image jobs' own run time (22 to 28ms on ten workers, 8.8 to 14.8ms on two), which neither
change touches but which writes two files per job to a nearly full disk. They need running again
before they can count either way.

## What the numbers say

- **The image flood is mostly bound by the image work.** On two workers an image job takes about
  9ms from grab to finish; on ten it takes about 22ms, on the same code. Ten busy containers
  spread onto the M4's efficiency cores and share ten CPUs with Postgres and the api; workers
  used about 500 to 580% CPU in these runs. That, more than the database, is why five times the
  workers gave 1.8x. The database share of a job was real but smaller: before the changes about
  4ms per job sat between a finish and the next grab on ten workers (the finish's wait for its
  team is in there), against under 1ms on two.
- **The queue itself has headroom.** Empty jobs moved at 1,280 to 1,430 jobs/s before the changes
  and 1,939 after, in single runs from different sessions. With empty jobs, waits for the team row
  are again most of what the database does, now from grabs only.
- **The changes do what they were for,** going by what they target directly: a finish no longer
  waits for its team (3.3ms to 0.25ms), and every job makes one write fewer (2.9 to 1.9 WAL
  flushes). On image throughput the effect is inside run to run noise in some runs and +19% in
  the best paired one (357 to 426 jobs/s, one team, same session); a clean repeated comparison is
  still owed.
- **Three teams don't beat one** by much in any version, because with the row out of the way the
  global order of notifying commits and the job CPU are next in line.

## Not done yet

- Rerun the matrix on a machine with free disk: before (`02e1782`) against both changes, two
  fixed and autoscaled, one and three teams, each twice, plus ten fixed workers with empty jobs so
  the scaler's ramp isn't part of a 7s run.
- If the empty job ceiling matters, the next step is fewer notifying commits per job: finishing a
  job and grabbing the next in one transaction, or sending job events outside the commit.
