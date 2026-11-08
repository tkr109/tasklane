package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"sync/atomic"
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
	buckets *repositories.RateBuckets
	types   jobs.Registry

	wake    chan struct{}
	running sync.WaitGroup
	// done counts the jobs this worker has finished; heartbeats report it.
	done atomic.Int64
	// held is when each rate limited type is due its next token. Only the grab
	// loop touches it.
	held map[string]time.Time
}

func NewWorker(repo *repositories.Jobs, workers *repositories.Workers, buckets *repositories.RateBuckets, types jobs.Registry, cfg config.Worker) *Worker {
	host, _ := os.Hostname()
	id := cfg.ID
	if id == "" {
		id = fmt.Sprintf("%s-%08x", host, rand.Uint32())
	}
	return &Worker{
		id:      id,
		host:    host,
		cfg:     cfg,
		repo:    repo,
		workers: workers,
		buckets: buckets,
		types:   types,
		wake:    make(chan struct{}, 1),
		held:    map[string]time.Time{},
	}
}

// Run pulls and runs jobs until ctx is cancelled. It then stops grabbing and gives
// running jobs the shutdown grace period to finish. Whatever is still running
// after that is abandoned, and the reaper requeues it once the lease runs out.
func (w *Worker) Run(ctx context.Context) error {
	log := slog.With("worker", w.id)
	if err := w.workers.Heartbeat(ctx, w.id, w.host, 0); err != nil {
		return fmt.Errorf("register worker: %w", err)
	}
	log.Info("worker started", "concurrency", w.cfg.Concurrency, "types", w.types.Names(), "limits", w.cfg.TypeLimits)

	// The heartbeat outlives ctx so the worker still shows as alive while it drains.
	beatCtx, stopBeat := context.WithCancel(context.WithoutCancel(ctx))
	defer stopBeat()
	go w.heartbeat(beatCtx)
	go w.reap(ctx)
	go w.listen(ctx)

	// Jobs get their own context so SIGTERM stops new grabs without killing them.
	jobCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()
	w.loop(ctx, jobCtx)

	log.Info("worker draining", "grace", w.cfg.ShutdownGrace)
	drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	if err := w.workers.Drain(drainCtx, w.id); err != nil {
		log.Warn("mark the worker draining", "err", err)
	}
	cancelDrain()
	if !waitFor(&w.running, w.cfg.ShutdownGrace) {
		log.Warn("grace period over, leaving unfinished jobs to the reaper")
		cancelJobs()
		waitFor(&w.running, 5*time.Second)
	}
	stopBeat()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := w.workers.Remove(cleanupCtx, w.id, w.done.Load()); err != nil {
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
		if err := w.workers.Heartbeat(ctx, w.id, w.host, w.done.Load()); err != nil && ctx.Err() == nil {
			slog.Warn("heartbeat", "worker", w.id, "err", err)
		}
	}
}

func (w *Worker) loop(ctx, jobCtx context.Context) {
	slots := make(chan struct{}, w.cfg.Concurrency)
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}

		job, err := grab(ctx, w.repo, w.id, w.startable(), w.cfg)
		if err != nil {
			<-slots
			if !errors.Is(err, repositories.ErrNotFound) && ctx.Err() == nil {
				slog.Error("grab job", "worker", w.id, "err", err)
			}
			select {
			case <-w.wake:
			case <-time.After(w.idleWait()):
			case <-ctx.Done():
				return
			}
			continue
		}
		if !w.takeToken(ctx, job) {
			<-slots
			continue
		}

		w.running.Go(func() {
			defer func() {
				<-slots
				// The job also freed a slot under its team's worker cap, and nothing
				// else announces that, so look for work right away.
				w.nudge()
			}()
			w.execute(jobCtx, job)
		})
	}
}

// startable is the job types this worker may grab now: all of them but those
// waiting for their rate limit to refill.
func (w *Worker) startable() []string {
	now := time.Now()
	var types []string
	for _, name := range w.types.Names() {
		if now.Before(w.held[name]) {
			continue
		}
		types = append(types, name)
	}
	return types
}

// idleWait is how long to wait for work when there was none to grab: the poll
// interval, or less if a held type is due a token sooner.
func (w *Worker) idleWait() time.Duration {
	wait := w.cfg.PollInterval
	for _, until := range w.held {
		if d := time.Until(until); d > 0 {
			wait = min(wait, d)
		}
	}
	return wait
}

// takeToken spends a token of the job type's rate limit, if the type has one.
// When there is none, the job goes back to the queue without using up its
// attempt, and this worker leaves the type alone until the next token is due.
func (w *Worker) takeToken(ctx context.Context, job models.Job) bool {
	rate, limited := w.cfg.TypeLimits[job.Type]
	if !limited {
		return true
	}
	ok, left, err := w.buckets.Take(ctx, typeBucket(job.Type), rate, typeBurst(rate), 1)
	if err != nil && ctx.Err() == nil {
		slog.Error("take a rate limit token", "worker", w.id, "type", job.Type, "err", err)
	}
	if ok {
		return true
	}

	wait := untilTokens(left, 1, rate)
	w.held[job.Type] = time.Now().Add(wait)
	// The hold must land even during shutdown, or the lease runs out and the
	// reaper counts the attempt.
	holdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	reason := fmt.Sprintf("%s is at its limit of %.3g/s, back in %s", job.Type, rate, wait.Round(time.Millisecond))
	if err := w.repo.Hold(holdCtx, job.ID, w.id, wait, reason); err != nil {
		slog.Error("hold a rate limited job", "worker", w.id, "job", job.ID, "err", err)
	}
	return false
}

// listen turns Postgres notifications into wakeups for the grab loop, so new jobs
// start right away instead of on the next poll.
func (w *Worker) listen(ctx context.Context) {
	for {
		err := w.repo.ListenQueued(ctx, w.nudge)
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

// nudge wakes the grab loop if it is waiting for work.
func (w *Worker) nudge() {
	select {
	case w.wake <- struct{}{}:
	default:
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
	progress := newProgress(progressEvery, func(percent int) {
		w.setProgress(ctx, job.ID, percent)
	})
	result, err := runHandler(ctx, jt.Handler, job, progress.report)
	progress.stop()

	// The outcome is recorded even if the job's own context has just ended.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelWrite()

	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errLeaseLost):
		log.Warn("lease lost, dropping the result", "err", err)
		return
	case err == nil:
		err = w.repo.Succeed(writeCtx, job.ID, w.id, result)
		log.Info("job succeeded", "took", time.Since(start))
	case errors.Is(cause, errTimedOut):
		err = w.fail(writeCtx, log, job, fmt.Errorf("timed out after %s", jt.Timeout))
	case jobCtx.Err() != nil:
		log.Warn("interrupted by shutdown, the lease will hand it back")
		return
	default:
		err = w.fail(writeCtx, log, job, err)
	}
	if err != nil {
		log.Error("record job outcome", "err", err)
		return
	}
	w.done.Add(1)
}

func (w *Worker) fail(ctx context.Context, log *slog.Logger, job models.Job, jobErr error) error {
	if job.Attempts >= job.MaxAttempts || jobs.IsPermanent(jobErr) {
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

func runHandler(ctx context.Context, h jobs.Handler, job models.Job, progress jobs.Progress) (result json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	out, err := h(ctx, job.Payload, progress)
	if err != nil || out == nil {
		return nil, err
	}
	return json.Marshal(out)
}

// waitFor reports whether wg finished within d.
func waitFor(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
