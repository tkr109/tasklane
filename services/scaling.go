package services

import (
	"fmt"
	"time"

	"github.com/tkr109/tasklane/models"
)

// policy is the scaler's arithmetic, apart from Docker and Postgres. It wants one
// worker per jobsPerWorker runnable jobs, adds them at once, and removes them one
// at a time once it has wanted fewer for a whole cool-down.
type policy struct {
	min, max      int
	jobsPerWorker int
	// concurrency is how many jobs one worker runs at once.
	concurrency int
	cooldown    time.Duration
	stepDown    time.Duration
	// interval is how often the scaler decides.
	interval time.Duration

	// fewerSince is when the scaler began wanting fewer workers than it has, and
	// zero while it doesn't.
	fewerSince   time.Time
	lastStepDown time.Time
	// holdFrom is the pool size of the last hold told, or 0. Another hold at
	// that size says nothing new.
	holdFrom int
}

type decision struct {
	// action is up, down or hold, or empty when there's nothing to tell.
	action   string
	from, to int
	target   int
	reason   string
}

// target is how many workers the demand calls for, and a note when a bound
// rather than the backlog decided it.
func (p *policy) target(d models.Demand) (int, string) {
	n, note := ceilDiv(d.Runnable, p.jobsPerWorker), ""
	// Workers past what the teams' caps can keep busy would sit idle.
	if busy := ceilDiv(d.Slots, p.concurrency); busy < n {
		n, note = busy, fmt.Sprintf("the teams' plans run %d at once", d.Slots)
	}
	switch {
	case n > p.max:
		n, note = p.max, fmt.Sprintf("at most %d", p.max)
	case n < p.min:
		n, note = p.min, fmt.Sprintf("at least %d", p.min)
	}
	return n, note
}

func (p *policy) decide(now time.Time, d models.Demand, current int) decision {
	target, note := p.target(d)
	dec := decision{from: current, to: current, target: target}
	switch {
	case target > current:
		p.fewerSince, p.holdFrom = time.Time{}, 0
		dec.action, dec.to = "up", target
		dec.reason = fmt.Sprintf("%d runnable, oldest wait %s", d.Runnable, d.OldestWait.Round(time.Second))
		if d.Held > 0 {
			dec.reason += fmt.Sprintf(", %d held by a rate limit not counted", d.Held)
		}
		if note != "" {
			dec.reason += "; " + note
		}
	case target < current && p.fewerSince.IsZero():
		p.fewerSince = now
		// A queue that empties and refills restarts the cool-down each time.
		if p.holdFrom == current {
			break
		}
		p.holdFrom = current
		dec.action = "hold"
		dec.reason = fmt.Sprintf("wants %d: waiting out the %s cool-down before scaling down", target, p.cooldown)
	case target < current:
		if !p.passed(p.fewerSince, p.cooldown, now) || !p.passed(p.lastStepDown, p.stepDown, now) {
			break
		}
		p.lastStepDown, p.holdFrom = now, 0
		dec.action, dec.to = "down", current-1
		if d.Runnable == 0 {
			dec.reason = "queue empty, cool-down passed"
		} else {
			dec.reason = fmt.Sprintf("%d runnable, wanted %d for %s", d.Runnable, target, now.Sub(p.fewerSince).Round(time.Second))
		}
	default:
		p.fewerSince = time.Time{}
	}
	return dec
}

// reset forgets the cool-down and the last hold.
func (p *policy) reset() {
	p.fewerSince, p.lastStepDown, p.holdFrom = time.Time{}, time.Time{}, 0
}

// scaleDownAt is when the cool-down ends, or nil if there is none under way.
func (p *policy) scaleDownAt() *time.Time {
	if p.fewerSince.IsZero() {
		return nil
	}
	at := p.fewerSince.Add(p.cooldown).UTC()
	return &at
}

// passed reports whether d has gone by since t. The loop runs a little late at
// times, so half an interval of slack keeps a 3s step from slipping to 4s.
func (p *policy) passed(t time.Time, d time.Duration, now time.Time) bool {
	return now.Sub(t) >= d-p.interval/2
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}
