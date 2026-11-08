package controllers

import (
	"net/http"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/services"
)

// Teams serves the admin endpoints behind the dashboard's Teams page.
type Teams struct {
	teams *services.Teams
}

func NewTeams(teams *services.Teams) *Teams {
	return &Teams{teams: teams}
}

type newKeyResponse struct {
	models.APIKey
	Key string `json:"key"`
}

func (h *Teams) Plans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.teams.Plans(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (h *Teams) List(w http.ResponseWriter, r *http.Request) {
	teams, err := h.teams.List(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (h *Teams) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	if !decode(w, r, &req) {
		return
	}
	team, err := h.teams.Create(r.Context(), req.Name, req.Plan)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, team)
}

func (h *Teams) SwitchPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Plan string `json:"plan"`
	}
	if !decode(w, r, &req) {
		return
	}
	team, err := h.teams.SwitchPlan(r.Context(), id, req.Plan)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (h *Teams) Keys(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	keys, err := h.teams.Keys(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (h *Teams) CreateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	key, secret, err := h.teams.CreateKey(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newKeyResponse{APIKey: key, Key: secret})
}

func (h *Teams) RevokeKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	keyID, ok := pathID(w, r, "key")
	if !ok {
		return
	}
	key, err := h.teams.RevokeKey(r.Context(), id, keyID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, key)
}
