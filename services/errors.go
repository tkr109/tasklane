package services

import "errors"

// Controllers map these to HTTP statuses. Wrap them to say what was wrong.
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
)
