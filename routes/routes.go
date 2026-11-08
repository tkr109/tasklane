package routes

import (
	"context"
	"net/http"

	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/middleware"
)

func New(jobs *controllers.Jobs, ping func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", controllers.Healthz(ping))

	mux.HandleFunc("POST /api/jobs", jobs.Submit)
	mux.HandleFunc("POST /api/jobs/batch", jobs.SubmitBatch)
	mux.HandleFunc("GET /api/jobs", jobs.List)
	mux.HandleFunc("GET /api/jobs/{id}", jobs.Get)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", jobs.Cancel)
	mux.HandleFunc("POST /api/jobs/{id}/retry", jobs.Retry)

	return middleware.Logging(mux)
}
