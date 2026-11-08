package repositories

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

// keepDecisions is how many of the scaler's latest decisions are kept.
const keepDecisions = 200

type Scaler struct {
	db *pgxpool.Pool
}

func NewScaler(db *pgxpool.Pool) *Scaler {
	return &Scaler{db: db}
}

// Leader takes a connection out of the pool to compete for the scaler lock.
func (r *Scaler) Leader(ctx context.Context) (*Leader, error) {
	return newLeader(ctx, r.db, scalerLock)
}

// AddDecision stores d, fills in its id and time, and drops the oldest decisions
// past keepDecisions.
func (r *Scaler) AddDecision(ctx context.Context, d models.Decision) (models.Decision, error) {
	err := r.db.QueryRow(ctx, `
		with added as (
			insert into scaler_decisions (action, from_workers, to_workers, reason) values ($1, $2, $3, $4)
			returning id, at
		), trimmed as (
			delete from scaler_decisions where id <= (select id from added) - $5
		)
		select id, at from added`,
		d.Action, d.From, d.To, d.Reason, keepDecisions).Scan(&d.ID, &d.At)
	return d, err
}

// Decisions returns the latest decisions, newest first.
func (r *Scaler) Decisions(ctx context.Context, limit int) ([]models.Decision, error) {
	rows, err := r.db.Query(ctx, `
		select id, at, action, from_workers, to_workers, reason from scaler_decisions
		order by id desc limit $1`,
		limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.Decision])
}

func (r *Scaler) SetStatus(ctx context.Context, s models.ScalerStatus) error {
	_, err := r.db.Exec(ctx, `
		insert into scaler_status (status) values ($1)
		on conflict (id) do update set status = excluded.status, updated_at = now()`,
		s)
	return err
}
