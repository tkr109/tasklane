package services

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/tkr109/tasklane/models"
)

func jobEvent(jobType string, status models.Status) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"id": 1, "type": jobType, "status": status})
	return b
}

func TestMonitorCountsFinishedJobs(t *testing.T) {
	e := NewEvents(nil)
	m := NewMonitor(nil, nil, nil, nil, e, nil, nil, 0)

	for range 4 {
		e.deliver("job", jobEvent("email", models.StatusSucceeded))
	}
	e.deliver("job", jobEvent("email", models.StatusFailed))
	e.deliver("job", jobEvent("webhook", models.StatusDead))
	e.deliver("job", jobEvent("webhook", models.StatusRunning))
	e.deliver("worker", json.RawMessage(`{"id":"w-1"}`))

	now := time.Now()
	sample := m.tick(now)
	if sample.Succeeded != 4 || sample.Failed != 2 || !sample.At.Equal(now) {
		t.Fatalf("sample %+v, want 4 succeeded and 2 failed", sample)
	}
	if next := m.tick(now.Add(time.Second)); next.Succeeded != 0 || next.Failed != 0 {
		t.Fatalf("the next second started with %+v", next)
	}
	rates := m.rates()
	if math.Abs(rates["email"]-5.0/rateWindow) > 1e-9 || math.Abs(rates["webhook"]-1.0/rateWindow) > 1e-9 {
		t.Fatalf("rates %v", rates)
	}
	for range rateWindow {
		m.tick(now)
	}
	if rates := m.rates(); rates["email"] != 0 {
		t.Fatalf("rates outlived the window: %v", rates)
	}
}

func TestMonitorServesThePublishedStats(t *testing.T) {
	e := NewEvents(nil)
	m := NewMonitor(nil, nil, nil, nil, e, nil, nil, 0)

	for i := range sampleHistory + 10 {
		tick := models.StatsTick{
			Snapshot: models.Snapshot{Counts: models.Counts{Queued: i}},
			Sample:   models.Sample{Queued: i},
		}
		b, err := json.Marshal(tick)
		if err != nil {
			t.Fatal(err)
		}
		e.deliver("stats", b)
	}
	// Fresh stats come from the events alone, so this needs no database.
	stats, err := m.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Counts.Queued != sampleHistory+9 || len(stats.Samples) != sampleHistory || stats.Samples[0].Queued != 10 {
		t.Fatalf("queued %d with %d samples starting at %d", stats.Counts.Queued, len(stats.Samples), stats.Samples[0].Queued)
	}
}

func TestTeamUsageSharesByWeight(t *testing.T) {
	team := func(id int64, weight, queued, running int) models.TeamSummary {
		var s models.TeamSummary
		s.ID, s.Plan.ShareWeight = id, weight
		s.Usage.Queued, s.Usage.Running = queued, running
		return s
	}
	usage := teamUsage([]models.TeamSummary{
		team(1, 1, 5, 0),
		team(2, 3, 0, 2),
		team(3, 10, 0, 0), // idle, so it takes no share
	}, map[int64]int{2: 40})

	want := []float64{0.25, 0.75, 0}
	for i, u := range usage {
		if math.Abs(u.FairShare-want[i]) > 1e-9 {
			t.Errorf("team %d: fair share %v, want %v", u.TeamID, u.FairShare, want[i])
		}
	}
	if usage[1].RejectedLastMinute != 40 || usage[0].RejectedLastMinute != 0 {
		t.Errorf("rejections %+v", usage)
	}
}

func TestTheBiggestEventsFit(t *testing.T) {
	chunk := make([]models.TeamUsage, teamsPerEvent)
	for i := range chunk {
		chunk[i] = models.TeamUsage{
			TeamID: math.MaxInt64, Queued: math.MaxInt32, Running: math.MaxInt32, JobsLastMinute: math.MaxInt32,
			RejectedLastMinute: math.MaxInt32, FairShare: 1.0 / 3,
		}
	}
	if _, err := encodeEvent("team_usage", chunk); err != nil {
		t.Error(err)
	}

	// A dozen job types with long names is more than any registry here holds.
	limit := 1.0 / 3
	var tick models.StatsTick
	at := time.Now()
	tick.Scaler = &models.ScalerStatus{
		Current: math.MaxInt32, Target: math.MaxInt32, Min: math.MaxInt32, Max: math.MaxInt32,
		JobsPerWorker: math.MaxInt32, IntervalSeconds: 1.0 / 3, CooldownSeconds: 1.0 / 3, StepDownSeconds: 1.0 / 3,
		ScaleDownAt: &at,
	}
	for range 12 {
		tick.Types = append(tick.Types, models.TypeStats{
			Type: "image.resize.thumbnail.large", TimeoutSeconds: 300, OutgoingPerSecond: &limit,
			RatePerSecond: 1.0 / 3, Held: math.MaxInt32,
		})
	}
	if _, err := encodeEvent("stats", tick); err != nil {
		t.Error(err)
	}
}
