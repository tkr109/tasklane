package services

import (
	"math"
	"testing"
	"time"

	"github.com/tkr109/tasklane/models"
)

const (
	step   = 30 * time.Second
	window = time.Minute
)

func TestAgedPriority(t *testing.T) {
	tests := []struct {
		priority int
		waited   time.Duration
		want     float64
	}{
		{5, 0, 5},
		{5, 15 * time.Second, 5.5},
		{0, 4*time.Minute + 30*time.Second, 9},
		{9, time.Hour, 129},
		{3, -time.Second, 3}, // clock skew between hosts
	}
	for _, tt := range tests {
		if got := agedPriority(tt.priority, tt.waited, step); got != tt.want {
			t.Errorf("agedPriority(%d, %v) = %v, want %v", tt.priority, tt.waited, got, tt.want)
		}
	}
}

func TestRecentServedDecays(t *testing.T) {
	tests := []struct {
		since time.Duration
		want  float64
	}{
		{0, 100},
		{-time.Second, 100},
		{window, 100 / math.E},
		{10 * window, 100 * math.Exp(-10)},
	}
	for _, tt := range tests {
		if got := recentServed(100, tt.since, window); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("recentServed(100, %v) = %v, want %v", tt.since, got, tt.want)
		}
	}
}

func TestFairTurnsPutsTheTeamFurthestBehindFirst(t *testing.T) {
	backlog := []models.Backlog{
		{TeamID: 1, ShareWeight: 1, Served: 5, Priority: 5},   // 5 per weight
		{TeamID: 2, ShareWeight: 3, Served: 9, Priority: 5},   // 3 per weight
		{TeamID: 3, ShareWeight: 10, Served: 40, Priority: 5}, // 4 per weight
	}
	turns := fairTurns(backlog, step, window)
	var order []int64
	for _, tn := range turns {
		order = append(order, tn.teamID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Fatalf("order = %v, want [2 3 1]", order)
	}
}

func TestFairTurnsCountsOnlyRecentService(t *testing.T) {
	// Team 1 served far more, but long ago; team 2 served less, just now.
	backlog := []models.Backlog{
		{TeamID: 1, ShareWeight: 1, Served: 500, SinceServed: 10 * time.Minute, Priority: 5},
		{TeamID: 2, ShareWeight: 1, Served: 20, Priority: 5},
	}
	if first := fairTurns(backlog, step, window)[0].teamID; first != 1 {
		t.Fatalf("team %d went first, want the team that has been idle", first)
	}
}

func TestFairTurnsTakesTheBestAgedLevel(t *testing.T) {
	tests := []struct {
		name    string
		backlog []models.Backlog
		want    int
	}{
		{"fresh jobs go by priority", []models.Backlog{
			{TeamID: 1, ShareWeight: 1, Priority: 2, Waited: time.Second},
			{TeamID: 1, ShareWeight: 1, Priority: 7, Waited: time.Second},
		}, 7},
		{"an old low job beats a new high one", []models.Backlog{
			{TeamID: 1, ShareWeight: 1, Priority: 9},
			{TeamID: 1, ShareWeight: 1, Priority: 0, Waited: 5 * time.Minute},
		}, 0},
		{"a tie goes to the higher priority", []models.Backlog{
			{TeamID: 1, ShareWeight: 1, Priority: 4, Waited: 2 * step},
			{TeamID: 1, ShareWeight: 1, Priority: 6},
		}, 6},
	}
	for _, tt := range tests {
		turns := fairTurns(tt.backlog, step, window)
		if len(turns) != 1 {
			t.Fatalf("%s: got %d turns, want 1", tt.name, len(turns))
		}
		if turns[0].priority != tt.want {
			t.Errorf("%s: took priority %d, want %d", tt.name, turns[0].priority, tt.want)
		}
	}
}

// When every team always has work, following the turns hands out jobs in the
// ratio of the plan weights.
func TestFairTurnsFollowPlanWeights(t *testing.T) {
	weights := map[int64]int{1: 1, 2: 3, 3: 10}
	served := map[int64]float64{}
	for range 1400 {
		var backlog []models.Backlog
		for id, w := range weights {
			backlog = append(backlog, models.Backlog{TeamID: id, ShareWeight: w, Served: served[id], Priority: 5})
		}
		served[fairTurns(backlog, step, window)[0].teamID]++
	}
	for id, w := range weights {
		if want := float64(100 * w); math.Abs(served[id]-want) > 1 {
			t.Errorf("team with weight %d served %v jobs, want %v", w, served[id], want)
		}
	}
}
