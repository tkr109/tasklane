package controllers

import (
	"net/http"
	"strconv"

	"github.com/tkr109/tasklane/services"
)

// Dashboard serves the admin reads behind the dashboard's overview, workers and
// dead jobs pages.
type Dashboard struct {
	monitor *services.Monitor
	dead    *services.DeadJobs
}

func NewDashboard(monitor *services.Monitor, dead *services.DeadJobs) *Dashboard {
	return &Dashboard{monitor: monitor, dead: dead}
}

func (h *Dashboard) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.monitor.Stats(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Dashboard) Workers(w http.ResponseWriter, r *http.Request) {
	workers, err := h.monitor.Workers(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	decisions, err := h.monitor.Decisions(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": workers, "decisions": decisions})
}

func (h *Dashboard) DeadGroups(w http.ResponseWriter, r *http.Request) {
	groups, total, err := h.dead.Groups(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "total": total})
}

func (h *Dashboard) DeadGroupJobs(w http.ResponseWriter, r *http.Request) {
	var limit int
	if v := r.URL.Query().Get("limit"); v != "" {
		var err error
		if limit, err = strconv.Atoi(v); err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a number")
			return
		}
	}
	jobs, err := h.dead.Jobs(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *Dashboard) RetryDeadGroup(w http.ResponseWriter, r *http.Request) {
	n, err := h.dead.Retry(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"retried": n})
}
