package routes

import (
	"net/http"

	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/middleware"
)

func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", controllers.Healthz)
	return middleware.Logging(mux)
}
