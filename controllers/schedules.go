package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/tkr109/tasklane/services"
)

// Schedules serves a team's repeating jobs. Like the jobs API, every handler
// works on the team of the request's key.
type Schedules struct {
	schedules *services.Schedules
}

func NewSchedules(schedules *services.Schedules) *Schedules {
	return &Schedules{schedules: schedules}
}

type scheduleRequest struct {
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int            `json:"priority"`
	MaxAttempts *int            `json:"max_attempts"`
	Enabled     *bool           `json:"enabled"`
}

func (r scheduleRequest) spec() services.ScheduleSpec {
	return services.ScheduleSpec{
		Cron:        r.Cron,
		Timezone:    r.Timezone,
		Type:        r.Type,
		Payload:     r.Payload,
		Priority:    r.Priority,
		MaxAttempts: r.MaxAttempts,
		Enabled:     r.Enabled,
	}
}

func (h *Schedules) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.schedules.List(r.Context(), teamID(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": list})
}

func (h *Schedules) Create(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if !decode(w, r, &req) {
		return
	}
	sc, err := h.schedules.Create(r.Context(), teamID(r), req.spec())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sc)
}

func (h *Schedules) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	sc, err := h.schedules.Get(r.Context(), teamID(r), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (h *Schedules) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req scheduleRequest
	if !decode(w, r, &req) {
		return
	}
	sc, err := h.schedules.Update(r.Context(), teamID(r), id, req.spec())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (h *Schedules) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.schedules.Delete(r.Context(), teamID(r), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
