package services

import (
	"context"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

// Monitor answers the dashboard's questions about the system as a whole.
type Monitor struct {
	workers *repositories.Workers
}

func NewMonitor(workers *repositories.Workers) *Monitor {
	return &Monitor{workers: workers}
}

func (m *Monitor) Workers(ctx context.Context) ([]models.Worker, error) {
	return m.workers.List(ctx)
}
