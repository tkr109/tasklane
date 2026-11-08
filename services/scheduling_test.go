package services

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

func createTeam(t *testing.T, pool *pgxpool.Pool, name, plan string) int64 {
	t.Helper()
	ctx := context.Background()
	teams := repositories.NewTeams(pool)
	p, err := teams.PlanByName(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	team, err := teams.Create(ctx, name, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return team.ID
}

// fill queues n "work" jobs for a team. Each payload names the team so handlers
// can tell whose job they are running.
func fill(t *testing.T, pool *pgxpool.Pool, teamID int64, n int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into jobs (team_id, type, payload)
		select $1, 'work', jsonb_build_object('team', $1::bigint) from generate_series(1, $2)`,
		teamID, n); err != nil {
		t.Fatal(err)
	}
}

// load runs "work" jobs for a few milliseconds each and remembers the most jobs
// it ever saw running at once for each team.
type load struct {
	mu      sync.Mutex
	running map[int64]int
	peak    map[int64]int
}

func (l *load) registry(d time.Duration) jobs.Registry {
	l.running, l.peak = map[int64]int{}, map[int64]int{}
	return jobs.Registry{"work": {Timeout: 5 * time.Second, Handler: func(ctx context.Context, payload json.RawMessage, _ jobs.Progress) (any, error) {
		var p struct{ Team int64 }
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		l.mu.Lock()
		l.running[p.Team]++
		l.peak[p.Team] = max(l.peak[p.Team], l.running[p.Team])
		l.mu.Unlock()

		time.Sleep(d)

		l.mu.Lock()
		l.running[p.Team]--
		l.mu.Unlock()
		return nil, nil
	}}}
}

func (l *load) peakFor(team int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak[team]
}

// started counts each team's jobs that a worker has picked up.
func started(t *testing.T, pool *pgxpool.Pool) map[int64]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`select team_id, count(*) from jobs where attempts > 0 group by team_id`)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int64]int{}
	var team int64
	var n int
	for rows.Next() {
		if err := rows.Scan(&team, &n); err != nil {
			t.Fatal(err)
		}
		counts[team] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestBusyTeamDoesNotStarveASmallOne(t *testing.T) {
	pool := testDB(t)
	big := createTeam(t, pool, "big", "enterprise")
	small := createTeam(t, pool, "small", "free")
	fill(t, pool, big, 5000)
	fill(t, pool, small, 10)

	var l load
	startWorker(t, pool, l.registry(2*time.Millisecond))

	waitUntil(t, 30*time.Second, "the small team's jobs to finish", func() bool {
		var n int
		_ = pool.QueryRow(context.Background(),
			`select count(*) from jobs where team_id = $1 and status = 'succeeded'`, small).Scan(&n)
		return n == 10
	})
	// At weights 10:1 the small team gets about one grab in eleven, so its ten
	// jobs should be done after roughly a hundred of the big team's.
	bigStarted := started(t, pool)[big]
	t.Logf("the big team started %d jobs while the small team ran its 10", bigStarted)
	if bigStarted > 500 {
		t.Fatalf("the small team waited behind %d of the big team's jobs", bigStarted)
	}
}

func TestServedCountsFollowPlanWeights(t *testing.T) {
	pool := testDB(t)
	weights := map[int64]float64{
		createTeam(t, pool, "free", "free"):             1,
		createTeam(t, pool, "pro", "pro"):               3,
		createTeam(t, pool, "enterprise", "enterprise"): 10,
	}
	for team := range weights {
		fill(t, pool, team, 3000)
	}

	// Eight slots in all keep every team's fair share under its worker cap, so the
	// weights decide and the caps don't.
	var l load
	types := l.registry(5 * time.Millisecond)
	startWorker(t, pool, types)
	startWorker(t, pool, types)

	const window = 1400
	var counts map[int64]int
	waitUntil(t, 60*time.Second, "enough jobs to start", func() bool {
		counts = started(t, pool)
		total := 0
		for _, n := range counts {
			total += n
		}
		return total >= window
	})

	total := 0
	for _, n := range counts {
		total += n
	}
	for team, w := range weights {
		t.Logf("weight %v: %d jobs", w, counts[team])
	}
	for team, w := range weights {
		want := float64(total) * w / 14
		if got := float64(counts[team]); math.Abs(got-want) > 0.3*want {
			t.Errorf("weight %v team started %v of %d jobs, want about %.0f", w, got, total, want)
		}
	}
}

func TestTeamsStayUnderTheirWorkerCap(t *testing.T) {
	pool := testDB(t)
	free := createTeam(t, pool, "free", "free")
	pro := createTeam(t, pool, "pro", "pro")
	fill(t, pool, free, 60)
	fill(t, pool, pro, 120)

	// 24 slots, far more than both caps together.
	var l load
	types := l.registry(20 * time.Millisecond)
	for range 3 {
		startWorker(t, pool, types)
	}

	ctx := context.Background()
	caps := map[int64]int{free: 2, pro: 6}
	waitUntil(t, 30*time.Second, "every job to finish", func() bool {
		rows, err := pool.Query(ctx, `select team_id, count(*) from jobs where status = 'running' group by team_id`)
		if err != nil {
			t.Fatal(err)
		}
		var team int64
		var n int
		for rows.Next() {
			if err := rows.Scan(&team, &n); err != nil {
				t.Fatal(err)
			}
			if n > caps[team] {
				t.Errorf("team %d had %d jobs running, its cap is %d", team, n, caps[team])
			}
		}
		var left int
		_ = pool.QueryRow(ctx, `select count(*) from jobs where status <> 'succeeded'`).Scan(&left)
		return left == 0
	})

	for team, limit := range caps {
		if peak := l.peakFor(team); peak > limit {
			t.Errorf("team %d ran %d jobs at once, its cap is %d", team, peak, limit)
		}
		var running int
		if err := pool.QueryRow(ctx, `select running from team_usage where team_id = $1`, team).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if running != 0 {
			t.Errorf("team %d still counts %d running jobs after the queue drained", team, running)
		}
	}
}

func TestOldLowPriorityJobBeatsANewUrgentOne(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	team := defaultTeam(t, pool)

	var old, urgent int64
	if err := pool.QueryRow(ctx, `
		insert into jobs (team_id, type, priority, run_at, ready_at)
		values ($1, 'noop', 0, now() - interval '10 minutes', now() - interval '10 minutes')
		returning id`, team).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		insert into jobs (team_id, type, priority) values ($1, 'noop', 9) returning id`, team).Scan(&urgent); err != nil {
		t.Fatal(err)
	}

	repo := repositories.NewJobs(pool)
	order := []int64{}
	for range 2 {
		job, err := grab(ctx, repo, "w1", []string{"noop"}, fastConfig())
		if err != nil {
			t.Fatal(err)
		}
		order = append(order, job.ID)
	}
	if order[0] != old || order[1] != urgent {
		t.Fatalf("grabbed %v, want the aged job %d before the urgent one %d", order, old, urgent)
	}
	if _, err := grab(ctx, repo, "w1", []string{"noop"}, fastConfig()); !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("third grab: err = %v, want ErrNotFound", err)
	}

	job, err := repo.Get(ctx, team, old)
	if err != nil || job.Status != models.StatusRunning {
		t.Fatalf("aged job: status=%s err=%v", job.Status, err)
	}
}

