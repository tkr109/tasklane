package routes

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/tkr109/tasklane/models"
)

func (a *api) stats() models.Stats {
	a.t.Helper()
	var stats models.Stats
	if code := a.call("GET", "/api/stats", adminToken, nil, &stats); code != http.StatusOK {
		a.t.Fatalf("stats: status %d", code)
	}
	return stats
}

func TestStatsCountTheQueue(t *testing.T) {
	a := newAPI(t, time.Minute)
	acme, _ := a.createTeam("acme", "pro")
	ctx := context.Background()

	if _, err := pgPool.Exec(ctx, `
		insert into jobs (team_id, type, status, run_at, ready_at, finished_at) values
			($1, 'sleep', 'queued', now() - interval '1 minute', now() - interval '1 minute', null),
			($1, 'sleep', 'queued', now(), now(), null),
			($1, 'sleep', 'queued', now() + interval '1 hour', now() + interval '1 hour', null),
			($1, 'flaky', 'failed', now() - interval '1 second', now() - interval '1 second', null),
			($1, 'flaky', 'failed', now() + interval '1 minute', now() + interval '1 minute', null),
			($1, 'sleep', 'succeeded', now(), now(), now()),
			($1, 'sleep', 'succeeded', now(), now(), now()),
			($1, 'sleep', 'succeeded', now(), now(), date_trunc('day', now()) - interval '1 second'),
			($1, 'sleep', 'dead', now(), now(), now()),
			($1, 'flaky', 'dead', now(), now(), now())`,
		acme.ID); err != nil {
		t.Fatal(err)
	}
	// Going through running fires the trigger that counts the team's running jobs.
	if _, err := pgPool.Exec(ctx, `
		update jobs set status = 'running' where id = (
			select id from jobs where status = 'queued' and run_at > now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := pgPool.Exec(ctx, `
		insert into workers (id, host, state) values ('w-1', 'h1', 'running'), ('w-2', 'h2', 'draining'), ('w-3', 'h3', 'starting')`); err != nil {
		t.Fatal(err)
	}

	stats := a.stats()
	want := models.Counts{Queued: 2, Running: 1, SucceededToday: 2, Failed: 2, Dead: 2}
	if stats.Counts != want {
		t.Errorf("counts %+v, want %+v", stats.Counts, want)
	}
	if stats.Runnable != 3 || stats.DeadGroups != 2 || stats.QueueCeiling != 0 {
		t.Errorf("runnable %d, dead groups %d, ceiling %d", stats.Runnable, stats.DeadGroups, stats.QueueCeiling)
	}
	if math.Abs(stats.OldestWaitSeconds-60) > 5 {
		t.Errorf("oldest wait %.1fs, want about 60", stats.OldestWaitSeconds)
	}
	if stats.Workers != (models.WorkerCounts{Running: 1, Starting: 1, Draining: 1}) {
		t.Errorf("workers %+v", stats.Workers)
	}
	if stats.Samples == nil {
		t.Error("samples is null, want a list")
	}
	types := map[string]models.TypeStats{}
	for _, ts := range stats.Types {
		types[ts.Type] = ts
	}
	if sleep := types["sleep"]; sleep.TimeoutSeconds != 300 || sleep.OutgoingPerSecond != nil {
		t.Errorf("sleep: %+v", sleep)
	}
	if flaky := types["flaky"]; flaky.OutgoingPerSecond == nil || *flaky.OutgoingPerSecond != 5 {
		t.Errorf("flaky: %+v", flaky)
	}
}

func TestTheLeaderPublishesStatsAndTeamUsage(t *testing.T) {
	leader := newAPI(t, time.Minute)
	other := newAPI(t, time.Minute)
	acme, key := leader.createTeam("acme", "free") // a burst of 200
	watching := other.stream("")

	if code := leader.call("POST", "/api/jobs/batch", key.Key, batchOf(150), nil); code != http.StatusCreated {
		t.Fatalf("batch of 150: status %d", code)
	}
	if code := leader.call("POST", "/api/jobs/batch", key.Key, batchOf(60), nil); code != http.StatusTooManyRequests {
		t.Fatalf("batch of 60 with 50 left: status %d, want 429", code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		leader.monitor.Run(ctx, func() bool { return true })
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	var tick models.StatsTick
	if err := json.Unmarshal([]byte(watching.next("stats", nil).data), &tick); err != nil {
		t.Fatal(err)
	}
	if tick.Counts.Queued != 150 || tick.Sample.Queued != 150 || tick.Sample.At.IsZero() {
		t.Errorf("stats tick: queued %d, sample %+v", tick.Counts.Queued, tick.Sample)
	}

	ev := watching.next("team_usage", func(data string) bool {
		var usage []models.TeamUsage
		if err := json.Unmarshal([]byte(data), &usage); err != nil {
			t.Fatal(err)
		}
		for _, u := range usage {
			if u.TeamID == acme.ID {
				return true
			}
		}
		return false
	})
	var usage []models.TeamUsage
	if err := json.Unmarshal([]byte(ev.data), &usage); err != nil {
		t.Fatal(err)
	}
	for _, u := range usage {
		if u.TeamID != acme.ID {
			continue
		}
		want := models.TeamUsage{TeamID: acme.ID, Queued: 150, JobsLastMinute: 150, RejectedLastMinute: 60, FairShare: 1}
		if u != want {
			t.Errorf("acme's usage %+v, want %+v", u, want)
		}
	}

	// The other api relays the same ticks, so it serves the same history.
	waitUntil(t, 2*time.Second, "the other api to keep a sample", func() bool {
		return len(other.stats().Samples) > 0
	})
}
