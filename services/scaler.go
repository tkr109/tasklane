package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/drivers"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

// Scaler sizes the worker pool to the queue. Every scaler process competes for an
// advisory lock, as the schedulers do, and only the one holding it acts.
type Scaler struct {
	cfg     config.Scaler
	driver  drivers.Driver
	repo    *repositories.Scaler
	jobs    *repositories.Jobs
	workers *repositories.Workers
	events  *Events
	leading atomic.Bool
	// policy is only touched by the session that leads.
	policy policy

	mu sync.Mutex
	// stopping is the workers being stopped in the background. They don't count.
	stopping map[string]bool
	stops    sync.WaitGroup
}

// NewScaler returns a scaler for workers that each run concurrency jobs at once.
func NewScaler(repo *repositories.Scaler, jobsRepo *repositories.Jobs, workers *repositories.Workers, events *Events,
	driver drivers.Driver, cfg config.Scaler, concurrency int) *Scaler {
	return &Scaler{
		cfg:     cfg,
		driver:  driver,
		repo:    repo,
		jobs:    jobsRepo,
		workers: workers,
		events:  events,
		policy: policy{
			min:           cfg.Min,
			max:           cfg.Max,
			jobsPerWorker: cfg.JobsPerWorker,
			concurrency:   concurrency,
			cooldown:      cfg.Cooldown,
			stepDown:      cfg.StepDown,
		},
		stopping: map[string]bool{},
	}
}

func (s *Scaler) Leading() bool {
	return s.leading.Load()
}

