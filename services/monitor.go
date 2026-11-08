package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

const (
	// statsEvery is how often the leading api publishes stats and team usage.
	statsEvery = time.Second
	// staleStats is how old the last published snapshot may be before a request
	// for stats counts afresh.
	staleStats = 3 * time.Second
	// sampleHistory is how many one-second samples the charts get.
	sampleHistory = 150
	// rateWindow is how many seconds a job type's rate is averaged over.
	rateWindow = 5
	// heldWindow is how recently a type must have been throttled for its waiting
	// jobs to count as held by its rate limit.
	heldWindow = 10 * time.Second
	// teamsPerEvent keeps a team usage event well under the payload limit.
	teamsPerEvent = 20
	// decisionsShown is how many of the scaler's latest decisions the workers
	// page gets.
	decisionsShown = 50
)

// Monitor answers the dashboard's questions about the system as a whole. Every api
// keeps the samples from the stats events it relays, but only the one leading the
// scheduler counts and publishes them, so the queries run once a second however
// many apis there are.
type Monitor struct {
	jobs       *repositories.Jobs
	workers    *repositories.Workers
	teams      *repositories.Teams
	scaler     *repositories.Scaler
	events     *Events
	types      jobs.Registry
	typeLimits map[string]float64
	ceiling    int

	mu sync.Mutex
	// What finished since the last tick, going by the job events.
	succeeded, failed int
	finished          map[string]int
	// finishedPerTick is the last rateWindow ticks' finished jobs by type.
	finishedPerTick []map[string]int
	rejected        []rejection
	latest          models.Snapshot
	latestAt        time.Time
	samples         []models.Sample
}

type rejection struct {
	at     time.Time
	teamID int64
	jobs   int
}

func NewMonitor(jobsRepo *repositories.Jobs, workers *repositories.Workers, teams *repositories.Teams,
	scaler *repositories.Scaler, events *Events, types jobs.Registry, typeLimits map[string]float64, ceiling int) *Monitor {
	m := &Monitor{
		jobs:       jobsRepo,
		workers:    workers,
		teams:      teams,
		scaler:     scaler,
		events:     events,
		types:      types,
		typeLimits: typeLimits,
		ceiling:    ceiling,
		finished:   map[string]int{},
	}
	events.watch(m.observe)
	return m
}

func (m *Monitor) Workers(ctx context.Context) ([]models.Worker, error) {
	return m.workers.List(ctx)
}

// Decisions returns the scaler's latest decisions, newest first.
func (m *Monitor) Decisions(ctx context.Context) ([]models.Decision, error) {
	return m.scaler.Decisions(ctx, decisionsShown)
}

// Stats returns the latest published snapshot with the recent samples. If no
// snapshot has come lately, it counts one itself.
func (m *Monitor) Stats(ctx context.Context) (models.Stats, error) {
	m.mu.Lock()
	snap, fresh := m.latest, time.Since(m.latestAt) < staleStats
	samples := append([]models.Sample{}, m.samples...)
	m.mu.Unlock()

	if !fresh {
		var err error
		if snap, err = m.snapshot(ctx); err != nil {
			return models.Stats{}, err
		}
	}
	return models.Stats{Snapshot: snap, Samples: samples}, nil
}

// Run closes a sample every second, and publishes it with a snapshot and the
// teams' usage whenever leading says this process leads.
func (m *Monitor) Run(ctx context.Context, leading func() bool) {
	t := time.NewTicker(statsEvery)
	defer t.Stop()
	for {
		var now time.Time
		select {
		case <-ctx.Done():
			return
		case now = <-t.C:
		}
		sample := m.tick(now)
		if !leading() {
			continue
		}
		if err := m.publish(ctx, sample); err != nil && ctx.Err() == nil {
			slog.Warn("publish stats", "err", err)
		}
	}
}

// tick returns what finished in the second that just ended, and keeps its counts
// by type for the rates.
func (m *Monitor) tick(now time.Time) models.Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	sample := models.Sample{At: now.UTC(), Succeeded: m.succeeded, Failed: m.failed}
	m.finishedPerTick = append(m.finishedPerTick, m.finished)
	if len(m.finishedPerTick) > rateWindow {
		m.finishedPerTick = m.finishedPerTick[1:]
	}
	m.succeeded, m.failed, m.finished = 0, 0, map[string]int{}
	return sample
}

