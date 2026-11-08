package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

func TestStartTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time {
		t := now.Add(d)
		return &t
	}
	tests := []struct {
		name  string
		runAt *time.Time
		delay string
		want  *time.Time
	}{
		{"neither", nil, "", nil},
		{"a delay", nil, "30m", at(30 * time.Minute)},
		{"a run_at", at(2 * time.Hour), "", at(2 * time.Hour)},
		{"a zero delay", nil, "0s", nil},
		{"a run_at in the past", at(-time.Hour), "", nil},
	}
	for _, tt := range tests {
		got, err := startTime(tt.runAt, tt.delay, now)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if (got == nil) != (tt.want == nil) || (got != nil && !got.Equal(*tt.want)) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}

	for name, bad := range map[string]struct {
		runAt *time.Time
		delay string
	}{
		"both":             {at(time.Hour), "1h"},
		"not a duration":   {nil, "tomorrow"},
		"a negative delay": {nil, "-5m"},
		"over a year":      {nil, "9000h"},
		"run_at too far":   {at(400 * 24 * time.Hour), ""},
	} {
		if _, err := startTime(bad.runAt, bad.delay, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestDelayedJobWaitsForItsTime(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	team := defaultTeam(t, pool)
	repo := repositories.NewJobs(pool)
	queue := NewQueue(repo, jobs.Defaults())

	later, _, err := queue.Submit(ctx, team, JobSpec{Type: "sleep", Delay: "1h"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Until(later.RunAt); wait < 59*time.Minute || wait > time.Hour {
		t.Fatalf("a job delayed by 1h has run_at %v from now", wait)
	}
	soonAt := time.Now().Add(300 * time.Millisecond)
	batch, err := queue.SubmitBatch(ctx, team, []JobSpec{{Type: "sleep", RunAt: &soonAt}})
	if err != nil {
		t.Fatal(err)
	}

	types := []string{"sleep"}
	if _, err := grab(ctx, repo, "w1", types, fastConfig()); !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("grab before any job is due: err = %v, want ErrNotFound", err)
	}
	time.Sleep(time.Until(soonAt) + 50*time.Millisecond)
	job, err := grab(ctx, repo, "w1", types, fastConfig())
	if err != nil || job.ID != batch[0].ID {
		t.Fatalf("grab once the batch job is due: job %d, err %v, want job %d", job.ID, err, batch[0].ID)
	}
	if _, err := grab(ctx, repo, "w1", types, fastConfig()); !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("the hour-delayed job was grabbed early: err = %v", err)
	}
}
