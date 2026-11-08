package repositories

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/models"
)

const keyColumns = `id, team_id, prefix, created_at, revoked_at`

type Keys struct {
	db *pgxpool.Pool
}

func NewKeys(db *pgxpool.Pool) *Keys {
	return &Keys{db: db}
}

func scanKey(row pgx.Row) (models.APIKey, error) {
	var k models.APIKey
	err := row.Scan(&k.ID, &k.TeamID, &k.Prefix, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

func (r *Keys) Create(ctx context.Context, teamID int64, prefix string, hash []byte) (models.APIKey, error) {
	key, err := scanKey(r.db.QueryRow(ctx, `
		insert into api_keys (team_id, prefix, hash) values ($1, $2, $3)
		returning `+keyColumns,
		teamID, prefix, hash))
	if isUniqueViolation(err) {
		return key, ErrDuplicate
	}
	return key, err
}

// Ensure makes the key a live key of the team: it adds it, or takes back a revoke
// if it is already stored.
func (r *Keys) Ensure(ctx context.Context, teamID int64, prefix string, hash []byte) error {
	_, err := r.db.Exec(ctx, `
		insert into api_keys (team_id, prefix, hash) values ($1, $2, $3)
		on conflict (hash) do update set team_id = excluded.team_id, revoked_at = null`,
		teamID, prefix, hash)
	return err
}

func (r *Keys) List(ctx context.Context, teamID int64) ([]models.APIKey, error) {
	rows, err := r.db.Query(ctx,
		`select `+keyColumns+` from api_keys where team_id = $1 order by id`, teamID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.APIKey, error) {
		return scanKey(row)
	})
}

// Revoke is idempotent: revoking a revoked key keeps its first revoked_at.
func (r *Keys) Revoke(ctx context.Context, teamID, id int64) (models.APIKey, error) {
	return scanKey(r.db.QueryRow(ctx, `
		update api_keys set revoked_at = coalesce(revoked_at, now())
		where id = $1 and team_id = $2
		returning `+keyColumns,
		id, teamID))
}

// TeamByHash returns the team that owns a live key with this hash.
func (r *Keys) TeamByHash(ctx context.Context, hash []byte) (models.Team, error) {
	return scanTeam(r.db.QueryRow(ctx, `
		select `+teamColumns+`
		from api_keys k
		join teams t on t.id = k.team_id
		join plans p on p.id = t.plan_id
		where k.hash = $1 and k.revoked_at is null`,
		hash))
}
