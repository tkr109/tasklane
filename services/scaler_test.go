package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/drivers"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

func fastScalerConfig() config.Scaler {
	return config.Scaler{
		Min:           1,
		Max:           4,
		JobsPerWorker: 10,
		Interval:      50 * time.Millisecond,
		Cooldown:      300 * time.Millisecond,
		StepDown:      100 * time.Millisecond,
	}
}

func startScaler(t *testing.T, pool *pgxpool.Pool, driver drivers.Driver) (s *Scaler, stop func()) {
	t.Helper()
	s = NewScaler(repositories.NewScaler(pool), repositories.NewJobs(pool), repositories.NewWorkers(pool),
		NewEvents(repositories.NewEvents(pool)), driver, fastScalerConfig(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	stop = sync.OnceFunc(func() {
		cancel()
		<-done
	})
	t.Cleanup(stop)
	return s, stop
}

func instances(t *testing.T, d drivers.Driver) []drivers.Instance {
	t.Helper()
	list, err := d.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// watchDecisions collects the decision events that come back over NOTIFY.
func watchDecisions(t *testing.T, pool *pgxpool.Pool) func() []models.Decision {
	t.Helper()
	events := NewEvents(repositories.NewEvents(pool))
	var (
		mu  sync.Mutex
		got []models.Decision
	)
	events.watch(func(ev Event) {
		var d models.Decision
		if ev.Name != "decision" || json.Unmarshal(ev.Data, &d) != nil {
			return
		}
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		events.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitUntil(t, 5*time.Second, "the events listener", events.Listening)
	return func() []models.Decision {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func queueJobs(t *testing.T, pool *pgxpool.Pool, jobType string, n int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into jobs (team_id, type) select $1, $2 from generate_series(1, $3)`,
		defaultTeam(t, pool), jobType, n); err != nil {
		t.Fatal(err)
	}
}

func TestScalerFollowsTheQueue(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	decisions := watchDecisions(t, pool)
	driver := drivers.NewNoop()
	startScaler(t, pool, driver)

	waitUntil(t, 5*time.Second, "the minimum of one worker", func() bool { return len(instances(t, driver)) == 1 })
	first := instances(t, driver)[0]
	listed, err := repositories.NewWorkers(pool).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != first.Name || listed[0].State != "starting" {
		t.Fatalf("workers %+v, want %s starting", listed, first.Name)
	}

	// A type its rate limit is holding back doesn't call for workers.
	if _, err := pool.Exec(ctx, `
		insert into jobs (team_id, type, throttled_at) select $1, 'email.send', now() from generate_series(1, 200)`,
		defaultTeam(t, pool)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(instances(t, driver)); n != 1 {
		t.Fatalf("%d workers for jobs held by their rate limit, want 1", n)
	}

	// 25 jobs want 3 workers.
	queueJobs(t, pool, "sleep", 25)
	waitUntil(t, 5*time.Second, "three workers", func() bool { return len(instances(t, driver)) == 3 })

	// 200 want 20, but the default team's plan only runs 6 jobs at once, and the
	// pool stops at 4 anyway.
	queueJobs(t, pool, "sleep", 175)
	want := models.ScalerStatus{Current: 4, Target: 4, Min: 1, Max: 4, JobsPerWorker: 10, IntervalSeconds: 0.05,
		CooldownSeconds: 0.3, StepDownSeconds: 0.1}
	waitUntil(t, 5*time.Second, "four workers", func() bool {
		stats, err := repositories.NewJobs(pool).Overview(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return stats.Scaler != nil && *stats.Scaler == want
	})
	if n := len(instances(t, driver)); n != 4 {
		t.Fatalf("%d workers, want 4", n)
	}

	if _, err := pool.Exec(ctx, `delete from jobs`); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "back to one worker", func() bool { return len(instances(t, driver)) == 1 })
	// The worker that stayed is the oldest one.
	if left := instances(t, driver)[0]; left.Name != first.Name {
		t.Errorf("kept %s, want the first worker %s", left.Name, first.Name)
	}
	if listed, _ := repositories.NewWorkers(pool).List(ctx); len(listed) != 1 {
		t.Errorf("stopped workers left rows behind: %+v", listed)
	}

	stored, err := repositories.NewScaler(pool).Decisions(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(stored)
	var steps []string
	for _, d := range stored {
		steps = append(steps, fmt.Sprintf("%s %d->%d", d.Action, d.From, d.To))
	}
	wantSteps := []string{"up 0->1", "up 1->3", "up 3->4", "hold 4->4", "down 4->3", "down 3->2", "down 2->1"}
	if !slices.Equal(steps, wantSteps) {
		t.Fatalf("decisions %v, want %v", steps, wantSteps)
	}
	waitUntil(t, 2*time.Second, "every decision event", func() bool { return len(decisions()) == len(stored) })
	got := decisions()
	event, row := got[len(got)-1], stored[len(stored)-1]
	// The event's time comes from JSON and the row's from pgx, so their locations differ.
	event.At, row.At = event.At.UTC(), row.At.UTC()
	if event != row {
		t.Errorf("last event %+v, want %+v", event, row)
	}
}

func TestOnlyOneScalerActs(t *testing.T) {
	pool := testDB(t)
	// Both scalers see the same workers, as two scalers on one Docker host would.
	driver := drivers.NewNoop()
	a, stopA := startScaler(t, pool, driver)
	b, stopB := startScaler(t, pool, driver)

	waitUntil(t, 5*time.Second, "a scaler to lead", func() bool { return a.Leading() || b.Leading() })
	waitUntil(t, 5*time.Second, "the minimum of one worker", func() bool { return len(instances(t, driver)) == 1 })
	if a.Leading() && b.Leading() {
		t.Fatal("both scalers lead")
	}
	waitUntil(t, 5*time.Second, "the decision to be stored", func() bool {
		stored, err := repositories.NewScaler(pool).Decisions(context.Background(), 50)
		if err != nil {
			t.Fatal(err)
		}
		return len(stored) == 1
	})

	other, stopLeader := b, stopA
	if b.Leading() {
		other, stopLeader = a, stopB
	}
	stopLeader()
	waitUntil(t, 5*time.Second, "the other scaler to take over", other.Leading)
	queueJobs(t, pool, "sleep", 15)
	waitUntil(t, 5*time.Second, "two workers", func() bool { return len(instances(t, driver)) == 2 })

	stored, err := repositories.NewScaler(pool).Decisions(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[1].To != 1 || stored[0].From != 1 || stored[0].To != 2 {
		t.Fatalf("decisions %+v, want 0->1 by the first leader and 1->2 by the second", stored)
	}
}
