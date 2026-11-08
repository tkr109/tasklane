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

// Jobs serves the API that apps call with their key. Every handler works on the
// team the auth middleware resolved.
type Jobs struct {
	queue  *services.Queue
	limits *services.Limits
}

func NewJobs(queue *services.Queue, limits *services.Limits) *Jobs {
	return &Jobs{queue: queue, limits: limits}
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
	if !h.admit(w, r, 1) {
		return
	}
	job, created, err := h.queue.Submit(r.Context(), teamID(r), req.spec(), r.Header.Get("Idempotency-Key"))
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
	if !h.admit(w, r, len(specs)) {
		return
	}
	created, err := h.queue.SubmitBatch(r.Context(), teamID(r), specs)
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

	list, next, err := h.queue.List(r.Context(), teamID(r), f)
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
	job, events, err := h.queue.Get(r.Context(), teamID(r), id)
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
	job, err := h.queue.Cancel(r.Context(), teamID(r), id)
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
	job, err := h.queue.Retry(r.Context(), teamID(r), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// admit charges n jobs to the team's rate limit and sets the rate limit headers.
// When the submit can't go ahead it writes the refusal and returns false.
func (h *Jobs) admit(w http.ResponseWriter, r *http.Request, n int) bool {
	quota, err := h.limits.Admit(r.Context(), services.TeamFrom(r.Context()), n)
	if quota.Limit > 0 {
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(quota.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(quota.Remaining))
	}
	if err != nil {
		writeServiceError(w, err)
		return false
	}
	return true
}

func teamID(r *http.Request) int64 {
	return services.TeamFrom(r.Context()).ID
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
