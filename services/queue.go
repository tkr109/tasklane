package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tkr109/tasklane/jobs"
	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var (
	ErrNotFound = errors.New("job not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
)

const (
	MaxBatchSize = 500

	defaultPriority    = 5
	defaultMaxAttempts = 3
	maxAttemptsCeiling = 25
	maxKeyLength       = 255
	defaultListLimit   = 50
	maxListLimit       = 200
)

// JobSpec is what a caller asks for. Nil fields get defaults.
type JobSpec struct {
	Type        string
	Payload     json.RawMessage
	Priority    *int
	MaxAttempts *int
}

type Queue struct {
	repo  *repositories.Jobs
	types jobs.Registry
}

func NewQueue(repo *repositories.Jobs, types jobs.Registry) *Queue {
	return &Queue{repo: repo, types: types}
}

func (q *Queue) Submit(ctx context.Context, teamID int64, spec JobSpec, idempotencyKey string) (job models.Job, created bool, err error) {
	if len(idempotencyKey) > maxKeyLength {
		return job, false, fmt.Errorf("%w: idempotency key longer than %d characters", ErrInvalid, maxKeyLength)
	}
	nj, err := q.newJob(teamID, spec)
	if err != nil {
		return job, false, err
	}
	nj.IdempotencyKey = idempotencyKey
	return q.repo.Create(ctx, nj)
}

func (q *Queue) SubmitBatch(ctx context.Context, teamID int64, specs []JobSpec) ([]models.Job, error) {
	if len(specs) == 0 || len(specs) > MaxBatchSize {
		return nil, fmt.Errorf("%w: a batch holds 1 to %d jobs", ErrInvalid, MaxBatchSize)
	}
	batch := make([]models.NewJob, len(specs))
	for i, spec := range specs {
		nj, err := q.newJob(teamID, spec)
		if err != nil {
			return nil, fmt.Errorf("job %d: %w", i, err)
		}
		batch[i] = nj
	}
	return q.repo.CreateBatch(ctx, batch)
}

func (q *Queue) newJob(teamID int64, spec JobSpec) (models.NewJob, error) {
	nj := models.NewJob{
		TeamID:      teamID,
		Type:        spec.Type,
		Payload:     spec.Payload,
		Priority:    defaultPriority,
		MaxAttempts: defaultMaxAttempts,
	}
	if _, ok := q.types[spec.Type]; !ok {
		return nj, fmt.Errorf("%w: unknown job type %q, expected one of %v", ErrInvalid, spec.Type, q.types.Names())
	}
	if spec.Priority != nil {
		if *spec.Priority < 0 || *spec.Priority > 9 {
			return nj, fmt.Errorf("%w: priority must be between 0 and 9", ErrInvalid)
		}
		nj.Priority = *spec.Priority
	}
	if spec.MaxAttempts != nil {
		if *spec.MaxAttempts < 1 || *spec.MaxAttempts > maxAttemptsCeiling {
			return nj, fmt.Errorf("%w: max_attempts must be between 1 and %d", ErrInvalid, maxAttemptsCeiling)
		}
		nj.MaxAttempts = *spec.MaxAttempts
	}
	if len(nj.Payload) == 0 || bytes.Equal(nj.Payload, []byte("null")) {
		nj.Payload = json.RawMessage("{}")
	}
	return nj, nil
}

// List returns a page of jobs and the cursor for the next page, which is 0 when
// there are no more.
func (q *Queue) List(ctx context.Context, teamID int64, f models.JobFilter) ([]models.Job, int64, error) {
	if f.Status != "" && !f.Status.Valid() {
		return nil, 0, fmt.Errorf("%w: unknown status %q", ErrInvalid, f.Status)
	}
	if f.Limit <= 0 {
		f.Limit = defaultListLimit
	}
	f.Limit = min(f.Limit, maxListLimit)

	list, err := q.repo.List(ctx, teamID, f)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) == f.Limit {
		next = list[len(list)-1].ID
	}
	return list, next, nil
}

func (q *Queue) Get(ctx context.Context, teamID, id int64) (models.Job, []models.JobEvent, error) {
	job, err := q.repo.Get(ctx, teamID, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return job, nil, ErrNotFound
	}
	if err != nil {
		return job, nil, err
	}
	events, err := q.repo.Events(ctx, id)
	return job, events, err
}

func (q *Queue) Cancel(ctx context.Context, teamID, id int64) (models.Job, error) {
	job, err := q.repo.Cancel(ctx, teamID, id)
	return job, q.explainMiss(ctx, teamID, id, err, "cancel")
}

func (q *Queue) Retry(ctx context.Context, teamID, id int64) (models.Job, error) {
	job, err := q.repo.Retry(ctx, teamID, id)
	return job, q.explainMiss(ctx, teamID, id, err, "retry")
}

// explainMiss turns a state change that matched no row into either "not found"
// or "the job is in the wrong state for that".
func (q *Queue) explainMiss(ctx context.Context, teamID, id int64, err error, action string) error {
	if !errors.Is(err, repositories.ErrNotFound) {
		return err
	}
	job, err := q.repo.Get(ctx, teamID, id)
	if errors.Is(err, repositories.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: can't %s a %s job", ErrConflict, action, job.Status)
}
