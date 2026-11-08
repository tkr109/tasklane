package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var (
	errTeamNotFound = fmt.Errorf("team %w", ErrNotFound)
	errKeyNotFound  = fmt.Errorf("API key %w", ErrNotFound)
)

const (
	defaultTeamName = "default"
	maxTeamNameSize = 64
)

// Teams is the admin side: teams, their plans and their API keys.
type Teams struct {
	teams *repositories.Teams
	keys  *repositories.Keys
	auth  *Auth
}

func NewTeams(teams *repositories.Teams, keys *repositories.Keys, auth *Auth) *Teams {
	return &Teams{teams: teams, keys: keys, auth: auth}
}

func (s *Teams) Plans(ctx context.Context) ([]models.Plan, error) {
	return s.teams.Plans(ctx)
}

func (s *Teams) List(ctx context.Context) ([]models.TeamSummary, error) {
	return s.teams.List(ctx)
}

func (s *Teams) Create(ctx context.Context, name, plan string) (models.Team, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxTeamNameSize {
		return models.Team{}, fmt.Errorf("%w: name must be 1 to %d characters", ErrInvalid, maxTeamNameSize)
	}
	p, err := s.plan(ctx, plan)
	if err != nil {
		return models.Team{}, err
	}
	team, err := s.teams.Create(ctx, name, p.ID)
	if errors.Is(err, repositories.ErrDuplicate) {
		return team, fmt.Errorf("%w: a team named %q already exists", ErrConflict, name)
	}
	return team, err
}

// SwitchPlan takes effect for scheduling on the next grab, and for API requests
// once the auth cache lets go of the old plan.
func (s *Teams) SwitchPlan(ctx context.Context, teamID int64, plan string) (models.Team, error) {
	p, err := s.plan(ctx, plan)
	if err != nil {
		return models.Team{}, err
	}
	team, err := s.teams.SetPlan(ctx, teamID, p.ID)
	if errors.Is(err, repositories.ErrNotFound) {
		return team, errTeamNotFound
	}
	return team, err
}

func (s *Teams) plan(ctx context.Context, name string) (models.Plan, error) {
	p, err := s.teams.PlanByName(ctx, name)
	if errors.Is(err, repositories.ErrNotFound) {
		return p, fmt.Errorf("%w: unknown plan %q", ErrInvalid, name)
	}
	return p, err
}

func (s *Teams) Keys(ctx context.Context, teamID int64) ([]models.APIKey, error) {
	if err := s.checkTeam(ctx, teamID); err != nil {
		return nil, err
	}
	return s.keys.List(ctx, teamID)
}

// CreateKey returns the new key in full. This is the only time anyone sees it,
// because only its hash is stored.
func (s *Teams) CreateKey(ctx context.Context, teamID int64) (models.APIKey, string, error) {
	if err := s.checkTeam(ctx, teamID); err != nil {
		return models.APIKey{}, "", err
	}
	secret := newKey()
	key, err := s.keys.Create(ctx, teamID, displayPrefix(secret), hashKey(secret))
	return key, secret, err
}

func (s *Teams) RevokeKey(ctx context.Context, teamID, keyID int64) (models.APIKey, error) {
	key, err := s.keys.Revoke(ctx, teamID, keyID)
	if errors.Is(err, repositories.ErrNotFound) {
		return key, errKeyNotFound
	}
	if err == nil {
		s.auth.forgetTeam(teamID)
	}
	return key, err
}

// EnsureDevKey makes key a working key for the default team, so local runs and
// the dashboard have something to authenticate with.
func (s *Teams) EnsureDevKey(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, keyPrefix) {
		return fmt.Errorf("%w: API keys start with %s", ErrInvalid, keyPrefix)
	}
	teamID, err := s.teams.IDByName(ctx, defaultTeamName)
	if err != nil {
		return fmt.Errorf("look up the default team: %w", err)
	}
	return s.keys.Ensure(ctx, teamID, displayPrefix(key), hashKey(key))
}

func (s *Teams) checkTeam(ctx context.Context, teamID int64) error {
	_, err := s.teams.Get(ctx, teamID)
	if errors.Is(err, repositories.ErrNotFound) {
		return errTeamNotFound
	}
	return err
}
