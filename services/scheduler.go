package services

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

// fireBatch is how many due schedules one transaction fires.
const fireBatch = 100

// Scheduler fires repeating jobs. Every api process runs one, but only the one
// holding the Postgres advisory lock fires. When that process goes away, its
// connection and the lock go with it, and another takes over within a tick.
type Scheduler struct {
	repo    *repositories.Schedules
	tick    time.Duration
	leading atomic.Bool
}

func NewScheduler(repo *repositories.Schedules, tick time.Duration) *Scheduler {
	return &Scheduler{repo: repo, tick: tick}
}

// Leading reports whether this process holds the lead. One whose connection has
// just died may think so for up to a tick, but it can't fire through a dead one.
func (s *Scheduler) Leading() bool {
	return s.leading.Load()
}

// Run competes for the lead, and fires due schedules every tick while it has it,
// until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		err := s.session(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("scheduler session ended, the lead is up for grabs", "err", err)
		select {
		case <-time.After(s.tick):
		case <-ctx.Done():
			return
		}
	}
}

// session runs on one dedicated connection: it asks for the lock every tick until
// it gets it, then fires through it. Any error ends the session, and closing the
// connection gives up the lock.
func (s *Scheduler) session(ctx context.Context) error {
	conn, err := s.repo.LeaderConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Step down before the connection, and the lock with it, goes.
	defer s.leading.Store(false)

	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		if !s.leading.Load() {
			won, err := conn.TryLead(ctx)
			if err != nil {
				return err
			}
			if won {
				s.leading.Store(true)
				slog.Info("leading the scheduler")
			}
		}
		if s.leading.Load() {
			if err := s.fire(ctx, conn); err != nil {
				return err
			}
		}
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, conn *repositories.LeaderConn) error {
	for {
		n, err := conn.FireDue(ctx, fireBatch, s.next)
		if err != nil {
			return err
		}
		if n > 0 {
			slog.Info("fired schedules", "count", n)
		}
		if n < fireBatch {
			return nil
		}
	}
}

// next is when a schedule that is firing now runs again. It counts from now, not
// from the tick that fired, so after an outage a schedule catches up once instead
// of once per missed tick.
func (s *Scheduler) next(sc models.Schedule) (time.Time, error) {
	after := time.Now()
	if sc.NextRunAt.After(after) {
		after = sc.NextRunAt
	}
	at, err := nextRun(sc.Cron, sc.Timezone, after)
	if err != nil {
		slog.Error("disabling a schedule that can't run again", "schedule", sc.ID, "err", err)
	}
	return at, err
}
