package services

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tkr109/tasklane/models"
)

func newPolicy() *policy {
	return &policy{min: 2, max: 10, jobsPerWorker: 500, concurrency: 1, cooldown: 20 * time.Second, stepDown: 3 * time.Second,
		interval: time.Second}
}

// plenty is more slots than any test here asks for, so only the backlog counts.
const plenty = 1000

func TestScalerTarget(t *testing.T) {
	tests := []struct {
		name        string
		runnable    int
		slots       int
		concurrency int
		want        int
	}{
		{"an empty queue keeps the minimum", 0, 0, 1, 2},
		{"one worker per 500 jobs", 1201, plenty, 1, 3},
		{"exactly 500 jobs a worker", 2500, plenty, 1, 5},
		{"a flood is clamped to the maximum", 10_000, plenty, 1, 10},
		{"no more workers than a team's cap can use", 10_000, 6, 1, 6},
		{"a worker running four jobs fills more of the cap", 10_000, 10, 4, 3},
		{"the cap never pushes below the minimum", 900, 1, 1, 2},
	}
	for _, tt := range tests {
		p := newPolicy()
		p.concurrency = tt.concurrency
		if got, _ := p.target(models.Demand{Runnable: tt.runnable, Slots: tt.slots}); got != tt.want {
			t.Errorf("%s: target %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestScalerScalesUpAtOnce(t *testing.T) {
	p := newPolicy()
	now := time.Now()
	d := p.decide(now, models.Demand{Runnable: 4000, OldestWait: 12 * time.Second, Slots: plenty}, 2)
	if d.action != "up" || d.from != 2 || d.to != 8 {
		t.Fatalf("decision %+v, want up from 2 to 8", d)
	}
	if d.reason != "4000 runnable, oldest wait 12s" {
		t.Errorf("reason %q", d.reason)
	}

	d = p.decide(now.Add(2*time.Second), models.Demand{Runnable: 20_000, Slots: plenty}, 8)
	if d.action != "up" || d.to != 10 || !strings.HasSuffix(d.reason, "; at most 10") {
		t.Fatalf("decision %+v, want up to the maximum", d)
	}
}

func TestScalerScalesDownOneAtATimeAfterTheCooldown(t *testing.T) {
	p := newPolicy()
	start := time.Now()
	current := 10
	var downs []time.Duration
	for elapsed := time.Duration(0); elapsed <= time.Minute; elapsed += time.Second {
		d := p.decide(start.Add(elapsed), models.Demand{}, current)
		switch d.action {
		case "hold":
			if elapsed != 0 {
				t.Fatalf("a second hold at %v", elapsed)
			}
			if at := p.scaleDownAt(); at == nil || !at.Equal(start.Add(20*time.Second).UTC()) {
				t.Fatalf("scale down at %v, want the end of the cool-down", at)
			}
		case "down":
			if d.to != current-1 {
				t.Fatalf("stepped from %d to %d", current, d.to)
			}
			current = d.to
			downs = append(downs, elapsed)
		case "up":
			t.Fatalf("scaled up at %v", elapsed)
		}
	}

	want := []time.Duration{20, 23, 26, 29, 32, 35, 38, 41}
	if len(downs) != len(want) {
		t.Fatalf("stepped down at %v, want %v seconds in", downs, want)
	}
	for i := range want {
		if downs[i] != want[i]*time.Second {
			t.Fatalf("stepped down at %v, want %v seconds in", downs, want)
		}
	}
	if current != 2 || p.scaleDownAt() != nil {
		t.Errorf("ended at %d workers with a cool-down at %v", current, p.scaleDownAt())
	}
}

func TestScalerStepsKeepTimeWhenTheLoopRunsLate(t *testing.T) {
	p := newPolicy()
	start := time.Now()
	current := 10
	var downs []int
	for loop := range 40 {
		// Every other loop runs 80ms late.
		at := start.Add(time.Duration(loop)*time.Second + time.Duration(loop%2)*80*time.Millisecond)
		if d := p.decide(at, models.Demand{}, current); d.action == "down" {
			current = d.to
			downs = append(downs, loop)
		}
	}
	if want := []int{20, 23, 26, 29, 32, 35, 38}; !slices.Equal(downs, want) {
		t.Errorf("stepped down on loops %v, want %v", downs, want)
	}
}

func TestScalerIgnoresJobsHeldByARateLimit(t *testing.T) {
	p := newPolicy()
	// Runnable already leaves out the held jobs; they only show in the reason.
	d := p.decide(time.Now(), models.Demand{Runnable: 1000, Held: 9000, Slots: plenty}, 2)
	if d.to != 2 || d.action != "" {
		t.Fatalf("decision %+v, want to stay at 2", d)
	}
	d = p.decide(time.Now(), models.Demand{Runnable: 1600, Held: 9000, Slots: plenty}, 2)
	if d.to != 4 || !strings.Contains(d.reason, "9000 held by a rate limit not counted") {
		t.Fatalf("decision %+v", d)
	}
}

func TestScalerDoesNotFlap(t *testing.T) {
	p := newPolicy()
	start := time.Now()
	current := 2
	actions := map[string]int{}
	// The queue fills and empties every 10s, never staying empty for a whole cool-down.
	for elapsed := time.Duration(0); elapsed < 2*time.Minute; elapsed += 2 * time.Second {
		demand := models.Demand{Slots: plenty}
		if elapsed%(20*time.Second) < 10*time.Second {
			demand.Runnable = 3000
		}
		d := p.decide(start.Add(elapsed), demand, current)
		actions[d.action]++
		current = d.to
	}
	if actions["up"] != 1 || actions["hold"] != 1 || actions["down"] != 0 || current != 6 {
		t.Errorf("actions %v ending at %d workers, want one scale up, one hold and no scale down", actions, current)
	}

	// Once it stays empty, the scaler comes back down.
	for elapsed := 2 * time.Minute; elapsed < 4*time.Minute; elapsed += 2 * time.Second {
		current = p.decide(start.Add(elapsed), models.Demand{}, current).to
	}
	if current != 2 {
		t.Errorf("ended at %d workers, want 2", current)
	}
}
