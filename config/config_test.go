package config

import (
	"maps"
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
