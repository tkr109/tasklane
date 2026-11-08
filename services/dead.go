package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var errDeadGroupNotFound = fmt.Errorf("dead job group %w", ErrNotFound)

const (
	defaultGroupJobs = 5
	maxGroupJobs     = 50
)

// DeadJobs groups jobs that ran out of attempts by type and error, so the
// dashboard can show one line per failure and retry a whole group.
type DeadJobs struct {
	repo *repositories.Jobs
}

func NewDeadJobs(repo *repositories.Jobs) *DeadJobs {
	return &DeadJobs{repo: repo}
}

// Groups returns every group and the number of dead jobs in all of them.
func (d *DeadJobs) Groups(ctx context.Context) ([]models.DeadGroup, int, error) {
	groups, err := d.repo.DeadGroups(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	for i := range groups {
		groups[i].ID = groupID(groups[i].Type, groups[i].Pattern)
		total += groups[i].Count
	}
	return groups, total, nil
}

func (d *DeadJobs) Jobs(ctx context.Context, id string, limit int) ([]models.Job, error) {
	jobType, pattern, ok := parseGroupID(id)
	if !ok {
		return nil, errDeadGroupNotFound
	}
	if limit <= 0 {
		limit = defaultGroupJobs
	}
	return d.repo.DeadGroupJobs(ctx, jobType, pattern, min(limit, maxGroupJobs))
}

// Retry queues every job in the group again. A group someone else has just
// retried has nothing left, which is not an error.
func (d *DeadJobs) Retry(ctx context.Context, id string) (int64, error) {
	jobType, pattern, ok := parseGroupID(id)
	if !ok {
		return 0, errDeadGroupNotFound
	}
	return d.repo.RetryDeadGroup(ctx, jobType, pattern)
}

// groupID names a group in URLs. It encodes the group's key itself, so there is
// no table of groups to keep. Type names never hold a newline.
func groupID(jobType, pattern string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(jobType + "\n" + pattern))
}

func parseGroupID(id string) (jobType, pattern string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", false
	}
	jobType, pattern, ok = strings.Cut(string(b), "\n")
	return jobType, pattern, ok && jobType != ""
}
