package routes

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type deadGroup struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Pattern   string    `json:"pattern"`
	Error     string    `json:"error"`
	Count     int       `json:"count"`
	Teams     []string  `json:"teams"`
	FirstAt   time.Time `json:"first_at"`
	LastAt    time.Time `json:"last_at"`
	Hourly    []int     `json:"hourly"`
	SampleIDs []int64   `json:"sample_ids"`
}

type deadGroups struct {
	Groups []deadGroup `json:"groups"`
	Total  int         `json:"total"`
}

func (a *api) deadGroups() deadGroups {
	a.t.Helper()
	var list deadGroups
	if code := a.call("GET", "/api/dead-groups", adminToken, nil, &list); code != http.StatusOK {
		a.t.Fatalf("list dead groups: status %d", code)
	}
	return list
}

// addJob inserts a job in the given state, finished the given time ago.
func addJob(t *testing.T, teamID int64, jobType, status, lastError string, ago time.Duration) int64 {
	t.Helper()
	var id int64
	if err := pgPool.QueryRow(context.Background(), `
		insert into jobs (team_id, type, status, attempts, last_error, finished_at)
		values ($1, $2, $3, 3, $4, now() - make_interval(secs => $5))
		returning id`,
		teamID, jobType, status, lastError, ago.Seconds()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDeadJobsAreGroupedByTypeAndError(t *testing.T) {
	a := newAPI(t, time.Minute)
	acme, _ := a.createTeam("acme", "free")
	globex, _ := a.createTeam("globex", "pro")

	if list := a.deadGroups(); len(list.Groups) != 0 || list.Groups == nil || list.Total != 0 {
		t.Fatalf("no dead jobs: %+v", list)
	}

	timeouts := []int64{
		addJob(t, acme.ID, "sleep", "dead", "connect to 10.0.0.5:443: i/o timeout", 3*time.Hour),
		addJob(t, globex.ID, "sleep", "dead", "connect to 10.0.0.9:443: i/o timeout", 2*time.Hour),
		addJob(t, acme.ID, "sleep", "dead", "connect to 10.0.0.7:443: i/o timeout", 30*time.Minute),
	}
	addJob(t, acme.ID, "flaky", "dead", "connect to 10.0.0.5:443: i/o timeout", time.Hour)
	addJob(t, globex.ID, "flaky", "dead", `user "bob" not found`, 10*time.Minute)
	addJob(t, globex.ID, "flaky", "dead", `user "alice" not found`, 5*time.Minute)
	// The jobs trigger cuts long text short, or this insert would fail to notify.
	addJob(t, acme.ID, "sleep", "dead", "boom "+strings.Repeat("x", 10_000), 30*time.Hour)
	// Jobs that aren't dead don't count, whatever their error.
	addJob(t, acme.ID, "sleep", "failed", "connect to 10.0.0.5:443: i/o timeout", time.Minute)
	addJob(t, acme.ID, "sleep", "succeeded", "", time.Minute)

	list := a.deadGroups()
	if len(list.Groups) != 4 || list.Total != 7 {
		t.Fatalf("got %d groups holding %d jobs, want 4 holding 7", len(list.Groups), list.Total)
	}
	byKey := map[string]deadGroup{}
	for _, g := range list.Groups {
		byKey[g.Type+" "+g.Pattern] = g
	}

	timeout, ok := byKey["sleep connect to <n>.<n>.<n>.<n>:<n>: i/o timeout"]
	if !ok {
		t.Fatalf("no group for the sleep timeouts in %+v", list.Groups)
	}
	if timeout.Count != 3 || !slices.Equal(timeout.Teams, []string{"acme", "globex"}) {
		t.Errorf("timeouts: count %d, teams %v", timeout.Count, timeout.Teams)
	}
	if timeout.Error != "connect to 10.0.0.7:443: i/o timeout" {
		t.Errorf("timeouts: error %q, want the latest", timeout.Error)
	}
	if want := []int64{timeouts[2], timeouts[1], timeouts[0]}; !slices.Equal(timeout.SampleIDs, want) {
		t.Errorf("timeouts: sample ids %v, want %v", timeout.SampleIDs, want)
	}
	if len(timeout.Hourly) != 24 || timeout.Hourly[23] != 1 || timeout.Hourly[21] != 1 || timeout.Hourly[20] != 1 {
		t.Errorf("timeouts: hourly %v, want one in each of the last, third and fourth hours", timeout.Hourly)
	}
	if age := time.Since(timeout.FirstAt); age < 3*time.Hour-time.Minute || age > 3*time.Hour+time.Minute {
		t.Errorf("timeouts: first_at %v ago, want 3h", age)
	}

	if g := byKey["flaky user <str> not found"]; g.Count != 2 || !slices.Equal(g.Teams, []string{"globex"}) {
		t.Errorf("missing users: %+v", g)
	}
	if g := byKey["flaky connect to <n>.<n>.<n>.<n>:<n>: i/o timeout"]; g.Count != 1 {
		t.Errorf("the same error under another type should be its own group: %+v", g)
	}
	var old deadGroup
	for _, g := range list.Groups {
		if strings.HasPrefix(g.Pattern, "boom") {
			old = g
		}
	}
	if old.Count != 1 || slices.Max(old.Hourly) != 0 || len(old.Pattern) != 200 {
		t.Errorf("a day-old group: count %d, hourly %v, pattern of %d chars", old.Count, old.Hourly, len(old.Pattern))
	}
	if list.Groups[0].Pattern != "user <str> not found" {
		t.Errorf("the most recent group isn't first: %q", list.Groups[0].Pattern)
	}

	var page struct {
		Jobs []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"jobs"`
	}
	if code := a.call("GET", "/api/dead-groups/"+timeout.ID+"/jobs?limit=2", adminToken, nil, &page); code != http.StatusOK {
		t.Fatalf("group jobs: status %d", code)
	}
	if len(page.Jobs) != 2 || page.Jobs[0].ID != timeouts[2] || page.Jobs[1].ID != timeouts[1] {
		t.Errorf("group jobs: %+v, want the two most recent", page.Jobs)
	}
}

func TestRetryingAGroupRequeuesExactlyThatGroup(t *testing.T) {
	a := newAPI(t, time.Minute)
	acme, _ := a.createTeam("acme", "free")
	var ids []int64
	for _, ip := range []string{"1", "2", "3"} {
		ids = append(ids, addJob(t, acme.ID, "sleep", "dead", "connect to 10.0.0."+ip+":443: refused", time.Minute))
	}
	other := addJob(t, acme.ID, "flaky", "dead", "connect to 10.0.0.1:443: refused", time.Minute)
	alsoOther := addJob(t, acme.ID, "sleep", "dead", "boom", time.Minute)

	var group deadGroup
	for _, g := range a.deadGroups().Groups {
		if g.Type == "sleep" && g.Count == 3 {
			group = g
		}
	}
	if group.ID == "" {
		t.Fatal("no group of three sleep jobs")
	}

	var retried struct {
		Retried int `json:"retried"`
	}
	if code := a.call("POST", "/api/dead-groups/"+group.ID+"/retry", adminToken, nil, &retried); code != http.StatusOK || retried.Retried != 3 {
		t.Fatalf("retry the group: status %d, retried %d", code, retried.Retried)
	}
	var queued, attempts int
	if err := pgPool.QueryRow(context.Background(), `
		select count(*) filter (where status = 'queued'), coalesce(sum(attempts), 0)
		from jobs where id = any($1)`, ids).Scan(&queued, &attempts); err != nil {
		t.Fatal(err)
	}
	if queued != 3 || attempts != 0 {
		t.Errorf("after the retry %d of 3 are queued with %d attempts between them", queued, attempts)
	}
	for _, id := range []int64{other, alsoOther} {
		var status string
		if err := pgPool.QueryRow(context.Background(), `select status from jobs where id = $1`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "dead" {
			t.Errorf("job %d outside the group is %s, want dead", id, status)
		}
	}

	if list := a.deadGroups(); len(list.Groups) != 2 || list.Total != 2 {
		t.Errorf("after the retry: %d groups holding %d jobs, want 2 and 2", len(list.Groups), list.Total)
	}
	if code := a.call("POST", "/api/dead-groups/"+group.ID+"/retry", adminToken, nil, &retried); code != http.StatusOK || retried.Retried != 0 {
		t.Errorf("retry it again: status %d, retried %d, want 200 and 0", code, retried.Retried)
	}
	for _, path := range []string{"/api/dead-groups/not-a-group/retry", "/api/dead-groups/@@/jobs"} {
		method := "POST"
		if strings.HasSuffix(path, "/jobs") {
			method = "GET"
		}
		if code := a.call(method, path, adminToken, nil, nil); code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", method, path, code)
		}
	}
}
