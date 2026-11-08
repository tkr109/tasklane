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
	"github.com/tkr109/tasklane/drivers"
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
		err = runScaler(ctx, cfg)
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
	teamsRepo := repositories.NewTeams(pool)
	teams := services.NewTeams(teamsRepo, keys, auth)
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
	monitor := services.NewMonitor(jobsRepo, repositories.NewWorkers(pool), teamsRepo, events,
		jobs.Defaults(cfg.Jobs), cfg.Worker.TypeLimits, cfg.API.QueueCeiling)
	// Every api runs a scheduler; the one holding the advisory lock fires schedules,
	// and publishes the stats.
	scheduler := services.NewScheduler(schedules, cfg.API.SchedulerTick)

	bgCtx, stopBackground := context.WithCancel(ctx)
	var background sync.WaitGroup
	background.Go(func() { scheduler.Run(bgCtx) })
	background.Go(func() { events.Run(bgCtx) })
	background.Go(func() { monitor.Run(bgCtx, scheduler.Leading) })
	defer func() {
		stopBackground()
		background.Wait()
	}()

	queue := services.NewQueue(jobsRepo, jobs.Defaults(cfg.Jobs))
	limits := services.NewLimits(repositories.NewRateBuckets(pool), jobsRepo, events, cfg.API.QueueCeiling)
	handler := routes.New(
		controllers.NewJobs(queue, limits),
		controllers.NewSchedules(services.NewSchedules(schedules, jobs.Defaults(cfg.Jobs), cfg.API.MaxSchedulesPerTeam)),
		controllers.NewTeams(teams),
		controllers.NewDashboard(monitor, services.NewDeadJobs(jobsRepo)),
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

	if cfg.Jobs.WebhookSecret == "" {
		slog.Warn("WEBHOOK_SECRET is not set, receivers can't trust webhook signatures")
	}
	worker := services.NewWorker(repositories.NewJobs(pool), repositories.NewWorkers(pool),
		repositories.NewRateBuckets(pool), jobs.Defaults(cfg.Jobs), cfg.Worker)
	return worker.Run(ctx)
}

func runScaler(ctx context.Context, cfg config.Config) error {
	pool, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	var driver drivers.Driver = drivers.NewNoop()
	if cfg.Scaler.Driver == "docker" {
		// A worker drains for its grace period, then needs a moment to hand back
		// what it didn't finish and remove its row.
		docker := drivers.NewDocker(cfg.Scaler.Docker, cfg.Worker.ShutdownGrace+10*time.Second)
		if err := docker.Ping(ctx); err != nil {
			return fmt.Errorf("reach docker at %s: %w", cfg.Scaler.Docker.Socket, err)
		}
		driver = docker
	}
	slog.Info("scaler started", "driver", cfg.Scaler.Driver)

	scaler := services.NewScaler(repositories.NewScaler(pool), repositories.NewJobs(pool), repositories.NewWorkers(pool),
		services.NewEvents(repositories.NewEvents(pool)), driver, cfg.Scaler, cfg.Worker.Concurrency)
	scaler.Run(ctx)
	slog.Info("scaler stopped")
	return nil
}
