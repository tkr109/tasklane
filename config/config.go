package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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
	// Submits get 503 once this many jobs are waiting to run. 0 turns it off.
	QueueCeiling int
	// SchedulerTick is how often the leading api fires due schedules, and how
	// often the others try to take over the lead.
	SchedulerTick time.Duration
	// MaxSchedulesPerTeam bounds what a team can queue without paying the
	// incoming rate limit, as scheduled jobs don't.
	MaxSchedulesPerTeam int
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
	// TypeLimits caps how many jobs of a type start per second, across all workers.
	TypeLimits map[string]float64
}

func Load() (Config, error) {
	l := loader{}
	cfg := Config{
		DatabaseURL: l.str("DATABASE_URL", "postgres://tasklane:tasklane@localhost:5432/tasklane?sslmode=disable"),
		HTTPAddr:    l.str("HTTP_ADDR", ":8080"),
		LogFormat:   l.str("LOG_FORMAT", "text"),
		API: API{
			AdminToken:          l.str("ADMIN_TOKEN", ""),
			AuthCacheTTL:        l.duration("AUTH_CACHE_TTL", 5*time.Second),
			DevAPIKey:           l.str("DEV_API_KEY", ""),
			QueueCeiling:        l.int("QUEUE_CEILING", 100_000),
			SchedulerTick:       l.duration("SCHEDULER_TICK", time.Second),
			MaxSchedulesPerTeam: l.int("MAX_SCHEDULES_PER_TEAM", 50),
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
			TypeLimits:        l.rates("TYPE_RATE_LIMITS", "email=10/s,webhook=200/s"),
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
	if cfg.API.QueueCeiling < 0 {
		l.fail("QUEUE_CEILING", fmt.Errorf("must not be negative"))
	}
	if cfg.API.SchedulerTick <= 0 {
		l.fail("SCHEDULER_TICK", fmt.Errorf("must be positive"))
	}
	if cfg.API.MaxSchedulesPerTeam < 1 {
		l.fail("MAX_SCHEDULES_PER_TEAM", fmt.Errorf("must be at least 1"))
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

// rates reads per-type limits such as "email=10/s,thumbnail=600/m". Unlike the
// other settings, an empty value counts: it turns every limit off.
func (l *loader) rates(key, def string) map[string]float64 {
	v, ok := os.LookupEnv(key)
	if !ok {
		v = def
	}
	rates, err := parseRates(v)
	if err != nil {
		l.fail(key, err)
	}
	return rates
}

func parseRates(s string) (map[string]float64, error) {
	rates := map[string]float64{}
	for item := range strings.SplitSeq(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, rate, ok := strings.Cut(item, "=")
		count, unit, ok2 := strings.Cut(rate, "/")
		if !ok || !ok2 || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%q should look like type=10/s or type=600/m", item)
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(count), 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%q: the rate must be a positive number", item)
		}
		switch unit {
		case "s":
		case "m":
			n /= 60
		default:
			return nil, fmt.Errorf("%q: the unit must be s or m", item)
		}
		rates[strings.TrimSpace(name)] = n
	}
	return rates, nil
}
