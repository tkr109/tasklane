package services

import (
	"errors"
	"testing"
	"time"

	"github.com/tkr109/tasklane/config"
	"github.com/tkr109/tasklane/jobs"
)

func TestNextRun(t *testing.T) {
	utc := func(s string) time.Time {
		at, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return at
	}
	tests := []struct {
		cron, tz string
		after    string
		want     string
	}{
		{"*/15 * * * *", "UTC", "2026-03-02T10:07:30Z", "2026-03-02T10:15:00Z"},
		{"*/15 * * * *", "UTC", "2026-03-02T10:15:00Z", "2026-03-02T10:30:00Z"},
		{"@hourly", "UTC", "2026-03-02T10:30:15Z", "2026-03-02T11:00:00Z"},
		{"0 0 1 1 *", "UTC", "2026-03-02T10:00:00Z", "2027-01-01T00:00:00Z"},
		// Weekdays at 9 in India: from a Friday morning to Monday, 09:00 IST is 03:30 UTC.
		{"0 9 * * 1-5", "Asia/Kolkata", "2026-03-06T05:00:00Z", "2026-03-09T03:30:00Z"},
		// 9am in New York is 14:00 UTC before the clocks go forward on 8 March, 13:00 after.
		{"0 9 * * *", "America/New_York", "2026-03-07T15:00:00Z", "2026-03-08T13:00:00Z"},
		{"@every 2h", "UTC", "2026-03-02T10:00:00Z", "2026-03-02T12:00:00Z"},
	}
	for _, tt := range tests {
		got, err := nextRun(tt.cron, tt.tz, utc(tt.after))
		if err != nil {
			t.Errorf("nextRun(%q, %s): %v", tt.cron, tt.tz, err)
			continue
		}
		if want := utc(tt.want); !got.Equal(want) {
			t.Errorf("nextRun(%q, %s, %s) = %s, want %s", tt.cron, tt.tz, tt.after, got.UTC().Format(time.RFC3339), tt.want)
		}
	}
}

func TestNextRunRejectsBadSchedules(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct{ cron, tz string }{
		{"61 * * * *", "UTC"},
		{"* * * *", "UTC"},
		{"0 0 30 2 *", "UTC"}, // 30 February
		{"0 9 * * *", "Mars/Olympus"},
	} {
		if _, err := nextRun(tt.cron, tt.tz, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("nextRun(%q, %q): err = %v, want ErrInvalid", tt.cron, tt.tz, err)
		}
	}
}

func TestBuildSchedule(t *testing.T) {
	s := NewSchedules(nil, jobs.Defaults(config.Jobs{}))
	now := time.Date(2026, 3, 2, 10, 7, 0, 0, time.UTC)

	sc, err := s.build(1, ScheduleSpec{Cron: " * * * * * ", Type: "sleep"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Cron != "* * * * *" || sc.Timezone != "UTC" || !sc.Enabled || sc.Priority != 5 || sc.MaxAttempts != 3 {
		t.Errorf("defaults: %+v", sc)
	}
	if want := now.Add(time.Minute); !sc.NextRunAt.Equal(want) {
		t.Errorf("next run %v, want %v", sc.NextRunAt, want)
	}

	off := false
	if sc, err := s.build(1, ScheduleSpec{Cron: "@daily", Type: "sleep", Enabled: &off}, now); err != nil || sc.Enabled {
		t.Errorf("a disabled schedule: enabled=%v err=%v", sc.Enabled, err)
	}

	tooHigh := 12
	for name, spec := range map[string]ScheduleSpec{
		"more than once a minute":  {Cron: "@every 30s", Type: "sleep"},
		"a zone in the expression": {Cron: "CRON_TZ=Asia/Tokyo 0 9 * * *", Type: "sleep"},
		"the host's zone":          {Cron: "0 9 * * *", Timezone: "Local", Type: "sleep"},
		"an unknown job type":      {Cron: "0 9 * * *", Type: "nope"},
		"a bad priority":           {Cron: "0 9 * * *", Type: "sleep", Priority: &tooHigh},
	} {
		if _, err := s.build(1, spec, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
