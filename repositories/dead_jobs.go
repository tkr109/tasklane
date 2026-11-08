package repositories

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tkr109/tasklane/models"
)

// deadSamples is how many of its most recent jobs a dead group lists.
const deadSamples = 5

// DeadGroups returns the dead jobs grouped by type and error pattern, most recent
// failure first. The group's ID is left for the caller to set.
func (r *Jobs) DeadGroups(ctx context.Context) ([]models.DeadGroup, error) {
	// One row per group and hour, hours past a day counted as 24, so the trend
	// comes out of the same scan as the totals.
	rows, err := r.db.Query(ctx, `
		select j.type, j.error_key, least(greatest(floor(extract(epoch from now() - j.finished_at) / 3600), 0), 24)::int,
			count(*), min(j.finished_at), max(j.finished_at), array_agg(distinct t.name)
		from jobs j
		join teams t on t.id = j.team_id
		where j.status = 'dead'
		group by 1, 2, 3`)
	if err != nil {
		return nil, err
	}
	type key struct{ jobType, pattern string }
	groups := map[key]*models.DeadGroup{}
	var (
		k        key
		hoursAgo int
		n        int
		first    time.Time
		last     time.Time
		teams    []string
	)
	_, err = pgx.ForEachRow(rows, []any{&k.jobType, &k.pattern, &hoursAgo, &n, &first, &last, &teams}, func() error {
		g, ok := groups[k]
		if !ok {
			g = &models.DeadGroup{Type: k.jobType, Pattern: k.pattern, FirstAt: first, LastAt: last, Hourly: make([]int, 24)}
			groups[k] = g
		}
		g.Count += n
		if first.Before(g.FirstAt) {
			g.FirstAt = first
		}
		if last.After(g.LastAt) {
			g.LastAt = last
		}
		g.Teams = append(g.Teams, teams...)
		if hoursAgo < 24 {
			g.Hourly[23-hoursAgo] = n
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	list := make([]models.DeadGroup, 0, len(groups))
	for _, g := range groups {
		slices.Sort(g.Teams)
		g.Teams = slices.Compact(g.Teams)
		list = append(list, *g)
	}
	slices.SortFunc(list, func(a, b models.DeadGroup) int {
		return cmp.Or(b.LastAt.Compare(a.LastAt), cmp.Compare(a.Type, b.Type), cmp.Compare(a.Pattern, b.Pattern))
	})
	if len(list) == 0 {
		return list, nil
	}
	return list, r.addSamples(ctx, list)
}

// addSamples fills in each group's latest error and most recent job ids, with one
// short index probe per group.
func (r *Jobs) addSamples(ctx context.Context, groups []models.DeadGroup) error {
	types := make([]string, len(groups))
	patterns := make([]string, len(groups))
	for i, g := range groups {
		types[i], patterns[i] = g.Type, g.Pattern
	}
	rows, err := r.db.Query(ctx, `
		select s.ids, s.last_error
		from unnest($1::text[], $2::text[]) with ordinality as g (type, pattern, n)
		cross join lateral (
			select array_agg(id order by finished_at desc, id desc) as ids,
				(array_agg(coalesce(last_error, '') order by finished_at desc, id desc))[1] as last_error
			from (
				select id, finished_at, last_error from jobs
				where status = 'dead' and type = g.type and error_key = g.pattern
				order by finished_at desc, id desc
				limit $3
			) recent
		) s
		order by g.n`,
		types, patterns, deadSamples)
	if err != nil {
		return err
	}
	i := 0
	var (
		ids       []int64
		lastError *string
	)
	_, err = pgx.ForEachRow(rows, []any{&ids, &lastError}, func() error {
		if i < len(groups) {
			groups[i].SampleIDs = slices.Clone(ids)
			if lastError != nil {
				groups[i].Error = *lastError
			}
		}
		i++
		return nil
	})
	return err
}

// DeadGroupJobs returns a dead group's most recent jobs.
func (r *Jobs) DeadGroupJobs(ctx context.Context, jobType, pattern string, limit int) ([]models.Job, error) {
	rows, err := r.db.Query(ctx, `
		select `+jobColumns+` from jobs
		where status = 'dead' and type = $1 and error_key = $2
		order by finished_at desc, id desc
		limit $3`,
		jobType, pattern, limit)
	if err != nil {
		return nil, err
	}
	return collectJobs(rows)
}

// RetryDeadGroup puts every dead job of the group back in the queue with a fresh
// set of attempts, and says how many there were.
func (r *Jobs) RetryDeadGroup(ctx context.Context, jobType, pattern string) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `
		with retried as (
			update jobs
			set status = 'queued', attempts = 0, progress = 0, run_at = now(), ready_at = now(),
				finished_at = null, updated_at = now()
			where status = 'dead' and type = $1 and error_key = $2
			returning id
		), logged as (
			insert into job_events (job_id, kind, message)
			select id, 'retried', 'retried with its dead job group' from retried
		)
		select count(*) from retried`,
		jobType, pattern).Scan(&n)
	if err == nil && n > 0 {
		r.wakeWorkers(ctx)
	}
	return n, err
}
