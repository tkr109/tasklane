package services

import (
	"context"
	"sync"
	"testing"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/repositories"
)

func TestIdempotencyKeyReturnsTheOriginalJob(t *testing.T) {
	pool := testDB(t)
	team := defaultTeam(t, pool)
	queue := NewQueue(repositories.NewJobs(pool), jobs.Defaults())
	ctx := context.Background()
	spec := JobSpec{Type: "sleep"}

	first, created, err := queue.Submit(ctx, team, spec, "order-42")
	if err != nil || !created {
		t.Fatalf("first submit: created=%v err=%v", created, err)
	}

	const callers = 10
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		recreated int
	)
	for range callers {
		wg.Go(func() {
			job, created, err := queue.Submit(ctx, team, spec, "order-42")
			if err != nil {
				t.Errorf("repeat submit: %v", err)
				return
			}
			if job.ID != first.ID {
				t.Errorf("repeat submit returned job %d, want %d", job.ID, first.ID)
			}
			if created {
				mu.Lock()
				recreated++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if recreated != 0 {
		t.Errorf("%d repeat submits created a job", recreated)
	}

	other, created, err := queue.Submit(ctx, team, spec, "order-43")
	if err != nil || !created || other.ID == first.ID {
		t.Fatalf("a new key should create a new job: id=%d created=%v err=%v", other.ID, created, err)
	}

	var count int
	if err := pool.QueryRow(ctx, `select count(*) from jobs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("got %d jobs, want 2", count)
	}
}
