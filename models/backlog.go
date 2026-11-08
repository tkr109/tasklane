package models

import "time"

// Backlog is one priority level of a team's runnable jobs, with what the
// scheduler needs to decide whose turn it is.
type Backlog struct {
	TeamID      int64
	ShareWeight int
	Served      float64
	SinceServed time.Duration
	Priority    int
	// Waited is how long the oldest runnable job at this priority has waited.
	Waited time.Duration
}
