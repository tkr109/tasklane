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

	teamID, err := repositories.NewTeams(pool).IDByName(ctx, "default")
	if err != nil {
		return fmt.Errorf("look up default team: %w", err)
	}
	queue := services.NewQueue(repositories.NewJobs(pool), jobs.Defaults())

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           routes.New(controllers.NewJobs(queue, teamID), pool.Ping),
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

	slog.Info("worker started")
	<-ctx.Done()
	slog.Info("worker stopped")
	return nil
}
