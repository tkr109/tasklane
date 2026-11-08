package services

import (
	"math/rand/v2"
	"time"
)

// backoff is the wait before the retry that follows the given attempt (1-based).
// The delay doubles each attempt up to ceiling, and the jitter keeps it in the
// upper half of that window so retries from a burst of failures spread out.
func backoff(attempt int, base, ceiling time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt && d < ceiling; i++ {
		d *= 2
	}
	d = min(d, ceiling)
	half := d / 2
	return half + rand.N(d-half+1)
}
