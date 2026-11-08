package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tkr109/tasklane/services"
)

// APIKey lets a request through only with a live key in "Authorization: Bearer",
// and puts the key's team in the request context.
func APIKey(auth *services.Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		team, err := auth.Team(r.Context(), bearer(r))
		if errors.Is(err, services.ErrUnauthorized) {
			unauthorized(w, "missing or invalid API key")
			return
		}
		if err != nil {
			slog.Error("resolve api key", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		next.ServeHTTP(w, r.WithContext(services.WithTeam(r.Context(), team)))
	})
}

// Admin guards the dashboard's admin endpoints with one shared token. With no
// token configured they are switched off.
func Admin(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			writeError(w, http.StatusForbidden, "admin endpoints are off, set ADMIN_TOKEN to use them")
			return
		}
		if subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(token)) != 1 {
			unauthorized(w, "missing or invalid admin token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, msg)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("write response", "err", err)
	}
}
