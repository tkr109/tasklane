package repositories

import (
	"context"
	"errors"

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
