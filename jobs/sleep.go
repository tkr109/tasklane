package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Sleep waits for the requested number of seconds, reporting progress in tenths.
func Sleep(ctx context.Context, payload json.RawMessage, progress Progress) error {
	var p struct {
		Seconds float64 `json:"seconds"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("sleep: bad payload: %w", err)
	}
	if p.Seconds <= 0 {
		p.Seconds = 5
	}

	const steps = 10
	step := time.Duration(p.Seconds * float64(time.Second) / steps)
	for i := 1; i <= steps; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(step):
		}
		progress(i * 100 / steps)
	}
	return nil
}