// Run competes for the lead, and sizes the pool every interval while it has it,
// until ctx ends. Then it waits for the workers it is still stopping.
func (s *Scaler) Run(ctx context.Context) {
	defer s.stops.Wait()
	for {
		err := s.session(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("scaler session ended, the lead is up for grabs", "err", err)
		select {
		case <-time.After(s.cfg.Interval):
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scaler) session(ctx context.Context) error {
	leader, err := s.repo.Leader(ctx)
	if err != nil {
		return err
	}
	defer leader.Close()
	defer s.leading.Store(false)

	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		if s.leading.Load() {
			// Another scaler takes over as soon as this connection drops.
			if err := leader.Alive(ctx); err != nil {
				return err
			}
		} else {
			won, err := leader.TryLead(ctx)
			if err != nil {
				return err
			}
			if won {
				s.leading.Store(true)
				// A new leader waits out a cool-down of its own before scaling down.
				s.policy.fewerSince, s.policy.lastStepDown = time.Time{}, time.Time{}
				slog.Info("leading the scaler", "min", s.cfg.Min, "max", s.cfg.Max, "jobs_per_worker", s.cfg.JobsPerWorker)
			}
		}
		if s.leading.Load() {
			if err := s.step(ctx); err != nil && ctx.Err() == nil {
				slog.Error("scale workers", "err", err)
			}
		}
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// step looks at the workers and the queue, and makes one decision.
func (s *Scaler) step(ctx context.Context) error {
	instances, err := s.driver.List(ctx)
	if err != nil {
		return fmt.Errorf("list workers: %w", err)
	}
	listed, err := s.workers.List(ctx)
	if err != nil {
		return err
	}
	rows := make(map[string]models.Worker, len(listed))
	for _, w := range listed {
		rows[w.ID] = w
	}

	var live []drivers.Instance
	for _, in := range instances {
		switch {
		case s.isStopping(in.Name):
		case !in.Running:
			s.cleanUp(ctx, in)
		// A draining worker is on its way out, whoever told it to stop.
		case rows[in.Name].State != "draining":
			live = append(live, in)
		}
	}

	held, err := s.jobs.HeldByLimit(ctx, heldWindow)
	if err != nil {
		return err
	}
	demand, err := s.jobs.Demand(ctx, slices.Collect(maps.Keys(held)))
	if err != nil {
		return err
	}
	for _, n := range held {
		demand.Held += n
	}

	dec := s.policy.decide(time.Now(), demand, len(live))
	switch dec.action {
	case "up":
		dec.to = dec.from + s.start(ctx, dec.to-dec.from)
	case "down":
		s.stop(ctx, pickToStop(live, rows).Name)
	}
	if dec.action == "hold" || dec.to != dec.from {
		s.record(ctx, dec)
	}

	return s.repo.SetStatus(ctx, models.ScalerStatus{
		Current:         dec.to,
		Target:          dec.target,
		Min:             s.cfg.Min,
		Max:             s.cfg.Max,
		JobsPerWorker:   s.cfg.JobsPerWorker,
		IntervalSeconds: s.cfg.Interval.Seconds(),
		CooldownSeconds: s.cfg.Cooldown.Seconds(),
		StepDownSeconds: s.cfg.StepDown.Seconds(),
		ScaleDownAt:     s.policy.scaleDownAt(),
	})
}

// start adds n workers side by side and returns how many started.
func (s *Scaler) start(ctx context.Context, n int) int {
	var (
		wg      sync.WaitGroup
		started atomic.Int64
	)
	for range n {
		wg.Go(func() {
			name := fmt.Sprintf("tasklane-worker-%08x", rand.Uint32())
			if err := s.startOne(ctx, name); err != nil {
				slog.Error("start a worker", "worker", name, "err", err)
				return
			}
			started.Add(1)
		})
	}
	wg.Wait()
	return int(started.Load())
}

// startOne adds the worker's row before its container, so even its first
// heartbeat finds the row it should turn from starting to running.
func (s *Scaler) startOne(ctx context.Context, name string) error {
	if err := s.workers.Starting(ctx, name, name); err != nil {
		return err
	}
	if err := s.driver.Start(ctx, name); err != nil {
		return errors.Join(err, s.workers.Remove(context.WithoutCancel(ctx), name, 0))
	}
	return nil
}

// stop drains a worker in the background, as that takes up to its grace period.
// It carries on even if the scaler is shutting down, and Run waits for it.
func (s *Scaler) stop(ctx context.Context, name string) {
	s.mu.Lock()
	s.stopping[name] = true
	s.mu.Unlock()

	ctx = context.WithoutCancel(ctx)
	s.stops.Go(func() {
		defer func() {
			s.mu.Lock()
			delete(s.stopping, name)
			s.mu.Unlock()
		}()
		if err := s.driver.Stop(ctx, name); err != nil {
			slog.Error("stop a worker", "worker", name, "err", err)
			return
		}
		// A worker removes its own row on the way out, unless it was killed first.
		if err := s.workers.Remove(ctx, name, 0); err != nil {
			slog.Warn("remove a stopped worker's row", "worker", name, "err", err)
		}
	})
}

func (s *Scaler) isStopping(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping[name]
}

// cleanUp removes a worker that exited on its own, crashed or never came up, and
// its row if it left one. A crashed worker's jobs come back when their leases run out.
func (s *Scaler) cleanUp(ctx context.Context, in drivers.Instance) {
	slog.Warn("removing a worker that exited", "worker", in.Name, "status", in.Status)
	if err := s.driver.Stop(ctx, in.Name); err != nil {
		slog.Error("remove an exited worker", "worker", in.Name, "err", err)
		return
	}
	if err := s.workers.Remove(ctx, in.Name, 0); err != nil {
		slog.Warn("remove an exited worker's row", "worker", in.Name, "err", err)
	}
}

func (s *Scaler) record(ctx context.Context, dec decision) {
	// The workers have changed already, so the decision is kept even if the
	// scaler is shutting down.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	slog.Info("scaler decision", "action", dec.action, "from", dec.from, "to", dec.to, "target", dec.target,
		"reason", dec.reason)
	d, err := s.repo.AddDecision(ctx, models.Decision{Action: dec.action, From: dec.from, To: dec.to, Reason: dec.reason})
	if err != nil {
		slog.Error("store a scaler decision", "err", err)
		return
	}
	if err := s.events.Publish(ctx, "decision", d); err != nil {
		slog.Warn("publish a scaler decision", "err", err)
	}
}

// pickToStop chooses the worker that loses least by stopping: the one running the
// fewest jobs, and of those the newest.
func pickToStop(live []drivers.Instance, rows map[string]models.Worker) drivers.Instance {
	return slices.MinFunc(live, func(a, b drivers.Instance) int {
		return cmp.Or(
			cmp.Compare(len(rows[a.Name].RunningJobs), len(rows[b.Name].RunningJobs)),
			b.Created.Compare(a.Created),
		)
	})
}
