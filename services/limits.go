package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var (
	ErrRateLimited = errors.New("rate limit reached")
	ErrOverloaded  = errors.New("too many jobs are waiting to run")
)

const (
	// overloadRetryAfter is how long an overloaded API asks callers to wait.
	overloadRetryAfter = 5 * time.Second
	// loadRecount is how stale the shared count of runnable jobs may get.
	loadRecount = time.Second
)

// TryLater turns a submit away for now. It wraps ErrRateLimited or ErrOverloaded.
type TryLater struct {
	Err   error
	After time.Duration
}

func (e *TryLater) Error() string { return e.Err.Error() }
func (e *TryLater) Unwrap() error { return e.Err }

// Quota is what the rate limit headers show: the size of the team's bucket and
// the whole jobs left in it.
type Quota struct {
	Limit     int
	Remaining int
}

// Limits decides whether a submit may go ahead. Each team has a token bucket
// refilled at its plan's jobs per minute, and the whole queue has a ceiling.
type Limits struct {
	buckets *repositories.RateBuckets
	jobs    *repositories.Jobs
	events  *Events
	ceiling int

	mu        sync.Mutex
	runnable  int
	countedAt time.Time
}

func NewLimits(buckets *repositories.RateBuckets, jobs *repositories.Jobs, events *Events, ceiling int) *Limits {
	return &Limits{buckets: buckets, jobs: jobs, events: events, ceiling: ceiling}
}

// Admit charges n jobs to the team's bucket. A refusal for now is a *TryLater.
func (l *Limits) Admit(ctx context.Context, team models.Team, n int) (Quota, error) {
	plan := team.Plan
	if n > plan.Burst {
		return Quota{}, fmt.Errorf("%w: %d jobs at once is more than the %s plan's burst of %d",
			ErrInvalid, n, plan.Name, plan.Burst)
	}
	if err := l.checkLoad(ctx); err != nil {
		return Quota{}, err
	}

	rate := float64(plan.JobsPerMinute) / 60
	ok, left, err := l.buckets.Take(ctx, teamBucket(team.ID), rate, plan.Burst, n)
	if err != nil {
		return Quota{}, err
	}
	quota := Quota{Limit: plan.Burst, Remaining: int(left)}
	if !ok {
		// The teams page shows refusals; losing one only makes that number low.
		if err := l.events.Publish(ctx, "rejected", rejectedJobs{TeamID: team.ID, Jobs: n}); err != nil {
			slog.Warn("publish a rejection", "team", team.ID, "err", err)
		}
		return quota, &TryLater{
			Err:   fmt.Errorf("%w: the %s plan allows %d jobs a minute", ErrRateLimited, plan.Name, plan.JobsPerMinute),
			After: untilTokens(left, n, rate),
		}
	}
	return quota, nil
}

// checkLoad turns submits away while too many jobs wait to run. Every request
// shares one count, refreshed at most once a loadRecount.
func (l *Limits) checkLoad(ctx context.Context) error {
	if l.ceiling == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.countedAt) >= loadRecount {
		n, err := l.jobs.CountRunnable(ctx)
		if err != nil {
			return err
		}
		l.runnable, l.countedAt = n, time.Now()
	}
	if l.runnable >= l.ceiling {
		return &TryLater{Err: ErrOverloaded, After: overloadRetryAfter}
	}
	return nil
}

// untilTokens is how long a bucket holding have tokens, refilled at rate tokens a
// second, takes to hold want.
func untilTokens(have float64, want int, rate float64) time.Duration {
	missing := float64(want) - have
	if missing <= 0 {
		return 0
	}
	return time.Duration(missing / rate * float64(time.Second))
}

// typeBurst lets a rate limited job type start a tenth of a second's worth of
// jobs at once, and always at least one.
func typeBurst(rate float64) int {
	return max(1, int(rate/10))
}

func teamBucket(teamID int64) string {
	return "team:" + strconv.FormatInt(teamID, 10)
}

func typeBucket(jobType string) string {
	return "type:" + jobType
}
