package repositories

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// refilled is a bucket's tokens topped up for the time since it was last spent
// from. $2 is the rate in tokens a second and $3 the burst.
const refilled = `least($3::float8, b.tokens + $2::float8 * extract(epoch from now() - b.updated_at)::float8)`

type RateBuckets struct {
	db *pgxpool.Pool
}

func NewRateBuckets(db *pgxpool.Pool) *RateBuckets {
	return &RateBuckets{db: db}
}

// Take spends cost tokens from the bucket at key if it holds that many, refill
// included. A new bucket starts full. ok says whether the tokens were spent, and
// left is what the bucket holds afterwards.
func (r *RateBuckets) Take(ctx context.Context, key string, rate float64, burst, cost int) (ok bool, left float64, err error) {
	err = r.db.QueryRow(ctx, `
		with spent as (
			insert into rate_buckets as b (key, tokens) values ($1, $3::float8 - $4::float8)
			on conflict (key) do update
			set tokens = `+refilled+` - $4::float8, updated_at = now()
			where `+refilled+` >= $4::float8
			returning b.tokens
		)
		select true, tokens from spent
		union all
		select false, `+refilled+` from rate_buckets b
		where b.key = $1 and not exists (select 1 from spent)`,
		key, rate, float64(burst), float64(cost)).Scan(&ok, &left)
	// A bucket another caller created after this statement's snapshot, and that
	// is already too low: there is nothing to read back, but it is still a no.
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	return ok, left, err
}
