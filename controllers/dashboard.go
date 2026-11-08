package controllers

import (
	"net/http"

	"github.com/tkr109/tasklane/services"
)

// Dashboard serves the admin reads behind the dashboard's overview, workers and
// dead jobs pages.
type Dashboard struct {
	monitor *services.Monitor
}

func NewDashboard(monitor *services.Monitor) *Dashboard {
	return &Dashboard{monitor: monitor}
}

func (h *Dashboard) Workers(w http.ResponseWriter, r *http.Request) {
	workers, err := h.monitor.Workers(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// The scaler will log its decisions here; until it exists there are none.
	writeJSON(w, http.StatusOK, map[string]any{"workers": workers, "decisions": []any{}})
}
