package repositories

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

// ErrDuplicate means an insert hit a unique constraint.
var ErrDuplicate = errors.New("duplicate")

const planColumns = `p.id, p.name, p.share_weight, p.max_workers, p.jobs_per_minute, p.burst`

const teamColumns = `t.id, t.name, t.created_at, ` + planColumns

type Teams struct {
	db *pgxpool.Pool
}

func NewTeams(db *pgxpool.Pool) *Teams {
	return &Teams{db: db}
}

func scanTeam(row pgx.Row, extra ...any) (models.Team, error) {
	var t models.Team
	p := &t.Plan
	dest := append([]any{&t.ID, &t.Name, &t.CreatedAt,
		&p.ID, &p.Name, &p.ShareWeight, &p.MaxWorkers, &p.JobsPerMinute, &p.Burst}, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (r *Teams) IDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `select id from teams where name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (r *Teams) Plans(ctx context.Context) ([]models.Plan, error) {
	rows, err := r.db.Query(ctx, `select `+planColumns+` from plans p order by p.share_weight, p.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.Plan])
}

func (r *Teams) PlanByName(ctx context.Context, name string) (models.Plan, error) {
	rows, err := r.db.Query(ctx, `select `+planColumns+` from plans p where p.name = $1`, name)
	if err != nil {
		return models.Plan{}, err
	}
	plan, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[models.Plan])
	if errors.Is(err, pgx.ErrNoRows) {
		return plan, ErrNotFound
	}
	return plan, err
}

// List returns every team with its plan and how many of its jobs are waiting or
// running right now.
func (r *Teams) List(ctx context.Context) ([]models.TeamSummary, error) {
	rows, err := r.db.Query(ctx, `
		select `+teamColumns+`,
			count(j.id) filter (where j.status in ('queued', 'failed')),
			count(j.id) filter (where j.status = 'running')
		from teams t
		join plans p on p.id = t.plan_id
		left join jobs j on j.team_id = t.id and j.status in ('queued', 'failed', 'running')
		group by t.id, p.id
		order by t.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.TeamSummary, error) {
		var s models.TeamSummary
		var err error
		s.Team, err = scanTeam(row, &s.Usage.Queued, &s.Usage.Running)
		return s, err
	})
}

func (r *Teams) Get(ctx context.Context, id int64) (models.Team, error) {
	return scanTeam(r.db.QueryRow(ctx, `
		select `+teamColumns+`
		from teams t join plans p on p.id = t.plan_id
		where t.id = $1`, id))
}

func (r *Teams) Create(ctx context.Context, name string, planID int64) (models.Team, error) {
	team, err := scanTeam(r.db.QueryRow(ctx, `
		with created as (
			insert into teams (name, plan_id) values ($1, $2)
			returning id, name, plan_id, created_at
		), usage as (
			insert into team_usage (team_id) select id from created
		)
		select `+teamColumns+`
		from created t join plans p on p.id = t.plan_id`,
		name, planID))
	if isUniqueViolation(err) {
		return team, ErrDuplicate
	}
	return team, err
}

func (r *Teams) SetPlan(ctx context.Context, id, planID int64) (models.Team, error) {
	return scanTeam(r.db.QueryRow(ctx, `
		with updated as (
			update teams set plan_id = $2 where id = $1
			returning id, name, plan_id, created_at
		)
		select `+teamColumns+`
		from updated t join plans p on p.id = t.plan_id`,
		id, planID))
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
