package routes

import (
	"context"
	"net/http"

	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/middleware"
)

func New(ping func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", controllers.Healthz(ping))
	return middleware.Logging(mux)
}
