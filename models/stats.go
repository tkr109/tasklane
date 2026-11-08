package models

import "time"

// Snapshot is the whole system at one moment, as the dashboard's overview shows it.
type Snapshot struct {
	Counts            Counts  `json:"counts"`
	OldestWaitSeconds float64 `json:"oldest_wait_seconds"`
	// Runnable is the jobs that could start now, less those a rate limit is
	// holding back, since more workers wouldn't start those any sooner.
	Runnable     int          `json:"runnable"`
	QueueCeiling int          `json:"queue_ceiling"`
	DeadGroups   int          `json:"dead_groups"`
	Workers      WorkerCounts `json:"workers"`
	Scaler       ScalerStatus `json:"scaler"`
	Types        []TypeStats  `json:"types"`
}

type Counts struct {
	Queued         int `json:"queued"`
	Running        int `json:"running"`
	SucceededToday int `json:"succeeded_today"`
	// Failed is jobs waiting out their backoff before another attempt.
	Failed int `json:"failed"`
	Dead   int `json:"dead"`
}

type WorkerCounts struct {
	Running  int `json:"running"`
	Starting int `json:"starting"`
	Draining int `json:"draining"`
}

type TypeStats struct {
	Type              string   `json:"type"`
	TimeoutSeconds    float64  `json:"timeout_seconds"`
	OutgoingPerSecond *float64 `json:"outgoing_per_second"`
	// RatePerSecond is how many jobs of the type finished a second, lately.
	RatePerSecond float64 `json:"rate_per_second"`
	Held          int     `json:"held"`
}

// Sample is one second of the charts: the queue and workers at the end of it, and
// the attempts that finished during it.
type Sample struct {
	At        time.Time `json:"at"`
	Queued    int       `json:"queued"`
	Workers   int       `json:"workers"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
}

// Stats is what GET /api/stats returns: the latest snapshot and the recent samples.
type Stats struct {
	Snapshot
	Samples []Sample `json:"samples"`
}

// StatsTick is the stats event sent every second: a snapshot and the new sample.
type StatsTick struct {
	Snapshot
	Sample Sample `json:"sample"`
}

// TeamUsage is one team's share of the work right now.
type TeamUsage struct {
	TeamID             int64 `json:"team_id"`
	Queued             int   `json:"queued"`
	Running            int   `json:"running"`
	JobsLastMinute     int   `json:"jobs_last_minute"`
	RejectedLastMinute int   `json:"rejected_last_minute"`
	// FairShare is the part of the workers the team's plan weight entitles it to,
	// among the teams with work.
	FairShare float64 `json:"fair_share"`
}
