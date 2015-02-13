package snappy

import (
	"errors"
)

var (
	// ErrPackageNotFound is returned when a snap can not be found
	ErrPackageNotFound = errors.New("Snappy package not found")
	// ErrNeedRoot is returned when a command needs root privs but
	// the caller is not root
	ErrNeedRoot = errors.New("This command requires root access. Please re-run using 'sudo'.")
)
