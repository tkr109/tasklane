package routes

import (
	"context"
	"net/http"

	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/middleware"
	"github.com/tkr109/tasklane/services"
)

func New(jobs *controllers.Jobs, schedules *controllers.Schedules, teams *controllers.Teams, dashboard *controllers.Dashboard, events *controllers.Events, auth *services.Auth, adminToken string, ping func(context.Context) error) http.Handler {
	key := func(h http.HandlerFunc) http.Handler { return middleware.APIKey(auth, h) }
	admin := func(h http.HandlerFunc) http.Handler { return middleware.Admin(adminToken, h) }

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", controllers.Healthz(ping))

	mux.Handle("POST /api/jobs", key(jobs.Submit))
	mux.Handle("POST /api/jobs/batch", key(jobs.SubmitBatch))
	mux.Handle("GET /api/jobs", key(jobs.List))
	mux.Handle("GET /api/jobs/{id}", key(jobs.Get))
	mux.Handle("POST /api/jobs/{id}/cancel", key(jobs.Cancel))
	mux.Handle("POST /api/jobs/{id}/retry", key(jobs.Retry))

	mux.Handle("GET /api/schedules", key(schedules.List))
	mux.Handle("POST /api/schedules", key(schedules.Create))
	mux.Handle("GET /api/schedules/{id}", key(schedules.Get))
	mux.Handle("PUT /api/schedules/{id}", key(schedules.Update))
	mux.Handle("DELETE /api/schedules/{id}", key(schedules.Delete))

	mux.Handle("GET /api/plans", admin(teams.Plans))
	mux.Handle("GET /api/teams", admin(teams.List))
	mux.Handle("POST /api/teams", admin(teams.Create))
	mux.Handle("PUT /api/teams/{id}/plan", admin(teams.SwitchPlan))
	mux.Handle("GET /api/teams/{id}/keys", admin(teams.Keys))
	mux.Handle("POST /api/teams/{id}/keys", admin(teams.CreateKey))
	mux.Handle("POST /api/teams/{id}/keys/{key}/revoke", admin(teams.RevokeKey))

	mux.Handle("GET /api/admin/jobs/{id}", admin(dashboard.Job))
	mux.Handle("POST /api/admin/jobs/{id}/cancel", admin(dashboard.CancelJob))
	mux.Handle("POST /api/admin/jobs/{id}/retry", admin(dashboard.RetryJob))
	mux.Handle("GET /api/stats", admin(dashboard.Stats))
	mux.Handle("GET /api/workers", admin(dashboard.Workers))
	mux.Handle("GET /api/dead-groups", admin(dashboard.DeadGroups))
	mux.Handle("GET /api/dead-groups/{id}/jobs", admin(dashboard.DeadGroupJobs))
	mux.Handle("POST /api/dead-groups/{id}/retry", admin(dashboard.RetryDeadGroup))
	mux.Handle("GET /api/events", middleware.AdminStream(adminToken, http.HandlerFunc(events.Stream)))

	return middleware.Logging(mux)
}
