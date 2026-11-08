package config

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestParseRates(t *testing.T) {
	tests := []struct {
		in   string
		want map[string]float64
	}{
		{"", map[string]float64{}},
		{"email=10/s", map[string]float64{"email": 10}},
		{" email = 10/s , thumbnail=600/m,", map[string]float64{"email": 10, "thumbnail": 10}},
		{"slow=1/m", map[string]float64{"slow": 1.0 / 60}},
	}
	for _, tt := range tests {
		got, err := parseRates(tt.in)
		if err != nil {
			t.Errorf("parseRates(%q): %v", tt.in, err)
			continue
		}
		if !maps.Equal(got, tt.want) {
			t.Errorf("parseRates(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}

	for _, bad := range []string{"email", "email=10", "=10/s", "email=ten/s", "email=0/s", "email=-1/s", "email=10/h"} {
		if _, err := parseRates(bad); err == nil {
			t.Errorf("parseRates(%q) should fail", bad)
		}
	}
}

func TestScalerPassesSetSettingsOn(t *testing.T) {
	t.Setenv("WORKER_ENV", "DATABASE_URL, SMTP_HOST,NOT_SET_ANYWHERE")
	t.Setenv("DATABASE_URL", "postgres://db/tasklane")
	t.Setenv("SMTP_HOST", "mailpit")
	t.Setenv("WORKER_VOLUMES", "tasklane_jobdata:/data,")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DATABASE_URL=postgres://db/tasklane", "SMTP_HOST=mailpit"}
	if !slices.Equal(cfg.Scaler.Docker.Env, want) {
		t.Errorf("env %v, want %v", cfg.Scaler.Docker.Env, want)
	}
	if !slices.Equal(cfg.Scaler.Docker.Volumes, []string{"tasklane_jobdata:/data"}) {
		t.Errorf("volumes %v", cfg.Scaler.Docker.Volumes)
	}
}

func TestScalerBoundsMustMakeSense(t *testing.T) {
	t.Setenv("SCALER_MIN", "5")
	t.Setenv("SCALER_MAX", "3")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SCALER_MAX") {
		t.Errorf("a max below the min loaded with err %v", err)
	}
}
