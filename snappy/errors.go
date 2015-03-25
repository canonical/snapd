package snappy

import (
	"errors"
)

var (
	// ErrPackageNotFound is returned when a snap can not be found
	ErrPackageNotFound = errors.New("snappy package not found")

	// ErrNeedRoot is returned when a command needs root privs but
	// the caller is not root
	ErrNeedRoot = errors.New("this command requires root access. Please re-run using 'sudo'")

	// ErrPackageNotRemovable is returned when trying to remove a package
	// that cannot be removed.
	ErrPackageNotRemovable = errors.New("snappy package cannot be removed")

	// ErrConfigNotFound is returned if a snap without a config is
	// getting configured
	ErrConfigNotFound = errors.New("no config found for this snap")

	// ErrInvalidHWDevice is returned when a invalid hardware device
	// is given in the hw-assign command
	ErrInvalidHWDevice = errors.New("invalid hardware device")

	// ErrHWAccessRemoveNotFound is returned if the user tries to
	// remove a device that does not exist
	ErrHWAccessRemoveNotFound = errors.New("can not find device in hw-access list")

	// ErrHWAccessAlreadyAdded is returned if you try to add a device
	// that is already in the hwaccess list
	ErrHWAccessAlreadyAdded = errors.New("device is already in hw-access list")

	// ErrReadmeInvalid is returned if the package contains a invalid
	// meta/readme.md
	ErrReadmeInvalid = errors.New("meta/readme.md invalid")

	// ErrAuthenticationNeeds2fa is returned if the authentication
	// needs 2factor
	ErrAuthenticationNeeds2fa = errors.New("authentication needs second factor")

	// ErrNotInstalled is returned when the snap is not installed
	ErrNotInstalled = errors.New("the given snap is not installed")

	// ErrPrivOpInProgress is returned when a privileged operation
	// cannot be performed since an existing privileged operation is
	// still running.
	ErrPrivOpInProgress = errors.New("privileged operation already in progress")

	// ErrInvalidCredentials is returned on login error
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrInvalidPackageYaml is returned is a package.yaml file can not
	// be parsed
	ErrInvalidPackageYaml = errors.New("can not parse package.yaml")

	// ErrSnapNotActive is returned if you try to unset a snap from
	// active to inactive
	ErrSnapNotActive = errors.New("snap not active")

	ErrLicenseNotAccepted = errors.New("license not accepted")
	ErrLicenseNotProvided = errors.New("package.yaml requires license, but no license was provided")
)
