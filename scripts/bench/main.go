// Bench floods a running stack with jobs and reports how fast they drain.
//
//	go run ./scripts/bench -jobs 10000 -teams 3
//
// It submits through the api like any app would, then reads Postgres directly for
// the timings, the WAL flushes, and what busy connections were waiting on meanwhile.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

const batchSize = 500

type bench struct {
	api, admin string
	db         *pgxpool.Pool
}

func main() {
	var (
		api     = flag.String("api", "http://localhost:8080", "the api's address")
		dbURL   = flag.String("db", "postgres://tasklane:tasklane@localhost:5432/tasklane?sslmode=disable", "Postgres, to read the results")
		admin   = flag.String("admin", cmp.Or(os.Getenv("ADMIN_TOKEN"), "dev-admin-token"), "the admin token")
		jobs    = flag.Int("jobs", 10_000, "how many jobs to flood")
		teams   = flag.Int("teams", 1, "how many teams to split them across")
		jobType = flag.String("type", "image.resize", "the job type")
		payload = flag.String("payload", `{"source":"sample:gradient","sizes":[320,640],"format":"jpeg"}`, "each job's payload")
	)
	flag.Parse()

	ctx := context.Background()
	db, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	b := &bench{api: strings.TrimSuffix(*api, "/"), admin: *admin, db: db}
	if err := b.run(ctx, *jobs, *teams, *jobType, json.RawMessage(*payload)); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}

func (b *bench) run(ctx context.Context, jobs, teams int, jobType string, payload json.RawMessage) error {
	keys, err := b.teamKeys(teams)
	if err != nil {
		return err
	}
	if err := b.waitIdle(ctx); err != nil {
		return err
	}

	var after int64
	if err := b.db.QueryRow(ctx, `select coalesce(max(id), 0) from jobs`).Scan(&after); err != nil {
		return err
	}
	flushesBefore, err := b.walFlushes(ctx)
	if err != nil {
		return err
	}
	sampleCtx, stopSampling := context.WithCancel(ctx)
	samples := make(chan waits, 1)
	go func() { samples <- b.sample(sampleCtx) }()

	start := time.Now()
	if err := b.flood(keys, jobs, jobType, payload); err != nil {
		stopSampling()
		return err
	}
	submitted := time.Since(start)
	err = b.waitDrained(ctx, after)
	stopSampling()
	if err != nil {
		return err
	}
	flushesAfter, err := b.walFlushes(ctx)
	if err != nil {
		return err
	}
	return b.report(ctx, after, jobType, teams, submitted, flushesAfter-flushesBefore, <-samples)
}

// teamKeys puts the default team and n-1 others on Enterprise, so a 10k flood fits
// their rate limits, and makes each a fresh key, which the auth cache hasn't seen.
func (b *bench) teamKeys(n int) ([]string, error) {
	var list struct {
		Teams []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := b.call("GET", "/api/teams", b.admin, nil, &list); err != nil {
		return nil, err
	}
	ids := map[string]int64{}
	for _, t := range list.Teams {
		ids[t.Name] = t.ID
	}

	var keys []string
	for i := range n {
		name := "default"
		if i > 0 {
			name = fmt.Sprintf("bench-%d", i+1)
		}
		id, ok := ids[name]
		if !ok {
			var t struct {
				ID int64 `json:"id"`
			}
			if err := b.call("POST", "/api/teams", b.admin, map[string]string{"name": name, "plan": "enterprise"}, &t); err != nil {
				return nil, err
			}
			id = t.ID
		}
		path := "/api/teams/" + strconv.FormatInt(id, 10)
		if err := b.call("PUT", path+"/plan", b.admin, map[string]string{"plan": "enterprise"}, nil); err != nil {
			return nil, err
		}
		var key struct {
			Key string `json:"key"`
		}
		if err := b.call("POST", path+"/keys", b.admin, nil, &key); err != nil {
			return nil, err
		}
		keys = append(keys, key.Key)
	}
	return keys, nil
}

// waitIdle waits for an earlier run's jobs to finish, so runs don't overlap.
func (b *bench) waitIdle(ctx context.Context) error {
	return poll(ctx, 5*time.Minute, "the queue to empty", func() (bool, error) {
		var n int
		err := b.db.QueryRow(ctx, `select count(*) from jobs where status in ('queued', 'running', 'failed')`).Scan(&n)
		return n == 0, err
	})
}

// flood splits the jobs evenly across the teams' keys and sends them in batches,
// a few at a time.
func (b *bench) flood(keys []string, jobs int, jobType string, payload json.RawMessage) error {
	var g errgroup.Group
	g.SetLimit(4)
	for i, key := range keys {
		share := jobs / len(keys)
		if i < jobs%len(keys) {
			share++
		}
		for sent := 0; sent < share; sent += batchSize {
			batch := make([]map[string]any, min(batchSize, share-sent))
			for j := range batch {
				batch[j] = map[string]any{"type": jobType, "payload": payload}
			}
			g.Go(func() error {
				return b.call("POST", "/api/jobs/batch", key, map[string]any{"jobs": batch}, nil)
			})
		}
	}
	return g.Wait()
}

func (b *bench) waitDrained(ctx context.Context, after int64) error {
	return poll(ctx, 15*time.Minute, "the flood to drain", func() (bool, error) {
		var n int
		err := b.db.QueryRow(ctx,
			`select count(*) from jobs where id > $1 and status in ('queued', 'running', 'failed')`, after).Scan(&n)
		return n == 0, err
	})
}

// walFlushes counts the WAL syncs that commits waited for. Commits that wait
// together share one, so fewer per job means they weren't queueing one by one.
func (b *bench) walFlushes(ctx context.Context) (int64, error) {
	var n int64
	err := b.db.QueryRow(ctx, `select wal_sync from pg_stat_wal`).Scan(&n)
	return n, err
}

// waits counts, over every sample, what the busy connections were waiting on.
// "CPU" is a connection running rather than waiting.
type waits struct {
	samples    int
	byEvent    map[string]int
	maxWorkers int
}

func (b *bench) sample(ctx context.Context) waits {
	w := waits{byEvent: map[string]int{}}
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return w
		case <-t.C:
		}
		rows, err := b.db.Query(ctx, `
			select coalesce(wait_event_type || ':' || wait_event, 'CPU'), count(*) from pg_stat_activity
			where datname = current_database() and state = 'active' and pid <> pg_backend_pid()
			group by 1`)
		if err != nil {
			continue
		}
		for rows.Next() {
			var (
				event string
				n     int
			)
			if rows.Scan(&event, &n) == nil {
				w.byEvent[event] += n
			}
		}
		rows.Close()
		var workers int
		if b.db.QueryRow(ctx, `select count(*) from workers where state = 'running'`).Scan(&workers) == nil {
			w.maxWorkers = max(w.maxWorkers, workers)
		}
		w.samples++
	}
}

