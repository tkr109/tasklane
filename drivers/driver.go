// Package drivers starts and stops worker processes for the scaler. Docker is the
// real one; a Kubernetes driver would sit next to it.
package drivers

import (
	"context"
	"time"
)

// Driver is how the scaler adds and removes workers. A worker started as name
// uses name as its id, so its heartbeats can be matched to it.
type Driver interface {
	// List returns the workers the driver has started and not yet removed,
	// including any that have exited.
	List(ctx context.Context) ([]Instance, error)
	Start(ctx context.Context, name string) error
	// Stop asks a worker to finish its jobs and exit, waits for that, and removes
	// it. Stopping one that has already exited just removes it.
	Stop(ctx context.Context, name string) error
}

// Instance is one worker a driver runs.
type Instance struct {
	Name    string
	Running bool
	// Status is the driver's own account of it, such as "Exited (1) 3 seconds ago".
	Status  string
	Created time.Time
}
