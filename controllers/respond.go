package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tkr109/tasklane/services"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
