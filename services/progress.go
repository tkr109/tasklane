package services

import (
	"sync"
	"time"
)

// progressEvery spaces out a job's progress writes, and with them its progress
// events, to about two a second.
const progressEvery = 500 * time.Millisecond

// progress passes a job's progress reports on at most once per interval. A report
// that comes sooner waits for the interval to end, and only the latest waiting
// value goes out then, so a job's last report is never lost.
type progress struct {
	every time.Duration
	send  func(percent int)

	mu      sync.Mutex
	sentAt  time.Time
	pending int
	timer   *time.Timer
	stopped bool
}

func newProgress(every time.Duration, send func(percent int)) *progress {
	return &progress{every: every, send: send}
}

func (p *progress) report(percent int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.pending = percent
	if p.timer != nil {
		return
	}
	if wait := p.every - time.Since(p.sentAt); wait > 0 {
		p.timer = time.AfterFunc(wait, p.flush)
		return
	}
	p.sentAt = time.Now()
	p.send(percent)
}

func (p *progress) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timer = nil
	if p.stopped {
		return
	}
	p.sentAt = time.Now()
	p.send(p.pending)
}

// stop drops a waiting report. Once it returns no send is under way or to come, so
// nothing lands after the job's outcome.
func (p *progress) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
	}
}
