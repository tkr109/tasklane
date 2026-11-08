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
func Flaky(ctx context.Context, payload json.RawMessage, progress Progress) error {
	p := struct {
		FailRate *float64 `json:"fail_rate"`
	}{}
	if err := json.Unmarshal(payload, &p); err != nil {
		return Permanent(fmt.Errorf("flaky: bad payload: %w", err))
	}
	rate := 0.5
	if p.FailRate != nil {
		rate = *p.FailRate
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	if rand.Float64() < rate {
		return errors.New("flaky: rolled a failure")
	}
	progress(100)
	return nil
}
