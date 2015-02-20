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

	// ErrRemoteSnapNotFound indicates that no snap with that name was
	// found in a remote repository
	ErrRemoteSnapNotFound = errors.New("Remote Snap not found")

	// ErrPackageNotRemovable is returned when trying to remove a package
	// that cannot be removed.
	ErrPackageNotRemovable = errors.New("snappy package cannot be removed")

	// ErrConfigNotFound is returned if a snap without a config is
	// getting configured
	ErrConfigNotFound = errors.New("No config found for this snap")
)
