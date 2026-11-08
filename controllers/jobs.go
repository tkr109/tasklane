package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/services"
)

const maxBodyBytes = 1 << 20

type Jobs struct {
	queue *services.Queue
	// Every request runs as this team until API keys land.
	teamID int64
}

func NewJobs(queue *services.Queue, teamID int64) *Jobs {
	return &Jobs{queue: queue, teamID: teamID}
}

type jobRequest struct {
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int            `json:"priority"`
	MaxAttempts *int            `json:"max_attempts"`
}

func (r jobRequest) spec() services.JobSpec {
	return services.JobSpec{
		Type:        r.Type,
		Payload:     r.Payload,
		Priority:    r.Priority,
		MaxAttempts: r.MaxAttempts,
	}
}

type jobDetail struct {
	models.Job
	Timeline []models.JobEvent `json:"timeline"`
}

func (h *Jobs) Submit(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if !decode(w, r, &req) {
		return
	}
	job, created, err := h.queue.Submit(r.Context(), h.teamID, req.spec(), r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, job)
}

func (h *Jobs) SubmitBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Jobs []jobRequest `json:"jobs"`
	}
	if !decode(w, r, &req) {
		return
	}
	specs := make([]services.JobSpec, len(req.Jobs))
	for i, j := range req.Jobs {
		specs[i] = j.spec()
	}
	created, err := h.queue.SubmitBatch(r.Context(), h.teamID, specs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"jobs": created})
}

func (h *Jobs) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := models.JobFilter{
		Status: models.Status(q.Get("status")),
		Type:   q.Get("type"),
	}
	var err error
	if v := q.Get("cursor"); v != "" {
		if f.Before, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "cursor is not valid")
			return
		}
	}
	if v := q.Get("limit"); v != "" {
		if f.Limit, err = strconv.Atoi(v); err != nil {
			writeError(w, http.StatusBadRequest, "limit must be a number")
			return
		}
	}

	list, next, err := h.queue.List(r.Context(), h.teamID, f)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	resp := map[string]any{"jobs": list, "next_cursor": ""}
	if next > 0 {
		resp["next_cursor"] = strconv.FormatInt(next, 10)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Jobs) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, events, err := h.queue.Get(r.Context(), h.teamID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobDetail{Job: job, Timeline: events})
}

func (h *Jobs) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, err := h.queue.Cancel(r.Context(), h.teamID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Jobs) Retry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, err := h.queue.Retry(r.Context(), h.teamID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, services.ErrNotFound.Error())
		return 0, false
	}
	return id, true
}
