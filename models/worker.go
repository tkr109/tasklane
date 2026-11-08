package models

import "time"

// Worker is a worker process as its heartbeats describe it. Its running jobs are
// the ones locked by its id.
type Worker struct {
	ID          string       `json:"id"`
	Host        string       `json:"host"`
	State       string       `json:"state"`
	StartedAt   time.Time    `json:"started_at"`
	LastSeenAt  time.Time    `json:"last_seen_at"`
	JobsDone    int64        `json:"jobs_done"`
	RunningJobs []RunningJob `json:"running_jobs"`
}

type RunningJob struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Progress int    `json:"progress"`
}
