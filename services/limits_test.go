package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tkr109/tasklane/models"
)

func TestUntilTokens(t *testing.T) {
	const free = 100.0 / 60 // the free plan's jobs per second
	tests := []struct {
		have float64
		want int
		rate float64
		wait time.Duration
	}{
		{0, 1, 10, 100 * time.Millisecond},
		{0.5, 1, 10, 50 * time.Millisecond},
		{3, 1, 10, 0},
		{1, 1, 10, 0},
		{0, 1, free, 600 * time.Millisecond},
		{5.5, 10, free, 2700 * time.Millisecond},
		{0, 200, free, 2 * time.Minute},
	}
	for _, tt := range tests {
		got := untilTokens(tt.have, tt.want, tt.rate)
		if diff := got - tt.wait; diff < -time.Microsecond || diff > time.Microsecond {
			t.Errorf("untilTokens(%v, %d, %.3f) = %v, want %v", tt.have, tt.want, tt.rate, got, tt.wait)
		}
	}
}

func TestAdmitRefusesMoreThanTheBurst(t *testing.T) {
	limits := NewLimits(nil, nil, 0)
	team := models.Team{ID: 1, Plan: models.Plan{Name: "free", JobsPerMinute: 100, Burst: 200}}
	if _, err := limits.Admit(context.Background(), team, 201); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a batch over the burst: err = %v, want ErrInvalid", err)
	}
}

func TestTypeBurst(t *testing.T) {
	tests := map[float64]int{0.5: 1, 1: 1, 10: 1, 25: 2, 200: 20}
	for rate, want := range tests {
		if got := typeBurst(rate); got != want {
			t.Errorf("typeBurst(%v) = %d, want %d", rate, got, want)
		}
	}
}
