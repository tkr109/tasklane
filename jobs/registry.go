package jobs

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// Progress reports how far along a job is, from 0 to 100.
type Progress func(percent int)

// Handler runs one job. A non-nil result is stored as JSON on the succeeded job.
type Handler func(ctx context.Context, payload json.RawMessage, progress Progress) (result any, err error)

type Type struct {
	Handler Handler
	Timeout time.Duration
}

// Registry maps a job type name to the code that runs it.
type Registry map[string]Type

func Defaults() Registry {
	return Registry{
		"sleep": {Handler: Sleep, Timeout: 5 * time.Minute},
		"flaky": {Handler: Flaky, Timeout: 30 * time.Second},
	}
}

func (r Registry) Names() []string {
	return slices.Sorted(maps.Keys(r))
}
