package services

import (
	"testing"
	"time"
)

func TestBackoffGrowsWithinBounds(t *testing.T) {
	base, ceiling := time.Second, time.Minute
	tests := []struct {
		attempt int
		window  time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{6, 32 * time.Second},
		{7, time.Minute},
		{50, time.Minute},
	}
	for _, tt := range tests {
		for range 200 {
			got := backoff(tt.attempt, base, ceiling)
			if got < tt.window/2 || got > tt.window {
				t.Fatalf("attempt %d: got %v, want within [%v, %v]", tt.attempt, got, tt.window/2, tt.window)
			}
		}
	}
}

func TestBackoffHasJitter(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 50 {
		seen[backoff(4, time.Second, time.Minute)] = true
	}
	if len(seen) < 10 {
		t.Fatalf("expected spread out delays, got %d distinct values", len(seen))
	}
}

func TestBackoffDoesNotOverflow(t *testing.T) {
	got := backoff(1000, 24*time.Hour, 365*24*time.Hour)
	if got <= 0 || got > 365*24*time.Hour {
		t.Fatalf("got %v", got)
	}
}
