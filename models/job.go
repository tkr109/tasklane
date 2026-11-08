package models

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusDead      Status = "dead"
	StatusCancelled Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusDead, StatusCancelled:
		return true
	}
	return false
}

type Job struct {
	ID             int64           `json:"id"`
	TeamID         int64           `json:"team_id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	Priority       int             `json:"priority"`
	Status         Status          `json:"status"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	RunAt          time.Time       `json:"run_at"`
	LeaseUntil     *time.Time      `json:"lease_until,omitempty"`
	LockedBy       string          `json:"locked_by,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Progress       int             `json:"progress"`
	LastError      string          `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
}

// NewJob is what gets inserted; everything else on Job is filled in by the database.
type NewJob struct {
	TeamID         int64
	Type           string
	Payload        json.RawMessage
	Priority       int
	MaxAttempts    int
	IdempotencyKey string
}

type JobEvent struct {
	ID      int64     `json:"id"`
	JobID   int64     `json:"job_id"`
	Kind    string    `json:"kind"`
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
}

type JobFilter struct {
	Status Status
	Type   string
	Before int64
	Limit  int
}
