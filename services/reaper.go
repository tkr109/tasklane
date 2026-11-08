package services

import (
	"context"
	"log/slog"
	"time"
)

// reap runs on every worker. The query skips rows another reaper has locked, so
// running it in many places at once is safe.
func (w *Worker) reap(ctx context.Context) {
	t := time.NewTicker(w.cfg.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		n, err := w.repo.ReapExpired(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("reap expired leases", "worker", w.id, "err", err)
			}
			continue
		}
		if n > 0 {
			slog.Info("took back jobs with expired leases", "worker", w.id, "count", n)
		}
	}
}
