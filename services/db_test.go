package services

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/tkr109/tasklane/repositories"
)

// One Postgres container is shared by every integration test in the package.
var (
	pgOnce      sync.Once
	pgContainer *postgres.PostgresContainer
	pgPool      *pgxpool.Pool
	pgErr       error
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	code := m.Run()
	if pgPool != nil {
		pgPool.Close()
	}
	if pgContainer != nil {
		_ = testcontainers.TerminateContainer(pgContainer)
	}
	os.Exit(code)
}

// testDB returns a migrated database with no jobs, no workers and only the
// default team. It skips the test when Docker isn't available or -short is set.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	pgOnce.Do(func() {
		ctx := context.Background()
		pgContainer, pgErr = postgres.Run(ctx, "postgres:16-alpine", postgres.BasicWaitStrategies())
		if pgErr != nil {
			return
		}
		url, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = err
			return
		}
		if pgPool, pgErr = repositories.Connect(ctx, url); pgErr != nil {
			return
		}
		pgErr = repositories.Migrate(ctx, pgPool)
	})
	if pgErr != nil {
		t.Fatalf("start postgres: %v", pgErr)
	}

	// Truncating skips the trigger that counts running jobs, so reset the counts too.
	if _, err := pgPool.Exec(context.Background(), `
		truncate jobs, job_events, workers, rate_buckets, schedules, scaler_decisions, scaler_status restart identity;
		delete from teams where name <> 'default';
		update team_usage set served = 0, served_at = now()`); err != nil {
		t.Fatalf("reset tables: %v", err)
	}
	return pgPool
}

func defaultTeam(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	id, err := repositories.NewTeams(pool).IDByName(context.Background(), "default")
	if err != nil {
		t.Fatalf("default team: %v", err)
	}
	return id
}

func waitUntil(t *testing.T, timeout time.Duration, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
