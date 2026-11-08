package services

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestNewKeyFormat(t *testing.T) {
	format := regexp.MustCompile(`^tk_live_[0-9a-f]{40}$`)
	a, b := newKey(), newKey()
	if !format.MatchString(a) {
		t.Fatalf("key %q does not look like tk_live_<40 hex>", a)
	}
	if a == b {
		t.Fatal("two keys came out the same")
	}
	if got := displayPrefix(a); got != a[:14] {
		t.Fatalf("display prefix = %q, want %q", got, a[:14])
	}
	if len(hashKey(a)) != 32 {
		t.Fatalf("hash is %d bytes, want a sha256", len(hashKey(a)))
	}
}

func TestAuthRejectsKeysWithoutThePrefix(t *testing.T) {
	auth := NewAuth(nil, 0)
	for _, key := range []string{"", "secret", "tk_test_abc", "TK_LIVE_abc"} {
		if _, err := auth.Team(context.Background(), key); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("key %q: err = %v, want ErrUnauthorized", key, err)
		}
	}
}
