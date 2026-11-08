package models

import "time"

// Demand is the work waiting for workers, as the scaler sees it.
type Demand struct {
	// Runnable is the jobs that could start now, leaving out the types a rate
	// limit is holding back, as more workers wouldn't start those any sooner.
	Runnable int
	// Held is how many waiting jobs were left out for that reason.
	Held       int
	OldestWait time.Duration
	// Slots is the most jobs the teams could run at once: each team's waiting
	// and running jobs, up to its plan's cap.
	Slots int
}

// ScalerStatus is what the scaler last reported, for the overview.
type ScalerStatus struct {
	// Current is the workers running or starting, less those draining.
	Current         int     `json:"current"`
	Target          int     `json:"target"`
	Min             int     `json:"min"`
	Max             int     `json:"max"`
	JobsPerWorker   int     `json:"jobs_per_worker"`
	IntervalSeconds float64 `json:"interval_seconds"`
	CooldownSeconds float64 `json:"cooldown_seconds"`
	StepDownSeconds float64 `json:"step_down_seconds"`
	// ScaleDownAt is when the cool-down ends, while the scaler wants fewer workers.
	ScaleDownAt *time.Time `json:"scale_down_at"`
}

// Decision is one change the scaler made or held off making.
type Decision struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	From   int       `json:"from"`
	To     int       `json:"to"`
	Reason string    `json:"reason"`
}