func (b *bench) report(ctx context.Context, after int64, jobType string, teams int, submitted time.Duration, flushes int64, w waits) error {
	var (
		n             int
		drain         float64
		p50, p95, p99 float64
		run, between  float64
	)
	err := b.db.QueryRow(ctx, `
		select count(*), extract(epoch from max(finished_at) - min(created_at))::float8,
			percentile_cont(0.5) within group (order by extract(epoch from started_at - created_at)),
			percentile_cont(0.95) within group (order by extract(epoch from started_at - created_at)),
			percentile_cont(0.99) within group (order by extract(epoch from started_at - created_at))
		from jobs where id > $1`,
		after).Scan(&n, &drain, &p50, &p95, &p99)
	if err != nil {
		return err
	}
	// A worker's time per job, split at the finish. Both ends are transaction
	// start times, so a grab's wait for its team's row counts in the first part.
	err = b.db.QueryRow(ctx, `
		with started as (
			select e.at, j.finished_at, split_part(e.message, ' on ', 2) as worker
			from job_events e join jobs j on j.id = e.job_id
			where e.job_id > $1 and e.kind = 'started'
		), gaps as (
			select at, finished_at, at - lag(finished_at) over (partition by worker order by at) as gap
			from started
		)
		select avg(extract(epoch from finished_at - at))::float8 * 1000, avg(extract(epoch from gap))::float8 * 1000
		from gaps where gap < interval '1 second'`,
		after).Scan(&run, &between)
	if err != nil {
		return err
	}

	fmt.Printf("%d %s jobs from %d team(s), submitted in %.1fs\n", n, jobType, teams, submitted.Seconds())
	fmt.Printf("drained in %.1fs: %.0f jobs/s, up to %d workers\n", drain, float64(n)/drain, w.maxWorkers)
	fmt.Printf("queue wait p50 %.1fs, p95 %.1fs, p99 %.1fs\n", p50, p95, p99)
	fmt.Printf("per job on a worker: %.1fms from grab to finish, %.1fms from there to the next grab\n", run, between)
	fmt.Printf("%d WAL flushes, %.2f per job\n", flushes, float64(flushes)/float64(n))

	rows, err := b.db.Query(ctx, `
		select t.name, count(*), extract(epoch from max(j.finished_at) - min(j.created_at))::float8
		from jobs j join teams t on t.id = j.team_id
		where j.id > $1 group by t.name order by t.name`,
		after)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			name  string
			jobs  int
			taken float64
		)
		if err := rows.Scan(&name, &jobs, &taken); err != nil {
			return err
		}
		if teams > 1 {
			fmt.Printf("  %-10s %5d jobs in %.1fs\n", name, jobs, taken)
		}
	}
	rows.Close()

	total := 0
	for _, c := range w.byEvent {
		total += c
	}
	fmt.Printf("busy connections, %d samples 100ms apart, %.1f busy on average:\n", w.samples, float64(total)/float64(max(w.samples, 1)))
	events := slices.SortedFunc(maps.Keys(w.byEvent), func(a, b string) int { return w.byEvent[b] - w.byEvent[a] })
	for _, e := range events[:min(len(events), 8)] {
		fmt.Printf("  %-28s %5.1f%%\n", e, 100*float64(w.byEvent[e])/float64(total))
	}
	return nil
}

func (b *bench) call(method, path, token string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	for {
		req, err := http.NewRequest(method, b.api+path, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		// A rate limit says when to come back; anything else is the run's end.
		if wait, _ := strconv.Atoi(resp.Header.Get("Retry-After")); resp.StatusCode == http.StatusTooManyRequests && wait > 0 {
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(data))
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

func poll(ctx context.Context, timeout time.Duration, what string, done func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := done()
		if err != nil || ok {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for " + what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
