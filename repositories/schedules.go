package repositories

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

const scheduleColumns = `id, team_id, cron, timezone, type, payload, priority, max_attempts, enabled,
	next_run_at, last_run_at, created_at, updated_at`

type Schedules struct {
	db *pgxpool.Pool
}

func NewSchedules(db *pgxpool.Pool) *Schedules {
	return &Schedules{db: db}
}

func scanSchedule(row pgx.Row) (models.Schedule, error) {
	var s models.Schedule
	err := row.Scan(&s.ID, &s.TeamID, &s.Cron, &s.Timezone, &s.Type, &s.Payload, &s.Priority,
		&s.MaxAttempts, &s.Enabled, &s.NextRunAt, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func collectSchedules(rows pgx.Rows) ([]models.Schedule, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Schedule, error) {
		return scanSchedule(row)
	})
}

func (r *Schedules) Create(ctx context.Context, s models.Schedule) (models.Schedule, error) {
	return scanSchedule(r.db.QueryRow(ctx, `
		insert into schedules (team_id, cron, timezone, type, payload, priority, max_attempts, enabled, next_run_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+scheduleColumns,
		s.TeamID, s.Cron, s.Timezone, s.Type, s.Payload, s.Priority, s.MaxAttempts, s.Enabled, s.NextRunAt))
}

func (r *Schedules) List(ctx context.Context, teamID int64) ([]models.Schedule, error) {
	rows, err := r.db.Query(ctx,
		`select `+scheduleColumns+` from schedules where team_id = $1 order by id`, teamID)
	if err != nil {
		return nil, err
	}
	return collectSchedules(rows)
}

func (r *Schedules) Get(ctx context.Context, teamID, id int64) (models.Schedule, error) {
	return scanSchedule(r.db.QueryRow(ctx,
		`select `+scheduleColumns+` from schedules where id = $1 and team_id = $2`, id, teamID))
}

// Update replaces everything about a schedule but its id, team and history.
func (r *Schedules) Update(ctx context.Context, s models.Schedule) (models.Schedule, error) {
	return scanSchedule(r.db.QueryRow(ctx, `
		update schedules
		set cron = $3, timezone = $4, type = $5, payload = $6, priority = $7, max_attempts = $8,
			enabled = $9, next_run_at = $10, updated_at = now()
		where id = $1 and team_id = $2
		returning `+scheduleColumns,
		s.ID, s.TeamID, s.Cron, s.Timezone, s.Type, s.Payload, s.Priority, s.MaxAttempts, s.Enabled, s.NextRunAt))
}

func (r *Schedules) Delete(ctx context.Context, teamID, id int64) error {
	tag, err := r.db.Exec(ctx, `delete from schedules where id = $1 and team_id = $2`, id, teamID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LeaderConn is the scheduler's own connection. It holds the advisory lock that
// makes its process the one firing schedules, and the firing goes through it too,
// so a process whose connection has died can't fire.
type LeaderConn struct {
	conn *pgx.Conn
}

// LeaderConn takes a connection out of the pool for good. Close it to give up the
// lead.
func (r *Schedules) LeaderConn(ctx context.Context) (*LeaderConn, error) {
	pooled, err := r.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &LeaderConn{conn: pooled.Hijack()}, nil
}

// TryLead takes the scheduler lock unless another session has it. The lock lasts
// as long as the session, which is how it passes on when a process dies.
func (l *LeaderConn) TryLead(ctx context.Context) (bool, error) {
	var ok bool
	err := l.conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, schedulerLock).Scan(&ok)
	return ok, err
}

func (l *LeaderConn) Close() {
	if err := l.conn.Close(context.Background()); err != nil {
		slog.Warn("close the scheduler connection", "err", err)
	}
}

// FireDue fires up to limit due schedules in one transaction, queueing each one's
// job under the key schedule:<id>:<tick> and moving next_run_at on to what next
// returns, or disabling the schedule if next fails. Row locks and the unique key
// make a tick fire once even if two processes briefly both think they lead.
func (l *LeaderConn) FireDue(ctx context.Context, limit int, next func(models.Schedule) (time.Time, error)) (int, error) {
	tx, err := l.conn.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		select `+scheduleColumns+` from schedules
		where enabled and next_run_at <= now()
		order by next_run_at
		limit $1
		for update skip locked`,
		limit)
	if err != nil {
		return 0, err
	}
	due, err := collectSchedules(rows)
	if err != nil || len(due) == 0 {
		return 0, err
	}

	ids := make([]int64, len(due))
	nexts := make([]*time.Time, len(due))
	for i, s := range due {
		ids[i] = s.ID
		if at, err := next(s); err == nil {
			nexts[i] = &at
		}
	}
	if _, err := tx.Exec(ctx, `
		with fired as (
			select s.id, s.team_id, s.type, s.payload, s.priority, s.max_attempts,
				extract(epoch from s.next_run_at)::bigint as tick, u.next
			from unnest($1::bigint[], $2::timestamptz[]) as u (id, next)
			join schedules s on s.id = u.id
		), queued as (
			insert into jobs (team_id, type, payload, priority, max_attempts, idempotency_key)
			select team_id, type, payload, priority, max_attempts, format('schedule:%s:%s', id, tick)
			from fired
			on conflict (team_id, idempotency_key) do nothing
			returning id, idempotency_key
		), logged as (
			insert into job_events (job_id, kind, message)
			select id, 'created', 'queued by ' || idempotency_key from queued
		)
		update schedules s
		set next_run_at = coalesce(f.next, s.next_run_at), enabled = f.next is not null,
			last_run_at = now(), updated_at = now()
		from fired f
		where s.id = f.id`,
		ids, nexts); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if _, err := l.conn.Exec(ctx, `select pg_notify($1, '')`, queuedChannel); err != nil {
		slog.Warn("notify workers", "err", err)
	}
	return len(due), nil
}
