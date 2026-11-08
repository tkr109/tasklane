package models

import "time"

// DeadGroup is the dead jobs of one type that failed the same way.
type DeadGroup struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Pattern is the error with ids, numbers and quoted values taken out; it is
	// what the jobs in a group share.
	Pattern string `json:"pattern"`
	// Error is the most recent error in the group, as it was.
	Error   string    `json:"error"`
	Count   int       `json:"count"`
	Teams   []string  `json:"teams"`
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
	// Hourly counts the jobs that died in each of the last 24 hours, oldest first.
	Hourly    []int   `json:"hourly"`
	SampleIDs []int64 `json:"sample_ids"`
}
