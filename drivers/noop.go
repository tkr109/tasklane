package drivers

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Noop runs nothing. It keeps the workers it was asked for in memory and logs
// what it would have done, for tests and for running the scaler outside Docker.
type Noop struct {
	mu        sync.Mutex
	instances map[string]Instance
}

func NewNoop() *Noop {
	return &Noop{instances: map[string]Instance{}}
}

func (d *Noop) List(context.Context) ([]Instance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	list := make([]Instance, 0, len(d.instances))
	for _, in := range d.instances {
		list = append(list, in)
	}
	slices.SortFunc(list, func(a, b Instance) int {
		return cmp.Or(a.Created.Compare(b.Created), cmp.Compare(a.Name, b.Name))
	})
	return list, nil
}

func (d *Noop) Start(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.instances[name] = Instance{Name: name, Running: true, Status: "pretending", Created: time.Now()}
	slog.Info("noop driver: would start a worker", "name", name)
	return nil
}

func (d *Noop) Stop(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.instances, name)
	slog.Info("noop driver: would stop a worker", "name", name)
	return nil
}
