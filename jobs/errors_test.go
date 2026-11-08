package jobs

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsPermanent(t *testing.T) {
	err := Permanent(errors.New("bad payload"))
	if !IsPermanent(err) || !IsPermanent(fmt.Errorf("webhook: %w", err)) {
		t.Error("a permanent error, wrapped or not, should be permanent")
	}
	if IsPermanent(errors.New("connection refused")) {
		t.Error("a plain error should be retried")
	}
	if err.Error() != "bad payload" {
		t.Errorf("Error() = %q", err.Error())
	}
}
