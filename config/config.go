package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	LogLevel    slog.Level
	LogFormat   string
	API         API
	Worker      Worker
}

type API struct {
	AdminToken   string
	AuthCacheTTL time.Duration
	// DevAPIKey, when set, is registered as a key for the default team on startup.
	DevAPIKey string
}

type Worker struct {
	Concurrency       int
	Lease             time.Duration
	PollInterval      time.Duration
	ReapInterval      time.Duration
	HeartbeatInterval time.Duration
	ShutdownGrace     time.Duration
	RetryBaseDelay    time.Duration
	RetryMaxDelay     time.Duration
	// A waiting job gains one priority level every AgingStep.
	AgingStep time.Duration
	// FairShareWindow is roughly how far back a team's served jobs count.
	FairShareWindow time.Duration
}

func Load() (Config, error) {
	l := loader{}
	cfg := Config{
		DatabaseURL: l.str("DATABASE_URL", "postgres://tasklane:tasklane@localhost:5432/tasklane?sslmode=disable"),
		HTTPAddr:    l.str("HTTP_ADDR", ":8080"),
		LogFormat:   l.str("LOG_FORMAT", "text"),
		API: API{
			AdminToken:   l.str("ADMIN_TOKEN", ""),
			AuthCacheTTL: l.duration("AUTH_CACHE_TTL", 5*time.Second),
			DevAPIKey:    l.str("DEV_API_KEY", ""),
		},
		Worker: Worker{
			Concurrency:       l.int("WORKER_CONCURRENCY", 4),
			Lease:             l.duration("WORKER_LEASE", 30*time.Second),
			PollInterval:      l.duration("WORKER_POLL_INTERVAL", 5*time.Second),
			ReapInterval:      l.duration("WORKER_REAP_INTERVAL", 10*time.Second),
			HeartbeatInterval: l.duration("WORKER_HEARTBEAT_INTERVAL", 5*time.Second),
			ShutdownGrace:     l.duration("WORKER_SHUTDOWN_GRACE", 30*time.Second),
			RetryBaseDelay:    l.duration("RETRY_BASE_DELAY", 2*time.Second),
			RetryMaxDelay:     l.duration("RETRY_MAX_DELAY", 10*time.Minute),
			AgingStep:         l.duration("PRIORITY_AGING_STEP", 30*time.Second),
			FairShareWindow:   l.duration("FAIR_SHARE_WINDOW", time.Minute),
		},
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(l.str("LOG_LEVEL", "info"))); err != nil {
		l.fail("LOG_LEVEL", err)
	}
	if cfg.Worker.Concurrency < 1 {
		l.fail("WORKER_CONCURRENCY", fmt.Errorf("must be at least 1"))
	}
	if cfg.Worker.AgingStep <= 0 {
		l.fail("PRIORITY_AGING_STEP", fmt.Errorf("must be positive"))
	}
	if cfg.Worker.FairShareWindow <= 0 {
		l.fail("FAIR_SHARE_WINDOW", fmt.Errorf("must be positive"))
	}
	return cfg, l.err
}

// loader keeps the first parse error so Load can read every variable in one go.
type loader struct {
	err error
}

func (l *loader) fail(key string, err error) {
	if l.err == nil {
		l.err = fmt.Errorf("config %s: %w", key, err)
	}
}

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (l *loader) int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, err)
		return def
	}
	return n
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, err)
		return def
	}
	return d
}
