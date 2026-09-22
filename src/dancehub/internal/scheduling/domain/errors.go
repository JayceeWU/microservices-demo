package domain

import "errors"

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrCapacityExceeded  = errors.New("class capacity exceeded")
	ErrWindowClosed      = errors.New("operation window is closed")
	ErrPermissionDenied  = errors.New("permission denied")
)
