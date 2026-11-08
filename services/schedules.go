package services

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var errScheduleNotFound = fmt.Errorf("schedule %w", ErrNotFound)

// minScheduleGap keeps a schedule from firing more than once a minute.
const minScheduleGap = time.Minute

// ScheduleSpec is a repeating job as a caller describes it. The job fields follow
// the same rules and defaults as a submit. Nil Enabled means enabled.
type ScheduleSpec struct {
	Cron        string
	Timezone    string
	Type        string
	Payload     json.RawMessage
	Priority    *int
	MaxAttempts *int
	Enabled     *bool
}

// Schedules manages a team's repeating jobs. The Scheduler fires them.
type Schedules struct {
	repo       *repositories.Schedules
	types      jobs.Registry
	maxPerTeam int
}

func NewSchedules(repo *repositories.Schedules, types jobs.Registry, maxPerTeam int) *Schedules {
	return &Schedules{repo: repo, types: types, maxPerTeam: maxPerTeam}
}

func (s *Schedules) Create(ctx context.Context, teamID int64, spec ScheduleSpec) (models.Schedule, error) {
	sc, err := s.build(teamID, spec, time.Now())
	if err != nil {
		return sc, err
	}
	sc, err = s.repo.Create(ctx, sc, s.maxPerTeam)
	if errors.Is(err, repositories.ErrFull) {
		return sc, fmt.Errorf("%w: a team can have %d schedules at most, delete one to add another", ErrConflict, s.maxPerTeam)
	}
	return sc, err
}

func (s *Schedules) List(ctx context.Context, teamID int64) ([]models.Schedule, error) {
	return s.repo.List(ctx, teamID)
}

func (s *Schedules) Get(ctx context.Context, teamID, id int64) (models.Schedule, error) {
	sc, err := s.repo.Get(ctx, teamID, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return sc, errScheduleNotFound
	}
	return sc, err
}

// Update replaces the schedule's definition. Its next run is worked out afresh
// from now.
func (s *Schedules) Update(ctx context.Context, teamID, id int64, spec ScheduleSpec) (models.Schedule, error) {
	sc, err := s.build(teamID, spec, time.Now())
	if err != nil {
		return sc, err
	}
	sc.ID = id
	sc, err = s.repo.Update(ctx, sc)
	if errors.Is(err, repositories.ErrNotFound) {
		return sc, errScheduleNotFound
	}
	return sc, err
}

func (s *Schedules) Delete(ctx context.Context, teamID, id int64) error {
	err := s.repo.Delete(ctx, teamID, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return errScheduleNotFound
	}
	return err
}

// build checks a spec and works out when the schedule first fires after now.
func (s *Schedules) build(teamID int64, spec ScheduleSpec, now time.Time) (models.Schedule, error) {
	nj, err := newJob(s.types, teamID, JobSpec{
		Type:        spec.Type,
		Payload:     spec.Payload,
		Priority:    spec.Priority,
		MaxAttempts: spec.MaxAttempts,
	})
	if err != nil {
		return models.Schedule{}, err
	}
	sc := models.Schedule{
		TeamID:      teamID,
		Cron:        strings.TrimSpace(spec.Cron),
		Timezone:    cmp.Or(strings.TrimSpace(spec.Timezone), "UTC"),
		Type:        nj.Type,
		Payload:     nj.Payload,
		Priority:    nj.Priority,
		MaxAttempts: nj.MaxAttempts,
		Enabled:     spec.Enabled == nil || *spec.Enabled,
	}
	// The zone lives in its own field, and Local would mean whatever the host is set to.
	if strings.Contains(sc.Cron, "TZ=") || sc.Timezone == "Local" {
		return sc, fmt.Errorf("%w: give the time zone in the timezone field, as an IANA name such as Europe/Berlin", ErrInvalid)
	}

	first, err := nextRun(sc.Cron, sc.Timezone, now)
	if err != nil {
		return sc, err
	}
	second, err := nextRun(sc.Cron, sc.Timezone, first)
	if err != nil {
		return sc, err
	}
	if second.Sub(first) < minScheduleGap {
		return sc, fmt.Errorf("%w: a schedule can fire once a minute at most", ErrInvalid)
	}
	sc.NextRunAt = first
	return sc, nil
}
