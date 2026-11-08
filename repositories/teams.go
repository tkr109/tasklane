package repositories

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Teams struct {
	db *pgxpool.Pool
}

func NewTeams(db *pgxpool.Pool) *Teams {
	return &Teams{db: db}
}

func (r *Teams) IDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `select id from teams where name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
