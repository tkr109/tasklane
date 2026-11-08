package jobs

import "errors"

// Permanent marks err as one that retrying won't fix, such as a bad payload or a
// 4xx answer. The worker sends the job straight to dead.
func Permanent(err error) error {
	return permanentError{err}
}

func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

type permanentError struct {
	err error
}

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }
