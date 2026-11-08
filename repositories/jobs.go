package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

// queuedChannel is where workers hear that a job became runnable.
const queuedChannel = "tasklane_jobs_queued"

const jobColumns = `id, team_id, type, payload, priority, status, attempts, max_attempts, run_at,
	lease_until, coalesce(locked_by, '') as locked_by, coalesce(idempotency_key, '') as idempotency_key,
	progress, coalesce(last_error, '') as last_error, created_at, updated_at, started_at, finished_at`

type Jobs struct {
	db *pgxpool.Pool
}

func NewJobs(db *pgxpool.Pool) *Jobs {
	return &Jobs{db: db}
}

func scanJob(row pgx.Row) (models.Job, error) {
	var j models.Job
	err := row.Scan(&j.ID, &j.TeamID, &j.Type, &j.Payload, &j.Priority, &j.Status, &j.Attempts,
		&j.MaxAttempts, &j.RunAt, &j.LeaseUntil, &j.LockedBy, &j.IdempotencyKey, &j.Progress,
		&j.LastError, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

func collectJobs(rows pgx.Rows) ([]models.Job, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Job, error) {
		return scanJob(row)
	})
}

// Create inserts a job. If the team already used the idempotency key, it returns
// the original job and created is false.
func (r *Jobs) Create(ctx context.Context, nj models.NewJob) (job models.Job, created bool, err error) {
	job, err = scanJob(r.db.QueryRow(ctx, `
		with inserted as (
			insert into jobs (team_id, type, payload, priority, max_attempts, idempotency_key)
			values ($1, $2, $3, $4, $5, nullif($6, ''))
			on conflict (team_id, idempotency_key) do nothing
			returning `+jobColumns+`
		), logged as (
			insert into job_events (job_id, kind) select id, 'created' from inserted
		)
		select * from inserted`,
		nj.TeamID, nj.Type, nj.Payload, nj.Priority, nj.MaxAttempts, nj.IdempotencyKey))
	if err == nil {
		r.wakeWorkers(ctx)
		return job, true, nil
	}
	if !errors.Is(err, ErrNotFound) || nj.IdempotencyKey == "" {
		return job, false, err
	}

	job, err = scanJob(r.db.QueryRow(ctx,
		`select `+jobColumns+` from jobs where team_id = $1 and idempotency_key = $2`,
		nj.TeamID, nj.IdempotencyKey))
	return job, false, err
}

func (r *Jobs) CreateBatch(ctx context.Context, batch []models.NewJob) ([]models.Job, error) {
	var (
		teams       = make([]int64, len(batch))
		types       = make([]string, len(batch))
		payloads    = make([]string, len(batch))
		priorities  = make([]int32, len(batch))
		maxAttempts = make([]int32, len(batch))
	)
	for i, nj := range batch {
		teams[i] = nj.TeamID
		types[i] = nj.Type
		payloads[i] = string(nj.Payload)
		priorities[i] = int32(nj.Priority)
		maxAttempts[i] = int32(nj.MaxAttempts)
	}

	rows, err := r.db.Query(ctx, `
		with inserted as (
			insert into jobs (team_id, type, payload, priority, max_attempts)
			select team_id, type, payload::jsonb, priority, max_attempts
			from unnest($1::bigint[], $2::text[], $3::text[], $4::int[], $5::int[])
				as u (team_id, type, payload, priority, max_attempts)
			returning `+jobColumns+`
		), logged as (
			insert into job_events (job_id, kind) select id, 'created' from inserted
		)
		select * from inserted order by id`,
		teams, types, payloads, priorities, maxAttempts)
	if err != nil {
		return nil, err
	}
	created, err := collectJobs(rows)
	if err == nil {
		r.wakeWorkers(ctx)
	}
	return created, err
}

func (r *Jobs) Get(ctx context.Context, teamID, id int64) (models.Job, error) {
	return scanJob(r.db.QueryRow(ctx,
		`select `+jobColumns+` from jobs where id = $1 and team_id = $2`, id, teamID))
}

// List returns a team's jobs newest first. f.Before is the cursor: only jobs with
// a smaller id are returned.
func (r *Jobs) List(ctx context.Context, teamID int64, f models.JobFilter) ([]models.Job, error) {
	where := []string{"team_id = $1"}
	args := []any{teamID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Type != "" {
		add("type = $%d", f.Type)
	}
	if f.Before > 0 {
		add("id < $%d", f.Before)
	}
	args = append(args, f.Limit)

	query := fmt.Sprintf(`select %s from jobs where %s order by id desc limit $%d`,
		jobColumns, strings.Join(where, " and "), len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return collectJobs(rows)
}

func (r *Jobs) Events(ctx context.Context, jobID int64) ([]models.JobEvent, error) {
	rows, err := r.db.Query(ctx,
		`select id, job_id, kind, message, at from job_events where job_id = $1 order by id`, jobID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.JobEvent])
}

// Cancel stops a job that hasn't started. ErrNotFound covers both a missing job
// and one that is already running or finished.
func (r *Jobs) Cancel(ctx context.Context, teamID, id int64) (models.Job, error) {
	return scanJob(r.db.QueryRow(ctx, `
		with cancelled as (
			update jobs
			set status = 'cancelled', finished_at = now(), updated_at = now()
			where id = $1 and team_id = $2 and status in ('queued', 'failed')
			returning `+jobColumns+`
		), logged as (
			insert into job_events (job_id, kind) select id, 'cancelled' from cancelled
		)
		select * from cancelled`,
		id, teamID))
}

// Retry puts a dead, failed or cancelled job back in the queue with a fresh set
// of attempts.
func (r *Jobs) Retry(ctx context.Context, teamID, id int64) (models.Job, error) {
	job, err := scanJob(r.db.QueryRow(ctx, `
		with retried as (
			update jobs
			set status = 'queued', attempts = 0, progress = 0, run_at = now(),
				finished_at = null, updated_at = now()
			where id = $1 and team_id = $2 and status in ('dead', 'failed', 'cancelled')
			returning `+jobColumns+`
		), logged as (
			insert into job_events (job_id, kind) select id, 'retried' from retried
		)
		select * from retried`,
		id, teamID))
	if err == nil {
		r.wakeWorkers(ctx)
	}
	return job, err
}

// Grab claims the most urgent runnable job of one of the given types for a worker.
// SKIP LOCKED lets many workers grab at once without waiting on, or double
// claiming, the same row. ErrNotFound means there is nothing to do.
func (r *Jobs) Grab(ctx context.Context, workerID string, types []string, lease time.Duration) (models.Job, error) {
	return scanJob(r.db.QueryRow(ctx, `
		with grabbed as (
			update jobs
			set status = 'running', attempts = attempts + 1, locked_by = $1,
				lease_until = now() + make_interval(secs => $3), progress = 0,
				started_at = now(), updated_at = now()
			where id = (
				select id from jobs
				where status in ('queued', 'failed') and run_at <= now() and type = any($2)
				order by priority desc, run_at, id
				limit 1
				for update skip locked
			)
			returning `+jobColumns+`
		), logged as (
			insert into job_events (job_id, kind, message)
			select id, 'started', format('attempt %s on %s', attempts, locked_by) from grabbed
		)
		select * from grabbed`,
		workerID, types, lease.Seconds()))
}

// ExtendLease pushes the lease out again. It returns false when the worker no
// longer holds the job, for example because the reaper took it back.
func (r *Jobs) ExtendLease(ctx context.Context, id int64, workerID string, lease time.Duration) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		update jobs set lease_until = now() + make_interval(secs => $3)
		where id = $1 and locked_by = $2 and status = 'running'`,
		id, workerID, lease.Seconds())
	return tag.RowsAffected() == 1, err
}

func (r *Jobs) SetProgress(ctx context.Context, id int64, workerID string, percent int) error {
	_, err := r.db.Exec(ctx, `
		update jobs set progress = $3, updated_at = now()
		where id = $1 and locked_by = $2 and status = 'running'`,
		id, workerID, percent)
	return err
}

// Succeed, ScheduleRetry and MarkDead only touch a job the worker still holds, and
// return ErrNotFound when it doesn't.
func (r *Jobs) Succeed(ctx context.Context, id int64, workerID string) error {
	return r.finish(ctx, `
		with done as (
			update jobs
			set status = 'succeeded', progress = 100, last_error = null, lease_until = null,
				locked_by = null, finished_at = now(), updated_at = now()
			where id = $1 and locked_by = $2 and status = 'running'
			returning id
		)
		insert into job_events (job_id, kind) select id, 'succeeded' from done`,
		id, workerID)
}

func (r *Jobs) ScheduleRetry(ctx context.Context, id int64, workerID string, delay time.Duration, errMsg string) error {
	return r.finish(ctx, `
		with failed as (
			update jobs
			set status = 'failed', run_at = now() + make_interval(secs => $3), last_error = $4,
				lease_until = null, locked_by = null, updated_at = now()
			where id = $1 and locked_by = $2 and status = 'running'
			returning id
		)
		insert into job_events (job_id, kind, message) select id, 'failed', $4 from failed`,
		id, workerID, delay.Seconds(), errMsg)
}

func (r *Jobs) MarkDead(ctx context.Context, id int64, workerID string, errMsg string) error {
	return r.finish(ctx, `
		with dead as (
			update jobs
			set status = 'dead', last_error = $3, lease_until = null, locked_by = null,
				finished_at = now(), updated_at = now()
			where id = $1 and locked_by = $2 and status = 'running'
			returning id
		)
		insert into job_events (job_id, kind, message) select id, 'dead', $3 from dead`,
		id, workerID, errMsg)
}

func (r *Jobs) finish(ctx context.Context, query string, args ...any) error {
	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReapExpired takes back running jobs whose lease ran out because their worker
// died or stalled. They return to the queue, or go dead if that was the last try.
func (r *Jobs) ReapExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		with expired as (
			select id, locked_by from jobs
			where status = 'running' and lease_until < now()
			for update skip locked
		), reaped as (
			update jobs j
			set status = case when j.attempts >= j.max_attempts then 'dead' else 'queued' end,
				finished_at = case when j.attempts >= j.max_attempts then now() end,
				last_error = 'lease expired', lease_until = null, locked_by = null, updated_at = now()
			from expired
			where j.id = expired.id
			returning j.id, j.status, expired.locked_by
		)
		insert into job_events (job_id, kind, message)
		select id, 'lease_expired', format('%s stopped renewing the lease, job is %s', locked_by, status)
		from reaped`)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() > 0 {
		r.wakeWorkers(ctx)
	}
	return tag.RowsAffected(), nil
}

// wakeWorkers runs after the write has committed, so a woken worker can see the
// job. A lost notification only costs a poll interval, hence no error.
func (r *Jobs) wakeWorkers(ctx context.Context) {
	if _, err := r.db.Exec(ctx, `select pg_notify($1, '')`, queuedChannel); err != nil {
		slog.Warn("notify workers", "err", err)
	}
}

// ListenQueued calls wake whenever a job becomes runnable. It holds one connection
// until ctx ends or the connection fails.
func (r *Jobs) ListenQueued(ctx context.Context, wake func()) error {
	pooled, err := r.db.Acquire(ctx)
	if err != nil {
		return err
	}
	// Take the connection out of the pool for good: it stays subscribed to the channel.
	conn := pooled.Hijack()
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "listen "+queuedChannel); err != nil {
		return err
	}
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return err
		}
		wake()
	}
}
