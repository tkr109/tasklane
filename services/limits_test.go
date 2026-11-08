package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

func TestUntilTokens(t *testing.T) {
	const free = 100.0 / 60 // the free plan's jobs per second
	tests := []struct {
		have float64
		want int
		rate float64
		wait time.Duration
	}{
		{0, 1, 10, 100 * time.Millisecond},
		{0.5, 1, 10, 50 * time.Millisecond},
		{3, 1, 10, 0},
		{1, 1, 10, 0},
		{0, 1, free, 600 * time.Millisecond},
		{5.5, 10, free, 2700 * time.Millisecond},
		{0, 200, free, 2 * time.Minute},
	}
	for _, tt := range tests {
		got := untilTokens(tt.have, tt.want, tt.rate)
		if diff := got - tt.wait; diff < -time.Microsecond || diff > time.Microsecond {
			t.Errorf("untilTokens(%v, %d, %.3f) = %v, want %v", tt.have, tt.want, tt.rate, got, tt.wait)
		}
	}
}

func TestAdmitRefusesMoreThanTheBurst(t *testing.T) {
	limits := NewLimits(nil, nil, 0)
	team := models.Team{ID: 1, Plan: models.Plan{Name: "free", JobsPerMinute: 100, Burst: 200}}
	if _, err := limits.Admit(context.Background(), team, 201); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a batch over the burst: err = %v, want ErrInvalid", err)
	}
}

func TestTypeBurst(t *testing.T) {
	tests := map[float64]int{0.5: 1, 1: 1, 10: 1, 25: 2, 200: 20}
	for rate, want := range tests {
		if got := typeBurst(rate); got != want {
			t.Errorf("typeBurst(%v) = %d, want %d", rate, got, want)
		}
	}
}

func TestTypeLimitHoldsAcrossWorkers(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	const (
		rate  = 20
		total = 60
	)

	var (
		mu     sync.Mutex
		starts []time.Time
	)
	types := jobs.Registry{"limited": {Timeout: 5 * time.Second, Handler: func(context.Context, json.RawMessage, jobs.Progress) error {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	}}}

	once := 1
	specs := make([]JobSpec, total)
	for i := range specs {
		specs[i] = JobSpec{Type: "limited", MaxAttempts: &once}
	}
	repo := repositories.NewJobs(pool)
	if _, err := NewQueue(repo, types).SubmitBatch(ctx, defaultTeam(t, pool), specs); err != nil {
		t.Fatal(err)
	}

	cfg := fastConfig()
	cfg.TypeLimits = map[string]float64{"limited": rate}
	startWorkerWith(t, pool, types, cfg)
	startWorkerWith(t, pool, types, cfg)

	waitUntil(t, 5*time.Second, "the limit to hold jobs back", func() bool {
		held, err := repo.HeldByLimit(ctx, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return held["limited"] > 0
	})
	waitUntil(t, 20*time.Second, "every job to succeed", func() bool {
		var n int
		_ = pool.QueryRow(ctx, `select count(*) from jobs where status = 'succeeded'`).Scan(&n)
		return n == total
	})

	mu.Lock()
	defer mu.Unlock()
	slices.SortFunc(starts, func(a, b time.Time) int { return a.Compare(b) })
	// A bucket lets through its burst plus the rate; one more covers the gap
	// between taking a token and the handler starting.
	most := typeBurst(rate) + rate + 1
	for i, from := range starts {
		in := 0
		for _, at := range starts[i:] {
			if at.Sub(from) >= time.Second {
				break
			}
			in++
		}
		if in > most {
			t.Fatalf("%d jobs started within a second of %v, the limit allows %d", in, from.Format(time.StampMilli), most)
		}
	}

	var burned int
	if err := pool.QueryRow(ctx, `select count(*) from jobs where attempts <> 1`).Scan(&burned); err != nil {
		t.Fatal(err)
	}
	if burned != 0 {
		t.Errorf("%d jobs used more than one attempt", burned)
	}
	held := countEvents(t, pool, "throttled")
	if held == 0 {
		t.Error("no job was held back, so the test didn't exercise the limit")
	}
	t.Logf("%d jobs ran in %v with %d holds", total, starts[len(starts)-1].Sub(starts[0]).Round(time.Millisecond), held)
}
