package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tkr109/tasklane/models"
	"github.com/tkr109/tasklane/repositories"
)

var ErrUnauthorized = errors.New("missing or unknown API key")

const keyPrefix = "tk_live_"

func newKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	return keyPrefix + hex.EncodeToString(b)
}

func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// displayPrefix is the start of a key, enough for people to tell keys apart.
func displayPrefix(key string) string {
	return key[:min(len(key), len(keyPrefix)+6)]
}

// Auth resolves API keys to teams. Answers are cached for a short TTL, so a plan
// switch, or a key revoked through another process, takes up to that long to show.
type Auth struct {
	keys *repositories.Keys
	ttl  time.Duration

	mu    sync.Mutex
	cache map[string]cachedTeam
}

type cachedTeam struct {
	team    models.Team
	expires time.Time
}

func NewAuth(keys *repositories.Keys, ttl time.Duration) *Auth {
	return &Auth{keys: keys, ttl: ttl, cache: map[string]cachedTeam{}}
}

func (a *Auth) Team(ctx context.Context, key string) (models.Team, error) {
	if !strings.HasPrefix(key, keyPrefix) {
		return models.Team{}, ErrUnauthorized
	}
	hash := hashKey(key)
	if team, ok := a.cached(string(hash)); ok {
		return team, nil
	}

	team, err := a.keys.TeamByHash(ctx, hash)
	if errors.Is(err, repositories.ErrNotFound) {
		return team, ErrUnauthorized
	}
	if err != nil {
		return team, err
	}
	a.mu.Lock()
	a.cache[string(hash)] = cachedTeam{team: team, expires: time.Now().Add(a.ttl)}
	a.mu.Unlock()
	return team, nil
}

// forgetTeam drops every cached key of the team, so a revoke shows here at once.
// Other API processes still catch up within the TTL.
func (a *Auth) forgetTeam(teamID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for hash, c := range a.cache {
		if c.team.ID == teamID {
			delete(a.cache, hash)
		}
	}
}

func (a *Auth) cached(hash string) (models.Team, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.cache[hash]
	if !ok {
		return models.Team{}, false
	}
	if time.Now().After(c.expires) {
		delete(a.cache, hash)
		return models.Team{}, false
	}
	return c.team, true
}

type teamKey struct{}

func WithTeam(ctx context.Context, team models.Team) context.Context {
	return context.WithValue(ctx, teamKey{}, team)
}

// TeamFrom returns the team the request authenticated as, or the zero Team,
// which owns nothing, if there is none.
func TeamFrom(ctx context.Context) models.Team {
	team, _ := ctx.Value(teamKey{}).(models.Team)
	return team
}
