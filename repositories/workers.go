package repositories

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

// announceWorkers ends a statement whose CTE "changed" returns worker rows, and
// sends each one as a worker event.
const announceWorkers = `
	select pg_notify('` + eventsChannel + `', jsonb_build_object('event', 'worker', 'data', to_jsonb(changed))::text)
	from changed`

const workerColumns = `id, host, state, started_at, last_seen_at, jobs_done`

type Workers struct {
	db *pgxpool.Pool
}

func NewWorkers(db *pgxpool.Pool) *Workers {
	return &Workers{db: db}
}

// Heartbeat records that the worker is alive and how many jobs it has finished,
// creating its row on the first call.
func (r *Workers) Heartbeat(ctx context.Context, id, host string, jobsDone int64) error {
	_, err := r.db.Exec(ctx, `
		with changed as (
			insert into workers (id, host, jobs_done) values ($1, $2, $3)
			on conflict (id) do update set last_seen_at = now(), jobs_done = excluded.jobs_done
			returning `+workerColumns+`
		)`+announceWorkers,
		id, host, jobsDone)
	return err
}

// Drain marks a worker that has stopped taking jobs and is finishing the ones it has.
func (r *Workers) Drain(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `
		with changed as (
			update workers set state = 'draining', last_seen_at = now() where id = $1
			returning `+workerColumns+`
		)`+announceWorkers,
		id)
	return err
}

// Remove takes a stopping worker off the list. Its last event carries the final
// count of jobs done, which may be ahead of its last heartbeat.
func (r *Workers) Remove(ctx context.Context, id string, jobsDone int64) error {
	_, err := r.db.Exec(ctx, `
		with changed as (
			delete from workers where id = $1
			returning id, host, 'stopped' as state, started_at, last_seen_at, $2::bigint as jobs_done
		)`+announceWorkers,
		id, jobsDone)
	return err
}

func (r *Workers) RemoveStale(ctx context.Context, silentFor time.Duration) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `
		with changed as (
			delete from workers where last_seen_at < now() - make_interval(secs => $1)
			returning id, host, 'stopped' as state, started_at, last_seen_at, jobs_done
		), announced as (`+announceWorkers+`
		)
		select count(*) from announced`,
		silentFor.Seconds()).Scan(&n)
	return n, err
}

// List returns every worker with the jobs it is running, oldest worker first.
func (r *Workers) List(ctx context.Context) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, `
		select w.id, w.host, w.state, w.started_at, w.last_seen_at, w.jobs_done,
			coalesce(jsonb_agg(jsonb_build_object('id', j.id, 'type', j.type, 'progress', j.progress)
				order by j.id) filter (where j.id is not null), '[]')
		from workers w
		left join jobs j on j.locked_by = w.id and j.status = 'running'
		group by w.id
		order by w.started_at, w.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.Worker])
}
