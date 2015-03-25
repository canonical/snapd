package snappy

import (
	"errors"
	"fmt"
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

	// ErrUnpackHelperNotFound is returned if the unpack helper
	// can not be found
	ErrUnpackHelperNotFound = errors.New("unpack helper not found, do you hae snappy installed in your PATH or GOPATH?")
)

// ErrUnpackFailed is the error type for a snap unpack problem
type ErrUnpackFailed struct {
	snapFile string
	instDir  string
	origErr  error
}

// ErrUnpackFailed is returned if unpacking a snap fails
func (e *ErrUnpackFailed) Error() string {
	return fmt.Sprintf("unpack %s to %s failed with %s", e.snapFile, e.instDir, e.origErr)
}

// ErrSignature is returned if a snap failed the signature verification
type ErrSignature struct {
	exitCode int
}

func (e *ErrSignature) Error() string {
	return fmt.Sprintf("Signature verification failed with exit status %v", e.exitCode)
}

// ErrSystemCtl is returned if the systemctl command failed
type ErrSystemCtl struct {
	cmd      []string
	exitCode int
}

func (e *ErrSystemCtl) Error() string {
	return fmt.Sprintf("%v failed with exit status %d", e.cmd, e.exitCode)
}

// ErrHookFailed is returned if a hook command fails
type ErrHookFailed struct {
	cmd      string
	exitCode int
}

func (e *ErrHookFailed) Error() string {
	return fmt.Sprintf("hook command %v failed with exit status %d", e.cmd, e.exitCode)
}

// ErrDataCopyFailed is returned if copying the snap data fialed
type ErrDataCopyFailed struct {
	oldPath  string
	newPath  string
	exitCode int
}

func (e *ErrDataCopyFailed) Error() string {
	return fmt.Sprintf("data copy from %v to %v failed with exit status %d", e.oldPath, e.newPath, e.exitCode)
}

// ErrUpgradeVerificationFailed is returned if the upgrade has not
// worked (i.e. no new version on the other partition)
type ErrUpgradeVerificationFailed struct {
	msg string
}

func (e *ErrUpgradeVerificationFailed) Error() string {
	return fmt.Sprintf("upgrade verification failed: %s", e.msg)
}
