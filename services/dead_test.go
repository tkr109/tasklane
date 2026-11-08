package services

import (
	"context"
	"testing"
)

func TestGroupIDRoundTrips(t *testing.T) {
	for _, g := range [][2]string{
		{"email", "connect to <n>.<n>.<n>.<n>:<n>: timed out"},
		{"webhook", "bad status\nwith a second line"},
		{"sleep", ""},
	} {
		jobType, pattern, ok := parseGroupID(groupID(g[0], g[1]))
		if !ok || jobType != g[0] || pattern != g[1] {
			t.Errorf("round trip of %q: got %q %q %v", g, jobType, pattern, ok)
		}
	}
	for _, bad := range []string{"", "!!", groupID("", "x"), "c2xlZXA"} {
		if _, _, ok := parseGroupID(bad); ok {
			t.Errorf("parseGroupID(%q) should fail", bad)
		}
	}
}

func TestErrorsAreGroupedByTheirShape(t *testing.T) {
	pool := testDB(t)
	tests := []struct{ msg, want string }{
		{"boom", "boom"},
		{"connect to 10.0.0.5:443: i/o timeout", "connect to <n>.<n>.<n>.<n>:<n>: i/o timeout"},
		{`user "bob" not found`, "user <str> not found"},
		{"order 'A-17' is locked", "order <str> is locked"},
		{"no mailbox for bob@example.com", "no mailbox for <email>"},
		{"job 3f2b9c1e-8a7d-4b6e-9f01-2c3d4e5f6a7b failed after 1500ms", "job <id> failed after <n>"},
		{"status 503 from s3", "status <n> from <n>"},
		{"", ""},
	}
	for _, tt := range tests {
		var got string
		if err := pool.QueryRow(context.Background(), `select normalize_error($1)`, tt.msg).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("normalize_error(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}