func (m *Monitor) publish(ctx context.Context, sample models.Sample) error {
	snap, err := m.snapshot(ctx)
	if err != nil {
		return err
	}
	sample.Queued, sample.Workers = snap.Counts.Queued, snap.Workers.Running
	if err := m.events.Publish(ctx, "stats", models.StatsTick{Snapshot: snap, Sample: sample}); err != nil {
		return err
	}

	teams, err := m.teams.List(ctx)
	if err != nil {
		return err
	}
	for chunk := range slices.Chunk(teamUsage(teams, m.rejectedLastMinute(time.Now())), teamsPerEvent) {
		if err := m.events.Publish(ctx, "team_usage", chunk); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) snapshot(ctx context.Context) (models.Snapshot, error) {
	snap, err := m.jobs.Overview(ctx)
	if err != nil {
		return snap, err
	}
	held, err := m.jobs.HeldByLimit(ctx, heldWindow)
	if err != nil {
		return snap, err
	}
	snap.QueueCeiling = m.ceiling
	for _, n := range held {
		snap.Runnable -= n
	}
	snap.Runnable = max(snap.Runnable, 0)

	rates := m.rates()
	snap.Types = []models.TypeStats{}
	for _, name := range m.types.Names() {
		ts := models.TypeStats{
			Type:           name,
			TimeoutSeconds: m.types[name].Timeout.Seconds(),
			RatePerSecond:  rates[name],
			Held:           held[name],
		}
		if limit, ok := m.typeLimits[name]; ok {
			ts.OutgoingPerSecond = &limit
		}
		snap.Types = append(snap.Types, ts)
	}
	return snap, nil
}

// rates is how many jobs of each type finished a second over the last few ticks.
func (m *Monitor) rates() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	rates := map[string]float64{}
	for _, tick := range m.finishedPerTick {
		for name, n := range tick {
			rates[name] += float64(n) / rateWindow
		}
	}
	return rates
}

func (m *Monitor) rejectedLastMinute(now time.Time) map[int64]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = slices.DeleteFunc(m.rejected, func(r rejection) bool { return now.Sub(r.at) > time.Minute })
	counts := map[int64]int{}
	for _, r := range m.rejected {
		counts[r.teamID] += r.jobs
	}
	return counts
}

// teamUsage puts each team's numbers together. Its fair share is its plan weight
// over the weights of every team with work queued or running.
func teamUsage(teams []models.TeamSummary, rejected map[int64]int) []models.TeamUsage {
	busyWeight := 0
	for _, t := range teams {
		if t.Usage.Queued+t.Usage.Running > 0 {
			busyWeight += t.Plan.ShareWeight
		}
	}
	usage := make([]models.TeamUsage, len(teams))
	for i, t := range teams {
		usage[i] = models.TeamUsage{
			TeamID:             t.ID,
			Queued:             t.Usage.Queued,
			Running:            t.Usage.Running,
			JobsLastMinute:     t.Usage.JobsLastMinute,
			RejectedLastMinute: rejected[t.ID],
		}
		if t.Usage.Queued+t.Usage.Running > 0 {
			usage[i].FairShare = float64(t.Plan.ShareWeight) / float64(busyWeight)
		}
	}
	return usage
}

// rejectedJobs is the event an api publishes when a team's rate limit turns a
// submit away, so the leading api can count every api's refusals.
type rejectedJobs struct {
	TeamID int64 `json:"team_id"`
	Jobs   int   `json:"jobs"`
}

// observe keeps count of what the relayed events say. It runs for every event, so
// it decodes only the ones it needs.
func (m *Monitor) observe(ev Event) {
	switch ev.Name {
	case "job":
		var j struct {
			Type   string        `json:"type"`
			Status models.Status `json:"status"`
		}
		if err := json.Unmarshal(ev.Data, &j); err != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		switch j.Status {
		case models.StatusSucceeded:
			m.succeeded++
		case models.StatusFailed, models.StatusDead:
			m.failed++
		default:
			return
		}
		m.finished[j.Type]++
	case "rejected":
		var r rejectedJobs
		if err := json.Unmarshal(ev.Data, &r); err != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.rejected = append(m.rejected, rejection{at: time.Now(), teamID: r.TeamID, jobs: r.Jobs})
	case "stats":
		var tick models.StatsTick
		if err := json.Unmarshal(ev.Data, &tick); err != nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.latest, m.latestAt = tick.Snapshot, time.Now()
		m.samples = append(m.samples, tick.Sample)
		if len(m.samples) > sampleHistory {
			m.samples = m.samples[len(m.samples)-sampleHistory:]
		}
	}
}
