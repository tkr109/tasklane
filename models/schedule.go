package models

import (
	"encoding/json"
	"time"
)

// Schedule queues a job from its template every time its cron expression fires.
type Schedule struct {
	ID          int64           `json:"id"`
	TeamID      int64           `json:"team_id"`
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	MaxAttempts int             `json:"max_attempts"`
	Enabled     bool            `json:"enabled"`
	NextRunAt   time.Time       `json:"next_run_at"`
	LastRunAt   *time.Time      `json:"last_run_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
