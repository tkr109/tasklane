package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/services"
)

// Dashboard serves the admin reads behind the dashboard's overview, workers, dead
// jobs and job pages.
type Dashboard struct {
	monitor *services.Monitor
	dead    *services.DeadJobs
	queue   *services.Queue
}

func NewDashboard(monitor *services.Monitor, dead *services.DeadJobs, queue *services.Queue) *Dashboard {
	return &Dashboard{monitor: monitor, dead: dead, queue: queue}
}

// Job reads a job of any team, since workers and dead groups link to every team's.
func (h *Dashboard) Job(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, events, err := h.queue.Find(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobDetail{Job: job, Timeline: events})
}

func (h *Dashboard) CancelJob(w http.ResponseWriter, r *http.Request) {
	h.changeJob(w, r, h.queue.CancelAny)
}

func (h *Dashboard) RetryJob(w http.ResponseWriter, r *http.Request) {
	h.changeJob(w, r, h.queue.RetryAny)
}

func (h *Dashboard) changeJob(w http.ResponseWriter, r *http.Request, change func(context.Context, int64) (models.Job, error)) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, err := change(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
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
