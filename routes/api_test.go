package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/tkr109/tasklane/controllers"
	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/repositories"
	"github.com/tkr109/tasklane/services"
)

const adminToken = "test-admin-token"

var (
	pgOnce      sync.Once
	pgContainer *postgres.PostgresContainer
	pgPool      *pgxpool.Pool
	pgErr       error
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	code := m.Run()
	if pgPool != nil {
		pgPool.Close()
	}
	if pgContainer != nil {
		_ = testcontainers.TerminateContainer(pgContainer)
	}
	os.Exit(code)
}

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	pgOnce.Do(func() {
		ctx := context.Background()
		pgContainer, pgErr = postgres.Run(ctx, "postgres:16-alpine", postgres.BasicWaitStrategies())
		if pgErr != nil {
			return
		}
		url, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = err
			return
		}
		if pgPool, pgErr = repositories.Connect(ctx, url); pgErr != nil {
			return
		}
		pgErr = repositories.Migrate(ctx, pgPool)
	})
	if pgErr != nil {
		t.Fatalf("start postgres: %v", pgErr)
	}
	if _, err := pgPool.Exec(context.Background(), `
		truncate jobs, job_events, api_keys restart identity;
		delete from teams where name <> 'default'`); err != nil {
		t.Fatalf("reset tables: %v", err)
	}
	return pgPool
}

// api is the whole HTTP stack on a test server, plus the Auth behind it so tests
// can look at what it has cached.
type api struct {
	t    *testing.T
	srv  *httptest.Server
	auth *services.Auth
}

func newAPI(t *testing.T, cacheTTL time.Duration) *api {
	pool := testDB(t)
	keys := repositories.NewKeys(pool)
	auth := services.NewAuth(keys, cacheTTL)
	handler := New(
		controllers.NewJobs(services.NewQueue(repositories.NewJobs(pool), jobs.Defaults())),
		controllers.NewTeams(services.NewTeams(repositories.NewTeams(pool), keys, auth)),
		auth, adminToken, pool.Ping,
	)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &api{t: t, srv: srv, auth: auth}
}

