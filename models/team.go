package models

import "time"

type Plan struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ShareWeight   int    `json:"share_weight"`
	MaxWorkers    int    `json:"max_workers"`
	JobsPerMinute int    `json:"jobs_per_minute"`
	Burst         int    `json:"burst"`
}

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Plan      Plan      `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

// TeamSummary is a team as the admin pages list it.
type TeamSummary struct {
	Team
	Usage Usage `json:"usage"`
}

type Usage struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	// JobsLastMinute is what the team submitted in the last minute, to set
	// against its plan's jobs per minute.
	JobsLastMinute int `json:"jobs_last_minute"`
}

// APIKey never carries the key itself; only its hash is stored.
type APIKey struct {
	ID        int64      `json:"id"`
	TeamID    int64      `json:"team_id"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
