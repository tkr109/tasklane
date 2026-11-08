package repositories

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Workers struct {
	db *pgxpool.Pool
}

func NewWorkers(db *pgxpool.Pool) *Workers {
	return &Workers{db: db}
}

// Heartbeat records that the worker is alive, creating its row on the first call.
func (r *Workers) Heartbeat(ctx context.Context, id, host string) error {
	_, err := r.db.Exec(ctx, `
		insert into workers (id, host) values ($1, $2)
		on conflict (id) do update set last_seen_at = now()`,
		id, host)
	return err
}

func (r *Workers) Remove(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `delete from workers where id = $1`, id)
	return err
}

func (r *Workers) RemoveStale(ctx context.Context, silentFor time.Duration) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`delete from workers where last_seen_at < now() - make_interval(secs => $1)`,
		silentFor.Seconds())
	return tag.RowsAffected(), err
}
