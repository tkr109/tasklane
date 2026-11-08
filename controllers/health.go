package controllers

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

func Healthz(ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			slog.Warn("health check failed", "err", err)
			writeError(w, http.StatusServiceUnavailable, "database unreachable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
