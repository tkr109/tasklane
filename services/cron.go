package services

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Five fields, minute to day of week, or a descriptor such as @hourly or @every 2h.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// nextRun is the first time the cron expression fires after the given time, in
// the given time zone.
func nextRun(expr, timezone string, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: unknown time zone %q", ErrInvalid, timezone)
	}
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: cron %q: %v", ErrInvalid, expr, err)
	}
	next := sched.Next(after.In(loc))
	if next.IsZero() {
		return next, fmt.Errorf("%w: cron %q never fires", ErrInvalid, expr)
	}
	return next, nil
}
