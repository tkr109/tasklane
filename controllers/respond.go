package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

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
	var later *services.TryLater
	switch {
	case errors.As(err, &later):
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(later.After.Seconds())))))
		status := http.StatusTooManyRequests
		if errors.Is(err, services.ErrOverloaded) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
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
