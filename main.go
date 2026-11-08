package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
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

	jobsRepo := repositories.NewJobs(pool)
	schedules := repositories.NewSchedules(pool)
	events := services.NewEvents(repositories.NewEvents(pool))
	// Every api runs a scheduler; the one holding the advisory lock fires schedules.
	scheduler := services.NewScheduler(schedules, cfg.API.SchedulerTick)

	bgCtx, stopBackground := context.WithCancel(ctx)
	var background sync.WaitGroup
	background.Go(func() { scheduler.Run(bgCtx) })
	background.Go(func() { events.Run(bgCtx) })
	defer func() {
		stopBackground()
		background.Wait()
	}()

	queue := services.NewQueue(jobsRepo, jobs.Defaults())
	limits := services.NewLimits(repositories.NewRateBuckets(pool), jobsRepo, cfg.API.QueueCeiling)
	handler := routes.New(
		controllers.NewJobs(queue, limits),
		controllers.NewSchedules(services.NewSchedules(schedules, jobs.Defaults(), cfg.API.MaxSchedulesPerTeam)),
		controllers.NewTeams(teams),
		controllers.NewEvents(events),
		auth,
		cfg.API.AdminToken,
		pool.Ping,
	)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Event streams never go idle, so Shutdown would wait them out.
	srv.RegisterOnShutdown(events.Close)

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

	worker := services.NewWorker(repositories.NewJobs(pool), repositories.NewWorkers(pool),
		repositories.NewRateBuckets(pool), jobs.Defaults(), cfg.Worker)
	return worker.Run(ctx)
}
