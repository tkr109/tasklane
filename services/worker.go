package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var (
	errTimedOut  = errors.New("job timed out")
	errLeaseLost = errors.New("lease lost")
)

type Worker struct {
	id      string
	host    string
	cfg     config.Worker
	repo    *repositories.Jobs
	workers *repositories.Workers
	types   jobs.Registry

	wake    chan struct{}
	running sync.WaitGroup
}

func NewWorker(repo *repositories.Jobs, workers *repositories.Workers, types jobs.Registry, cfg config.Worker) *Worker {
	host, _ := os.Hostname()
	return &Worker{
		id:      fmt.Sprintf("%s-%08x", host, rand.Uint32()),
		host:    host,
		cfg:     cfg,
		repo:    repo,
		workers: workers,
		types:   types,
		wake:    make(chan struct{}, 1),
	}
}

func (w *Worker) ID() string {
	return w.id
}

// Run pulls and runs jobs until ctx is cancelled, then waits for the jobs it
// already holds.
func (w *Worker) Run(ctx context.Context) error {
	log := slog.With("worker", w.id)
	if err := w.workers.Heartbeat(ctx, w.id, w.host); err != nil {
		return fmt.Errorf("register worker: %w", err)
	}
	log.Info("worker started", "concurrency", w.cfg.Concurrency, "types", w.types.Names())

	// The heartbeat outlives ctx so the worker still shows as alive while it drains.
	beatCtx, stopBeat := context.WithCancel(context.WithoutCancel(ctx))
	defer stopBeat()
	go w.heartbeat(beatCtx)
	go w.reap(ctx)
	go w.listen(ctx)

	w.loop(ctx, context.WithoutCancel(ctx))

	log.Info("worker draining")
	w.running.Wait()
	stopBeat()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := w.workers.Remove(cleanupCtx, w.id); err != nil {
		log.Warn("remove worker row", "err", err)
	}
	log.Info("worker stopped")
	return nil
}

func (w *Worker) heartbeat(ctx context.Context) {
	t := time.NewTicker(w.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := w.workers.Heartbeat(ctx, w.id, w.host); err != nil && ctx.Err() == nil {
			slog.Warn("heartbeat", "worker", w.id, "err", err)
		}
	}
}

func (w *Worker) loop(ctx, jobCtx context.Context) {
	slots := make(chan struct{}, w.cfg.Concurrency)
	types := w.types.Names()
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}

		job, err := w.repo.Grab(ctx, w.id, types, w.cfg.Lease)
		if err != nil {
			<-slots
			if !errors.Is(err, repositories.ErrNotFound) && ctx.Err() == nil {
				slog.Error("grab job", "worker", w.id, "err", err)
			}
			select {
			case <-w.wake:
			case <-time.After(w.cfg.PollInterval):
			case <-ctx.Done():
				return
			}
			continue
		}

		w.running.Go(func() {
			defer func() { <-slots }()
			w.execute(jobCtx, job)
		})
	}
}

// listen turns Postgres notifications into wakeups for the grab loop, so new jobs
// start right away instead of on the next poll.
func (w *Worker) listen(ctx context.Context) {
	for {
		err := w.repo.ListenQueued(ctx, func() {
			select {
			case w.wake <- struct{}{}:
			default:
			}
		})
		if ctx.Err() != nil {
			return
		}
		slog.Warn("listen for new jobs, retrying", "worker", w.id, "err", err)
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (w *Worker) execute(jobCtx context.Context, job models.Job) {
	log := slog.With("worker", w.id, "job", job.ID, "type", job.Type, "attempt", job.Attempts)
	jt := w.types[job.Type]

	ctx, cancel := context.WithCancelCause(jobCtx)
	defer cancel(nil)
	ctx, stop := context.WithTimeoutCause(ctx, jt.Timeout, errTimedOut)
	defer stop()
	go w.keepLease(ctx, job.ID, cancel)

	log.Info("job started")
	start := time.Now()
	err := runHandler(ctx, jt.Handler, job, func(percent int) {
		w.setProgress(ctx, job.ID, percent)
	})

	// The outcome is recorded even if the job's own context has just ended.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelWrite()

	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errLeaseLost):
		log.Warn("lease lost, dropping the result", "err", err)
		return
	case err == nil:
		err = w.repo.Succeed(writeCtx, job.ID, w.id)
		log.Info("job succeeded", "took", time.Since(start))
	case errors.Is(cause, errTimedOut):
		err = w.fail(writeCtx, log, job, fmt.Errorf("timed out after %s", jt.Timeout))
	default:
		err = w.fail(writeCtx, log, job, err)
	}
	if err != nil {
		log.Error("record job outcome", "err", err)
	}
}

func (w *Worker) fail(ctx context.Context, log *slog.Logger, job models.Job, jobErr error) error {
	if job.Attempts >= job.MaxAttempts {
		log.Warn("job is dead", "err", jobErr)
		return w.repo.MarkDead(ctx, job.ID, w.id, jobErr.Error())
	}
	delay := backoff(job.Attempts, w.cfg.RetryBaseDelay, w.cfg.RetryMaxDelay)
	log.Warn("job failed, will retry", "err", jobErr, "retry_in", delay)
	return w.repo.ScheduleRetry(ctx, job.ID, w.id, delay, jobErr.Error())
}

// keepLease renews the lease while the job runs. If the worker has lost the job,
// it cancels the job's context so the handler stops.
func (w *Worker) keepLease(ctx context.Context, jobID int64, lost context.CancelCauseFunc) {
	t := time.NewTicker(w.cfg.Lease / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		held, err := w.repo.ExtendLease(ctx, jobID, w.id, w.cfg.Lease)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("extend lease", "worker", w.id, "job", jobID, "err", err)
			}
			continue
		}
		if !held {
			lost(errLeaseLost)
			return
		}
	}
}

func (w *Worker) setProgress(ctx context.Context, jobID int64, percent int) {
	percent = max(0, min(percent, 100))
	if err := w.repo.SetProgress(ctx, jobID, w.id, percent); err != nil && ctx.Err() == nil {
		slog.Warn("set progress", "worker", w.id, "job", jobID, "err", err)
	}
}

func runHandler(ctx context.Context, h jobs.Handler, job models.Job, progress jobs.Progress) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(ctx, job.Payload, progress)
}
