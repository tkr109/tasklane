package services

import (
	"sync"
	"testing"
	"time"
)

type sent struct {
	percent int
	at      time.Time
}

// recorder collects what a progress throttle sends.
type recorder struct {
	mu   sync.Mutex
	sent []sent
}

func (r *recorder) send(percent int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sent{percent, time.Now()})
}

func (r *recorder) all() []sent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sent(nil), r.sent...)
}

func TestProgressSendsAtMostOncePerInterval(t *testing.T) {
	const every = 50 * time.Millisecond
	var r recorder
	p := newProgress(every, r.send)

	start := time.Now()
	for i := 1; i <= 100; i++ {
		p.report(i)
		time.Sleep(time.Millisecond)
	}
	took := time.Since(start)
	time.Sleep(2 * every)

	got := r.all()
	if got[0].percent != 1 || got[0].at.Sub(start) > 5*time.Millisecond {
		t.Errorf("the first report went out as %d after %v, want 1 at once", got[0].percent, got[0].at.Sub(start))
	}
	if last := got[len(got)-1].percent; last != 100 {
		t.Errorf("the last value sent was %d, want 100", last)
	}
	if most := int(took/every) + 2; len(got) > most {
		t.Errorf("%d sends in %v, want at most %d", len(got), took, most)
	}
	for i := 1; i < len(got); i++ {
		if gap := got[i].at.Sub(got[i-1].at); gap < every-2*time.Millisecond {
			t.Errorf("sends %d and %d were %v apart, want at least %v", i-1, i, gap, every)
		}
	}
}

func TestProgressStopDropsAWaitingReport(t *testing.T) {
	const every = 30 * time.Millisecond
	var r recorder
	p := newProgress(every, r.send)

	p.report(10)
	p.report(20)
	p.stop()
	p.report(30)
	time.Sleep(2 * every)

	if got := r.all(); len(got) != 1 || got[0].percent != 10 {
		t.Fatalf("sent %v, want only the 10 from before stop", got)
	}
}