func TestAHeldJobKeepsTheAgeItBuiltUp(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	team := defaultTeam(t, pool)
	repo := repositories.NewJobs(pool)
	types := []string{"email"}

	var old int64
	if err := pool.QueryRow(ctx, `
		insert into jobs (team_id, type, priority, run_at, ready_at)
		values ($1, 'email', 0, now() - interval '10 minutes', now() - interval '10 minutes')
		returning id`, team).Scan(&old); err != nil {
		t.Fatal(err)
	}
	job, err := grab(ctx, repo, "w1", types, fastConfig())
	if err != nil || job.ID != old {
		t.Fatalf("first grab: job %d, err %v", job.ID, err)
	}
	// The type's rate limit sends it back for a moment, and an urgent job arrives.
	if err := repo.Hold(ctx, old, "w1", 50*time.Millisecond, "email is at its limit"); err != nil {
		t.Fatal(err)
	}
	var urgent int64
	if err := pool.QueryRow(ctx, `
		insert into jobs (team_id, type, priority) values ($1, 'email', 9) returning id`, team).Scan(&urgent); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// Ten minutes of waiting is worth twenty levels, so it still beats a fresh 9.
	job, err = grab(ctx, repo, "w1", types, fastConfig())
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != old {
		t.Fatalf("grabbed job %d after the hold, want the aged job %d before the urgent %d", job.ID, old, urgent)
	}
}
