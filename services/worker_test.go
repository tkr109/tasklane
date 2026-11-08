package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

func fastConfig() config.Worker {
	return config.Worker{
		Concurrency:       4,
		Lease:             2 * time.Second,
		PollInterval:      50 * time.Millisecond,
		ReapInterval:      100 * time.Millisecond,
		HeartbeatInterval: 100 * time.Millisecond,
		ShutdownGrace:     2 * time.Second,
		RetryBaseDelay:    10 * time.Millisecond,
		RetryMaxDelay:     50 * time.Millisecond,
		AgingStep:         30 * time.Second,
		FairShareWindow:   time.Minute,
	}
}

func startWorker(t *testing.T, pool *pgxpool.Pool, types jobs.Registry) {
	t.Helper()
	w := NewWorker(repositories.NewJobs(pool), repositories.NewWorkers(pool), types, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("worker: %v", err)
		}
	})
}

func jobStatus(t *testing.T, pool *pgxpool.Pool, id int64) models.Job {
	t.Helper()
	job, err := repositories.NewJobs(pool).Get(context.Background(), defaultTeam(t, pool), id)
	if err != nil {
		t.Fatalf("get job %d: %v", id, err)
	}
	return job
}

func countEvents(t *testing.T, pool *pgxpool.Pool, kind string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from job_events where kind = $1`, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTwoWorkersNeverRunTheSameJob(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	var (
		mu   sync.Mutex
		runs = map[int]int{}
	)
	types := jobs.Registry{"count": {Timeout: 5 * time.Second, Handler: func(ctx context.Context, payload json.RawMessage, _ jobs.Progress) error {
		var p struct{ N int }
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		mu.Lock()
		runs[p.N]++
		mu.Unlock()
		time.Sleep(time.Millisecond)
		return nil
	}}}

	const total = 300
	specs := make([]JobSpec, total)
	for i := range specs {
		specs[i] = JobSpec{Type: "count", Payload: json.RawMessage(fmt.Sprintf(`{"n": %d}`, i))}
	}
	if _, err := NewQueue(repositories.NewJobs(pool), types).SubmitBatch(ctx, defaultTeam(t, pool), specs); err != nil {
		t.Fatal(err)
	}

	startWorker(t, pool, types)
	startWorker(t, pool, types)

	waitUntil(t, 20*time.Second, "all jobs to succeed", func() bool {
		var n int
		_ = pool.QueryRow(ctx, `select count(*) from jobs where status = 'succeeded'`).Scan(&n)
		return n == total
	})

	mu.Lock()
	defer mu.Unlock()
	if len(runs) != total {
		t.Fatalf("%d distinct jobs ran, want %d", len(runs), total)
	}
	for n, times := range runs {
		if times != 1 {
			t.Errorf("job %d ran %d times", n, times)
		}
	}
	if got := countEvents(t, pool, "started"); got != total {
		t.Errorf("%d started events, want %d", got, total)
	}
}

func TestExpiredLeaseIsRequeued(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	repo := repositories.NewJobs(pool)
	types := jobs.Registry{"noop": {Timeout: time.Second, Handler: func(context.Context, json.RawMessage, jobs.Progress) error {
		return nil
	}}}

	submitted, _, err := NewQueue(repo, types).Submit(ctx, defaultTeam(t, pool), JobSpec{Type: "noop"}, "")
	if err != nil {
		t.Fatal(err)
	}

	// A worker grabs the job and then dies without ever renewing its lease.
	cfg := fastConfig()
	cfg.Lease = 50 * time.Millisecond
	if _, err := grab(ctx, repo, "doomed-worker", []string{"noop"}, cfg); err != nil {
		t.Fatalf("grab: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	reaped, err := repo.ReapExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reaped != 1 {
		t.Fatalf("reaped %d jobs, want 1", reaped)
	}
	job := jobStatus(t, pool, submitted.ID)
	if job.Status != models.StatusQueued || job.LockedBy != "" || job.Attempts != 1 {
		t.Fatalf("after reaping: status=%s locked_by=%q attempts=%d", job.Status, job.LockedBy, job.Attempts)
	}
	if held, _ := repo.ExtendLease(ctx, submitted.ID, "doomed-worker", time.Minute); held {
		t.Fatal("the dead worker could still renew a lease it lost")
	}

	startWorker(t, pool, types)
	waitUntil(t, 5*time.Second, "the requeued job to succeed", func() bool {
		return jobStatus(t, pool, submitted.ID).Status == models.StatusSucceeded
	})
}

func TestJobGoesDeadAfterMaxAttempts(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	types := jobs.Registry{"boom": {Timeout: time.Second, Handler: func(context.Context, json.RawMessage, jobs.Progress) error {
		return errors.New("boom")
	}}}

	maxAttempts := 3
	submitted, _, err := NewQueue(repositories.NewJobs(pool), types).
		Submit(ctx, defaultTeam(t, pool), JobSpec{Type: "boom", MaxAttempts: &maxAttempts}, "")
	if err != nil {
		t.Fatal(err)
	}

	startWorker(t, pool, types)
	waitUntil(t, 10*time.Second, "the job to die", func() bool {
		return jobStatus(t, pool, submitted.ID).Status == models.StatusDead
	})

	job := jobStatus(t, pool, submitted.ID)
	if job.Attempts != maxAttempts || job.LastError != "boom" || job.FinishedAt == nil {
		t.Fatalf("dead job: attempts=%d last_error=%q finished_at=%v", job.Attempts, job.LastError, job.FinishedAt)
	}
	if got := countEvents(t, pool, "failed"); got != maxAttempts-1 {
		t.Errorf("%d failed events, want %d", got, maxAttempts-1)
	}
	if got := countEvents(t, pool, "dead"); got != 1 {
		t.Errorf("%d dead events, want 1", got)
	}
}

func TestTimeoutCountsAsFailure(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	types := jobs.Registry{"hang": {Timeout: 100 * time.Millisecond, Handler: func(ctx context.Context, _ json.RawMessage, _ jobs.Progress) error {
		<-ctx.Done()
		return ctx.Err()
	}}}

	once := 1
	submitted, _, err := NewQueue(repositories.NewJobs(pool), types).
		Submit(ctx, defaultTeam(t, pool), JobSpec{Type: "hang", MaxAttempts: &once}, "")
	if err != nil {
		t.Fatal(err)
	}

	startWorker(t, pool, types)
	waitUntil(t, 5*time.Second, "the job to time out", func() bool {
		return jobStatus(t, pool, submitted.ID).Status == models.StatusDead
	})
	if got := jobStatus(t, pool, submitted.ID).LastError; got != "timed out after 100ms" {
		t.Fatalf("last_error = %q", got)
	}
}