// call sends body as JSON with token as the bearer, and decodes the reply into out
// if out is not nil.
func (a *api) call(method, path, token string, body, out any) int {
	a.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			a.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, a.srv.URL+path, &buf)
	if err != nil {
		a.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.srv.Client().Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			a.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

type team struct {
	ID   int64 `json:"id"`
	Plan struct {
		Name string `json:"name"`
	} `json:"plan"`
}

type newKey struct {
	ID  int64  `json:"id"`
	Key string `json:"key"`
}

func (a *api) createTeam(name, plan string) (team, newKey) {
	a.t.Helper()
	var tm team
	if code := a.call("POST", "/api/teams", adminToken, map[string]string{"name": name, "plan": plan}, &tm); code != http.StatusCreated {
		a.t.Fatalf("create team: status %d", code)
	}
	var key newKey
	if code := a.call("POST", teamPath(tm.ID)+"/keys", adminToken, nil, &key); code != http.StatusCreated {
		a.t.Fatalf("create key: status %d", code)
	}
	return tm, key
}

func teamPath(id int64) string {
	return "/api/teams/" + strconv.FormatInt(id, 10)
}

func waitUntil(t *testing.T, timeout time.Duration, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestKeysScopeJobsToTheirTeam(t *testing.T) {
	a := newAPI(t, time.Minute)
	acme, acmeKey := a.createTeam("acme", "free")
	_, otherKey := a.createTeam("globex", "pro")

	var job struct {
		ID     int64 `json:"id"`
		TeamID int64 `json:"team_id"`
	}
	if code := a.call("POST", "/api/jobs", acmeKey.Key, map[string]string{"type": "sleep"}, &job); code != http.StatusCreated {
		t.Fatalf("submit: status %d", code)
	}
	if job.TeamID != acme.ID {
		t.Fatalf("job belongs to team %d, want %d", job.TeamID, acme.ID)
	}
	jobPath := "/api/jobs/" + strconv.FormatInt(job.ID, 10)
	if code := a.call("GET", jobPath, acmeKey.Key, nil, nil); code != http.StatusOK {
		t.Fatalf("owner reading its job: status %d", code)
	}
	if code := a.call("GET", jobPath, otherKey.Key, nil, nil); code != http.StatusNotFound {
		t.Fatalf("another team reading the job: status %d, want 404", code)
	}
}

func TestBadAndRevokedKeysGet401(t *testing.T) {
	const ttl = 300 * time.Millisecond
	a := newAPI(t, time.Minute)
	acme, key := a.createTeam("acme", "free")
	// otherAPI stands in for a second API process with its own cache.
	otherAPI := services.NewAuth(repositories.NewKeys(pgPool), ttl)
	ctx := context.Background()

	for name, token := range map[string]string{
		"no key":          "",
		"wrong prefix":    "sk_live_0123456789",
		"unknown key":     "tk_live_0000000000000000000000000000000000000000",
		"the admin token": adminToken,
	} {
		if code := a.call("GET", "/api/jobs", token, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
	}
	if code := a.call("GET", "/api/teams", key.Key, nil, nil); code != http.StatusUnauthorized {
		t.Errorf("api key on an admin route: status %d, want 401", code)
	}

	if code := a.call("GET", "/api/jobs", key.Key, nil, nil); code != http.StatusOK {
		t.Fatalf("live key: status %d", code)
	}
	if _, err := otherAPI.Team(ctx, key.Key); err != nil {
		t.Fatalf("live key on the other API: %v", err)
	}
	revoke := teamPath(acme.ID) + "/keys/" + strconv.FormatInt(key.ID, 10) + "/revoke"
	if code := a.call("POST", revoke, adminToken, nil, nil); code != http.StatusOK {
		t.Fatalf("revoke: status %d", code)
	}
	if code := a.call("GET", "/api/jobs", key.Key, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked key on the API that revoked it: status %d, want 401 at once", code)
	}
	waitUntil(t, ttl+time.Second, "the other API to refuse the revoked key", func() bool {
		_, err := otherAPI.Team(ctx, key.Key)
		return errors.Is(err, services.ErrUnauthorized)
	})
}

func TestPlanSwitchAppliesWithinCacheTTL(t *testing.T) {
	const ttl = 500 * time.Millisecond
	a := newAPI(t, ttl)
	acme, key := a.createTeam("acme", "free")
	ctx := context.Background()

	planOf := func() string {
		tm, err := a.auth.Team(ctx, key.Key)
		if err != nil {
			t.Fatal(err)
		}
		return tm.Plan.Name
	}
	if got := planOf(); got != "free" {
		t.Fatalf("plan = %q, want free", got)
	}

	var switched team
	if code := a.call("PUT", teamPath(acme.ID)+"/plan", adminToken, map[string]string{"plan": "enterprise"}, &switched); code != http.StatusOK {
		t.Fatalf("switch plan: status %d", code)
	}
	if switched.Plan.Name != "enterprise" {
		t.Fatalf("switch returned plan %q", switched.Plan.Name)
	}
	if got := planOf(); got != "free" {
		t.Fatalf("plan = %q straight after the switch, want the cached free", got)
	}

	start := time.Now()
	waitUntil(t, 2*ttl, "the new plan to show", func() bool { return planOf() == "enterprise" })
	if waited := time.Since(start); waited > ttl+100*time.Millisecond {
		t.Errorf("the new plan took %v to show, the cache TTL is %v", waited, ttl)
	}
}

func TestAdminRoutes(t *testing.T) {
	a := newAPI(t, time.Minute)

	var plans struct {
		Plans []struct {
			Name        string `json:"name"`
			ShareWeight int    `json:"share_weight"`
			MaxWorkers  int    `json:"max_workers"`
		} `json:"plans"`
	}
	if code := a.call("GET", "/api/plans", adminToken, nil, &plans); code != http.StatusOK {
		t.Fatalf("list plans: status %d", code)
	}
	if len(plans.Plans) != 3 || plans.Plans[2].Name != "enterprise" || plans.Plans[2].ShareWeight != 10 || plans.Plans[2].MaxWorkers != 10 {
		t.Fatalf("plans = %+v", plans.Plans)
	}

	acme, _ := a.createTeam("acme", "pro")
	if code := a.call("POST", "/api/teams", adminToken, map[string]string{"name": "acme", "plan": "free"}, nil); code != http.StatusConflict {
		t.Errorf("duplicate team: status %d, want 409", code)
	}
	if code := a.call("POST", "/api/teams", adminToken, map[string]string{"name": "initech", "plan": "platinum"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown plan: status %d, want 400", code)
	}
	if code := a.call("PUT", "/api/teams/999999/plan", adminToken, map[string]string{"plan": "free"}, nil); code != http.StatusNotFound {
		t.Errorf("switching a missing team: status %d, want 404", code)
	}
	if code := a.call("POST", "/api/teams/999999/keys", adminToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("key for a missing team: status %d, want 404", code)
	}

	var keys struct {
		Keys []map[string]any `json:"keys"`
	}
	if code := a.call("GET", teamPath(acme.ID)+"/keys", adminToken, nil, &keys); code != http.StatusOK {
		t.Fatalf("list keys: status %d", code)
	}
	if len(keys.Keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys.Keys))
	}
	if _, leaked := keys.Keys[0]["key"]; leaked {
		t.Error("listing keys returned a full key")
	}

	var teams struct {
		Teams []struct {
			Name  string `json:"name"`
			Usage struct {
				Queued int `json:"queued"`
			} `json:"usage"`
		} `json:"teams"`
	}
	if code := a.call("GET", "/api/teams", adminToken, nil, &teams); code != http.StatusOK {
		t.Fatalf("list teams: status %d", code)
	}
	if len(teams.Teams) != 2 || teams.Teams[1].Name != "acme" {
		t.Fatalf("teams = %+v", teams.Teams)
	}

	off := httptest.NewServer(New(controllers.NewJobs(nil), controllers.NewTeams(nil), nil, "", nil))
	defer off.Close()
	resp, err := http.Get(off.URL + "/api/teams")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("admin route with no ADMIN_TOKEN: status %d, want 403", resp.StatusCode)
	}
}

func TestDevKeyWorksForTheDefaultTeam(t *testing.T) {
	a := newAPI(t, time.Minute)
	teams := services.NewTeams(repositories.NewTeams(pgPool), repositories.NewKeys(pgPool), a.auth)
	ctx := context.Background()

	const devKey = "tk_live_localdev"
	for range 2 {
		if err := teams.EnsureDevKey(ctx, devKey); err != nil {
			t.Fatalf("ensure dev key: %v", err)
		}
	}
	var job struct {
		TeamID int64 `json:"team_id"`
	}
	if code := a.call("POST", "/api/jobs", devKey, map[string]string{"type": "sleep"}, &job); code != http.StatusCreated {
		t.Fatalf("submit with the dev key: status %d", code)
	}
	defaultID, err := repositories.NewTeams(pgPool).IDByName(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if job.TeamID != defaultID {
		t.Fatalf("job went to team %d, want the default team %d", job.TeamID, defaultID)
	}

	// Someone revokes the dev key from the dashboard; the next startup brings it back.
	var keys struct {
		Keys []newKey `json:"keys"`
	}
	if code := a.call("GET", teamPath(defaultID)+"/keys", adminToken, nil, &keys); code != http.StatusOK || len(keys.Keys) != 1 {
		t.Fatalf("list the default team's keys: status %d, %d keys", code, len(keys.Keys))
	}
	revoke := teamPath(defaultID) + "/keys/" + strconv.FormatInt(keys.Keys[0].ID, 10) + "/revoke"
	if code := a.call("POST", revoke, adminToken, nil, nil); code != http.StatusOK {
		t.Fatalf("revoke the dev key: status %d", code)
	}
	if code := a.call("GET", "/api/jobs", devKey, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked dev key: status %d, want 401", code)
	}
	if err := teams.EnsureDevKey(ctx, devKey); err != nil {
		t.Fatalf("ensure dev key again: %v", err)
	}
	if code := a.call("GET", "/api/jobs", devKey, nil, nil); code != http.StatusOK {
		t.Fatalf("dev key after a restart: status %d, want 200", code)
	}

	if err := teams.EnsureDevKey(ctx, "localdev"); err == nil {
		t.Error("a dev key without the tk_live_ prefix was accepted")
	}
}
