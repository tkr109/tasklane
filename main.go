package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/repositories"
	"github.com/tkr109/tasklane/routes"
	"github.com/tkr109/tasklane/services"
)

const usage = "usage: tasklane api|worker|scaler"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	slog.SetDefault(newLogger(cfg))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mode := os.Args[1]
	switch mode {
	case "api":
		err = runAPI(ctx, cfg)
	case "worker":
		err = runWorker(ctx, cfg)
	case "scaler":
		slog.Warn("scaler is not implemented yet")
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("exiting", "mode", mode, "err", err)
		os.Exit(1)
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func openDB(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	pool, err := repositories.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := repositories.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func runAPI(ctx context.Context, cfg config.Config) error {
	pool, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	keys := repositories.NewKeys(pool)
	auth := services.NewAuth(keys, cfg.API.AuthCacheTTL)
	teams := services.NewTeams(repositories.NewTeams(pool), keys, auth)
	if cfg.API.DevAPIKey != "" {
		if err := teams.EnsureDevKey(ctx, cfg.API.DevAPIKey); err != nil {
			return fmt.Errorf("dev api key: %w", err)
		}
		slog.Info("dev api key for the default team", "key", cfg.API.DevAPIKey)
	}
	if cfg.API.AdminToken == "" {
		slog.Warn("ADMIN_TOKEN is not set, admin endpoints are off")
	}

	// Every api runs a scheduler; the one holding the advisory lock fires schedules.
	schedules := repositories.NewSchedules(pool)
	scheduler := services.NewScheduler(schedules, cfg.API.SchedulerTick)
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		scheduler.Run(schedulerCtx)
	}()
	defer func() {
		stopScheduler()
		<-schedulerDone
	}()

	jobsRepo := repositories.NewJobs(pool)
	queue := services.NewQueue(jobsRepo, jobs.Defaults(cfg.Jobs))
	limits := services.NewLimits(repositories.NewRateBuckets(pool), jobsRepo, cfg.API.QueueCeiling)
	handler := routes.New(
		controllers.NewJobs(queue, limits),
		controllers.NewSchedules(services.NewSchedules(schedules, jobs.Defaults(cfg.Jobs))),
		controllers.NewTeams(teams),
		auth,
		cfg.API.AdminToken,
		pool.Ping,
	)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("api stopped")
	return nil
}

func runWorker(ctx context.Context, cfg config.Config) error {
	pool, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.Jobs.WebhookSecret == "" {
		slog.Warn("WEBHOOK_SECRET is not set, receivers can't trust webhook signatures")
	}
	worker := services.NewWorker(repositories.NewJobs(pool), repositories.NewWorkers(pool),
		repositories.NewRateBuckets(pool), jobs.Defaults(cfg.Jobs), cfg.Worker)
	return worker.Run(ctx)
}
