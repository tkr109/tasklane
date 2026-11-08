package services

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

// turn is a team's place in line and the priority level to take a job from.
type turn struct {
	teamID   int64
	priority int
	// share is the team's recent served count per unit of plan weight. The
	// lowest share is the team furthest behind.
	share float64
	aged  float64
}

// agedPriority is a job's effective priority: it gains a level for every step
// it has waited, so low priority jobs still reach the front.
func agedPriority(priority int, waited, step time.Duration) float64 {
	return float64(priority) + max(waited, 0).Seconds()/step.Seconds()
}

// recentServed decays a served count recorded since ago, by a factor of e per
// window. The grab query applies the same decay when it bumps the count.
func recentServed(served float64, since, window time.Duration) float64 {
	return served * math.Exp(-max(since, 0).Seconds()/window.Seconds())
}

// fairTurns orders the teams in the backlog, furthest behind its fair share first,
// and picks each team's priority level with the highest aged priority.
func fairTurns(backlog []models.Backlog, agingStep, window time.Duration) []turn {
	var turns []turn
	index := map[int64]int{}
	for _, b := range backlog {
		aged := agedPriority(b.Priority, b.Waited, agingStep)
		i, seen := index[b.TeamID]
		if !seen {
			index[b.TeamID] = len(turns)
			turns = append(turns, turn{
				teamID:   b.TeamID,
				priority: b.Priority,
				share:    recentServed(b.Served, b.SinceServed, window) / float64(b.ShareWeight),
				aged:     aged,
			})
			continue
		}
		t := &turns[i]
		if aged > t.aged || (aged == t.aged && b.Priority > t.priority) {
			t.priority, t.aged = b.Priority, aged
		}
	}
	slices.SortFunc(turns, func(a, b turn) int {
		return cmp.Or(cmp.Compare(a.share, b.share), cmp.Compare(a.teamID, b.teamID))
	})
	return turns
}

// grab claims the next job for a worker. It reads the backlog, takes turns here,
// and tries the teams in that order. A try comes back empty when another worker
// got the last job at that level or the team's last free slot first.
func grab(ctx context.Context, repo *repositories.Jobs, workerID string, types []string, cfg config.Worker) (models.Job, error) {
	backlog, err := repo.Backlog(ctx, types)
	if err != nil {
		return models.Job{}, err
	}
	for _, t := range fairTurns(backlog, cfg.AgingStep, cfg.FairShareWindow) {
		job, err := repo.Grab(ctx, workerID, t.teamID, t.priority, types, cfg.Lease, cfg.FairShareWindow)
		if !errors.Is(err, repositories.ErrNotFound) {
			return job, err
		}
	}
	return models.Job{}, repositories.ErrNotFound
}
