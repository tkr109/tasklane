package services

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/repositories"
)

// everySecond adds n schedules that fire every second, starting on the current
// whole second. That is faster than the API allows, which keeps these tests short.
func everySecond(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into schedules (team_id, cron, type, next_run_at)
		select $1, '@every 1s', 'sleep', date_trunc('second', now()) from generate_series(1, $2)`,
		defaultTeam(t, pool), n); err != nil {
		t.Fatal(err)
	}
}

// firedTicks reads which ticks each schedule has fired from its jobs' keys.
func firedTicks(t *testing.T, pool *pgxpool.Pool) map[int64][]int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(), `select idempotency_key from jobs order by id`)
	if err != nil {
		t.Fatal(err)
	}
	ticks := map[int64][]int64{}
	var key string
	for rows.Next() {
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(key, ":")
		if len(parts) != 3 || parts[0] != "schedule" {
			t.Fatalf("job key %q is not schedule:<id>:<tick>", key)
		}
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		tick, _ := strconv.ParseInt(parts[2], 10, 64)
		ticks[id] = append(ticks[id], tick)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ticks
}

// checkTicks fails if a schedule fired the same tick twice or went back in time.
func checkTicks(t *testing.T, ticks map[int64][]int64, atLeast int) {
	t.Helper()
	for id, list := range ticks {
		if len(list) < atLeast {
			t.Errorf("schedule %d fired %d times, want at least %d", id, len(list), atLeast)
		}
		for i := 1; i < len(list); i++ {
			if list[i] <= list[i-1] {
				t.Errorf("schedule %d fired tick %d after tick %d", id, list[i], list[i-1])
			}
		}
	}
}

func startScheduler(t *testing.T, pool *pgxpool.Pool) (s *Scheduler, stop func()) {
	t.Helper()
	s = NewScheduler(repositories.NewSchedules(pool), 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	stop = sync.OnceFunc(func() {
		cancel()
		<-done
	})
	t.Cleanup(stop)
	return s, stop
}

func TestOneSchedulerLeadsAndAnotherTakesOver(t *testing.T) {
	pool := testDB(t)
	everySecond(t, pool, 3)

	a, stopA := startScheduler(t, pool)
	b, stopB := startScheduler(t, pool)

	var bothLed atomic.Bool
	watchCtx, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go func() {
		for watchCtx.Err() == nil {
			if a.Leading() && b.Leading() {
				bothLed.Store(true)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	waitUntil(t, 5*time.Second, "a scheduler to lead", func() bool { return a.Leading() || b.Leading() })
	time.Sleep(2500 * time.Millisecond)

	other, stopLeader := b, stopA
	if b.Leading() {
		other, stopLeader = a, stopB
	}
	firedBefore := len(firedTicks(t, pool)[1])
	stopLeader()
	stopped := time.Now()
	waitUntil(t, 5*time.Second, "the other scheduler to take over", other.Leading)
	t.Logf("took over %v after the leader stopped", time.Since(stopped).Round(time.Millisecond))
	time.Sleep(2500 * time.Millisecond)
	stopA()
	stopB()

	if bothLed.Load() {
		t.Error("both schedulers led at the same time")
	}
	ticks := firedTicks(t, pool)
	if len(ticks) != 3 {
		t.Fatalf("%d schedules fired, want 3", len(ticks))
	}
	checkTicks(t, ticks, 4)
	if len(ticks[1]) <= firedBefore {
		t.Errorf("nothing fired after the takeover")
	}
}

func TestALeaderThatLosesItsConnectionStepsDown(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	everySecond(t, pool, 3)

	a, _ := startScheduler(t, pool)
	b, _ := startScheduler(t, pool)
	waitUntil(t, 5*time.Second, "a scheduler to lead", func() bool { return a.Leading() || b.Leading() })
	time.Sleep(1500 * time.Millisecond)

	// Postgres drops the leader's session, as it would if the process or its
	// network died. The only advisory lock held in this test is the scheduler's.
	var killed int
	if err := pool.QueryRow(ctx, `
		select count(pg_terminate_backend(pid)) from pg_locks
		where locktype = 'advisory' and granted`).Scan(&killed); err != nil {
		t.Fatal(err)
	}
	if killed != 1 {
		t.Fatalf("terminated %d sessions holding an advisory lock, want 1", killed)
	}
	waitUntil(t, 5*time.Second, "the old leader to notice", func() bool { return !(a.Leading() && b.Leading()) })
	waitUntil(t, 5*time.Second, "a scheduler to lead again", func() bool { return a.Leading() != b.Leading() })
	before := len(firedTicks(t, pool)[1])
	time.Sleep(2 * time.Second)

	ticks := firedTicks(t, pool)
	checkTicks(t, ticks, 3)
	if len(ticks[1]) <= before {
		t.Error("nothing fired after the leader lost its connection")
	}
}

func TestATickFiresOnceEvenWithTwoLeaders(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	everySecond(t, pool, 20)

	// Two connections fire side by side as if both believed they lead, which the
	// lock prevents, but the firing itself must not rely on that.
	var (
		wg    sync.WaitGroup
		fired atomic.Int64
	)
	deadline := time.Now().Add(2500 * time.Millisecond)
	for range 2 {
		conn, err := repositories.NewSchedules(pool).LeaderConn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		wg.Go(func() {
			for time.Now().Before(deadline) {
				n, err := conn.FireDue(ctx, fireBatch, (&Scheduler{}).next)
				if err != nil {
					t.Errorf("fire: %v", err)
					return
				}
				fired.Add(int64(n))
				time.Sleep(2 * time.Millisecond)
			}
		})
	}
	wg.Wait()

	var jobs int64
	if err := pool.QueryRow(ctx, `select count(*) from jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if fired.Load() != jobs {
		t.Fatalf("the two connections fired %d times between them but queued %d jobs", fired.Load(), jobs)
	}
	checkTicks(t, firedTicks(t, pool), 2)
}

func TestAnOutageCatchesUpOnce(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	team := defaultTeam(t, pool)
	// Every minute, last fired an hour ago: sixty ticks were missed.
	var late, paused int64
	if err := pool.QueryRow(ctx, `
		insert into schedules (team_id, cron, type, next_run_at) values ($1, '* * * * *', 'sleep', now() - interval '1 hour')
		returning id`, team).Scan(&late); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into schedules (team_id, cron, type, next_run_at, enabled) values ($1, '* * * * *', 'sleep', now() - interval '1 hour', false)
		returning id`, team).Scan(&paused); err != nil {
		t.Fatal(err)
	}

	s, _ := startScheduler(t, pool)
	waitUntil(t, 5*time.Second, "the scheduler to lead", s.Leading)
	time.Sleep(500 * time.Millisecond)

	ticks := firedTicks(t, pool)
	if len(ticks[late]) != 1 || len(ticks[paused]) != 0 {
		t.Fatalf("the late schedule fired %d times and the paused one %d, want 1 and 0", len(ticks[late]), len(ticks[paused]))
	}
	sc, err := repositories.NewSchedules(pool).Get(ctx, team, late)
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Until(sc.NextRunAt); wait <= 0 || wait > time.Minute {
		t.Fatalf("after catching up, the next run is %v away, want within the coming minute", wait)
	}
}
