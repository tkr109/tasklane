package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/tkr109/tasklane/migrations"
)

// ErrNotFound means no row matched, either because it doesn't exist or because
// it wasn't in a state the query accepts.
var ErrNotFound = errors.New("not found")

// Keys of the advisory locks that make sure only one process does a job at a time.
const (
	reaperLock    int64 = 7_400_001
	schedulerLock int64 = 7_400_002
	scalerLock    int64 = 7_400_003
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// Migrate brings the schema up to date. Every process calls it on startup, so the
// session lock keeps two of them from migrating at the same time.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	for _, r := range results {
		slog.Info("applied migration", "version", r.Source.Version, "duration", r.Duration)
	}
	return nil
}

// listen subscribes a connection of its own to channel and calls handle with each
// notification's payload until ctx ends or the connection fails. ready, if set,
// runs once the subscription is in place.
func listen(ctx context.Context, db *pgxpool.Pool, channel string, ready func(), handle func(payload string)) error {
	pooled, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	// Take the connection out of the pool for good: it stays subscribed to the channel.
	conn := pooled.Hijack()
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "listen "+channel); err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		handle(n.Payload)
	}
}

// Leader is a connection of its own that competes for one advisory lock. A
// session lock lasts as long as the session, which is how the lead passes on when
// a process dies.
type Leader struct {
	conn *pgx.Conn
	key  int64
}

func newLeader(ctx context.Context, db *pgxpool.Pool, key int64) (*Leader, error) {
	pooled, err := db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &Leader{conn: pooled.Hijack(), key: key}, nil
}

// TryLead takes the lock unless another session has it.
func (l *Leader) TryLead(ctx context.Context) (bool, error) {
	var ok bool
	err := l.conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, l.key).Scan(&ok)
	return ok, err
}

// Alive checks the connection still works, and so that a lock taken on it is
// still held.
func (l *Leader) Alive(ctx context.Context) error {
	return l.conn.Ping(ctx)
}

func (l *Leader) Close() {
	if err := l.conn.Close(context.Background()); err != nil {
		slog.Warn("close a leader connection", "err", err)
	}
}
