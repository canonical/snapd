package snappy

import (
	"errors"
)

var (
	ErrPackageNotFound = errors.New("Snappy package not found")
	ErrNeedRoot        = errors.New("This command requires root access. Please re-run using 'sudo'.")
)
