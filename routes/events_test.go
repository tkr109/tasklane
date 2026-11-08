package routes

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sseEvent struct {
	id, name, data string
}

// sseStream reads a /api/events response in the background.
type sseStream struct {
	t      *testing.T
	events chan sseEvent
}

// stream opens the event stream with the admin token in the URL, as EventSource
// does. lastEventID, if set, goes in the header a reconnecting browser sends.
func (a *api) stream(lastEventID string) *sseStream {
	a.t.Helper()
	waitUntil(a.t, 5*time.Second, "the api to listen for events", a.events.Listening)

	req, err := http.NewRequest("GET", a.srv.URL+"/api/events?token="+adminToken, nil)
	if err != nil {
		a.t.Fatal(err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := a.srv.Client().Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		resp.Body.Close()
		a.t.Fatalf("open the stream: status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	a.t.Cleanup(func() { resp.Body.Close() })

	s := &sseStream{t: a.t, events: make(chan sseEvent, 1000)}
	go func() {
		defer close(s.events)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.name != "" {
					s.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return s
}

// take returns the next event, whatever it is.
func (s *sseStream) take() sseEvent {
	s.t.Helper()
	select {
	case ev, ok := <-s.events:
		if !ok {
			s.t.Fatal("the stream ended")
		}
		return ev
	case <-time.After(5 * time.Second):
		s.t.Fatal("no event within 5s")
	}
	return sseEvent{}
}

// next skips ahead to the next event with the given name for which match, if set,
// returns true.
func (s *sseStream) next(name string, match func(data string) bool) sseEvent {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ev := s.take(); ev.name == name && (match == nil || match(ev.data)) {
			return ev
		}
	}
	s.t.Fatalf("no matching %s event within 5s", name)
	return sseEvent{}
}

type jobUpdate struct {
	ID        int64  `json:"id"`
	TeamID    int64  `json:"team_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Attempts  int    `json:"attempts"`
	LockedBy  string `json:"locked_by"`
	LastError string `json:"last_error"`
}

func decodeJob(t *testing.T, data string) jobUpdate {
	t.Helper()
	var j jobUpdate
	if err := json.Unmarshal([]byte(data), &j); err != nil {
		t.Fatalf("job event %s: %v", data, err)
	}
	return j
}

// jobIs matches job events of one job in one status.
func jobIs(t *testing.T, id int64, status string) func(string) bool {
	return func(data string) bool {
		j := decodeJob(t, data)
		return j.ID == id && j.Status == status
	}
}

func (a *api) submit(key string, body any) int64 {
	a.t.Helper()
	var job struct {
		ID int64 `json:"id"`
	}
	if code := a.call("POST", "/api/jobs", key, body, &job); code != http.StatusCreated {
		a.t.Fatalf("submit: status %d", code)
	}
	return job.ID
}

func TestTheStreamNeedsTheAdminToken(t *testing.T) {
	a := newAPI(t, time.Minute)
	_, key := a.createTeam("acme", "free")

	get := func(path, header string) int {
		req, err := http.NewRequest("GET", a.srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if header != "" {
			req.Header.Set("Authorization", "Bearer "+header)
		}
		resp, err := a.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/api/events", ""); code != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", code)
	}
	if code := get("/api/events?token=wrong", ""); code != http.StatusUnauthorized {
		t.Errorf("a wrong token: status %d, want 401", code)
	}
	if code := get("/api/events?token="+key.Key, ""); code != http.StatusUnauthorized {
		t.Errorf("an API key: status %d, want 401", code)
	}
	if code := get("/api/teams?token="+adminToken, ""); code != http.StatusUnauthorized {
		t.Errorf("the token in the URL of another admin route: status %d, want 401", code)
	}
	a.stream("") // the token in the URL works for the stream itself
}

func TestEventsReachEveryAPI(t *testing.T) {
	a := newAPI(t, time.Minute)
	b := newAPI(t, time.Minute)
	_, key := a.createTeam("acme", "free")
	watching := b.stream("")

	id := a.submit(key.Key, map[string]any{"type": "sleep", "priority": 7})
	ev := watching.next("job", jobIs(t, id, "queued"))
	if j := decodeJob(t, ev.data); j.Type != "sleep" || j.Attempts != 0 || j.Progress != 0 {
		t.Fatalf("queued event: %+v", j)
	}

	path := "/api/jobs/" + strconv.FormatInt(id, 10) + "/cancel"
	if code := a.call("POST", path, key.Key, nil, nil); code != http.StatusOK {
		t.Fatalf("cancel: status %d", code)
	}
	watching.next("job", jobIs(t, id, "cancelled"))
}

func TestAReconnectReplaysWhatItMissed(t *testing.T) {
	a := newAPI(t, time.Minute)
	_, key := a.createTeam("acme", "free")
	sleep := map[string]string{"type": "sleep"}

	first := a.stream("")
	seen := first.next("job", jobIs(t, a.submit(key.Key, sleep), "queued"))

	// The browser drops off. Another stream shows when the next three are in.
	watching := a.stream("")
	var missed []int64
	for range 3 {
		id := a.submit(key.Key, sleep)
		missed = append(missed, id)
		watching.next("job", jobIs(t, id, "queued"))
	}

	again := a.stream(seen.id)
	for _, id := range missed {
		ev := again.take()
		if j := decodeJob(t, ev.data); ev.name != "job" || j.ID != id {
			t.Fatalf("replayed %s %s, want job %d", ev.name, ev.data, id)
		}
	}

	// Another process can't replay this one's events, and says so.
	other := newAPI(t, time.Minute)
	if ev := other.stream(seen.id).take(); ev.name != "reset" {
		t.Fatalf("a reconnect to another api got %s first, want reset", ev.name)
	}
}
