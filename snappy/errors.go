/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snappy

import (
	"errors"
	"fmt"
	"strings"
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

	// ErrAlreadyInstalled is returned when the snap is already installed
	ErrAlreadyInstalled = errors.New("the given snap is already installed")

	// ErrForkAlreadyInstalled is returned when you try to install
	// a fork of something you already have installed
	ErrForkAlreadyInstalled = errors.New("a package by that name is already installed")

	// ErrPrivOpInProgress is returned when a privileged operation
	// cannot be performed since an existing privileged operation is
	// still running.
	ErrPrivOpInProgress = errors.New("privileged operation already in progress")

	// ErrInvalidCredentials is returned on login error
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrInvalidPackageYaml is returned if a package.yaml file can not
	// be parsed
	ErrInvalidPackageYaml = errors.New("can not parse package.yaml")

	// ErrInvalidFrameworkSpecInYaml is returned if a package.yaml
	// has both frameworks and framework entries.
	ErrInvalidFrameworkSpecInYaml = errors.New(`yaml can't have both "frameworks" and (deprecated) "framework" keys`)

	// ErrSnapNotActive is returned if you try to unset a snap from
	// active to inactive
	ErrSnapNotActive = errors.New("snap not active")

	// ErrBuildPlatformNotSupported is returned if you build on
	// a not (yet) supported platform
	ErrBuildPlatformNotSupported = errors.New("building on a not (yet) supported platform")

	// ErrUnpackHelperNotFound is returned if the unpack helper
	// can not be found
	ErrUnpackHelperNotFound = errors.New("unpack helper not found, do you have snappy installed in your PATH or GOPATH?")

	// ErrLicenseNotAccepted is returned when the user does not accept the
	// license
	ErrLicenseNotAccepted = errors.New("license not accepted")
	// ErrLicenseBlank is returned when the package specifies that
	// accepting license is required, but the license file was empty or
	// blank
	ErrLicenseBlank = errors.New("package.yaml requires accepting a license, but license file was blank")
	// ErrLicenseNotProvided is returned when the package specifies that
	// accepting a license is required, but no license file is provided
	ErrLicenseNotProvided = errors.New("package.yaml requires license, but no license was provided")

	// ErrNotFirstBoot is an error that indicates that the first boot has already
	// run
	ErrNotFirstBoot = errors.New("this is not your first boot")

	// ErrNotImplemented may be returned when an implementation of
	// an interface is partial.
	ErrNotImplemented = errors.New("not implemented")

	// ErrNoOemConfiguration may be returned when there is a SnapTypeOem installed
	// but does not provide a configuration.
	ErrNoOemConfiguration = errors.New("no configuration entry found in the oem snap")

	// ErrInstalledNonSnapPart is returned if a part that is purportedly
	// installed turns out to not be a SnapPart.
	ErrInstalledNonSnapPart = errors.New("installed dependent snap is not a SnapPart")

	// ErrSideLoaded is returned on system update if the system was
	// created with a custom enablement part.
	ErrSideLoaded = errors.New("cannot update system that uses custom enablement")
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

// ErrStructIllegalContent is returned if a struct contains illegal content
// as matched via "verifyWhitelistForStruct"
type ErrStructIllegalContent struct {
	field     string
	content   string
	whitelist string
}

func (e *ErrStructIllegalContent) Error() string {
	return fmt.Sprintf("services description field '%s' contains illegal '%s' (legal: '%s')", e.field, e.content, e.whitelist)
}

// ErrGarbageCollectImpossible is alerting about some of the assumptions of the
// garbage collector not being true (and thus not safe to run the gc).
type ErrGarbageCollectImpossible string

func (e ErrGarbageCollectImpossible) Error() string {
	return fmt.Sprintf("garbage collection impossible: prerequisites untrue: %s", string(e))
}

// ErrNameClash reports a conflict between a named service and binary in a package.
type ErrNameClash string

func (e ErrNameClash) Error() string {
	return fmt.Sprintf("you can't have a binary and service both called %s", string(e))
}

// ErrMissingFrameworks reports a conflict between the frameworks needed by an app and those installed in the system
type ErrMissingFrameworks []string

func (e ErrMissingFrameworks) Error() string {
	return fmt.Sprintf("missing frameworks: %s", strings.Join(e, ", "))
}

// ErrFrameworkInUse reports that a framework is still needed by apps currently installed
type ErrFrameworkInUse []string

func (e ErrFrameworkInUse) Error() string {
	return fmt.Sprintf("framework still in use by: %s", strings.Join(e, ", "))
}
