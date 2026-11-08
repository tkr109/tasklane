package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Flaky does a little fake work and then fails with probability fail_rate
// (default 0.5). It exists to exercise retries and dead jobs.
func Flaky(ctx context.Context, payload json.RawMessage, progress Progress) (any, error) {
	p := struct {
		FailRate *float64 `json:"fail_rate"`
	}{}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, Permanent(fmt.Errorf("flaky: bad payload: %w", err))
	}
	rate := 0.5
	if p.FailRate != nil {
		rate = *p.FailRate
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	if rand.Float64() < rate {
		return nil, errors.New("flaky: rolled a failure")
	}
	progress(100)
	return nil, nil
}
