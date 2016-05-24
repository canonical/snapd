// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"github.com/snapcore/snapd/arch"
)

var (
	// ErrPackageNotFound is returned when a snap can not be found
	ErrPackageNotFound = errors.New("snappy package not found")

	// ErrServiceNotFound is returned when a service can not be found
	ErrServiceNotFound = errors.New("snappy service not found")

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

	// ErrNotInstalled is returned when the snap is not installed
	ErrNotInstalled = errors.New("the given snap is not installed")

	// ErrAlreadyInstalled is returned when the snap is already installed
	ErrAlreadyInstalled = errors.New("the given snap is already installed")

	// ErrStillActive is returned when the snap is still installed
	ErrStillActive = errors.New("the given snap is still installed")

	// ErrPackageNameAlreadyInstalled is returned when you try to install
	// a fork of something you already have installed
	ErrPackageNameAlreadyInstalled = errors.New("a package by that name is already installed")

	// ErrGadgetPackageInstall is returned when you try to install
	// a gadget package type on a running system.
	ErrGadgetPackageInstall = errors.New("gadget package installation not allowed")

	// ErrPrivOpInProgress is returned when a privileged operation
	// cannot be performed since an existing privileged operation is
	// still running.
	ErrPrivOpInProgress = errors.New("privileged operation already in progress")

	// ErrSnapNotActive is returned if you try to unset a snap from
	// active to inactive
	ErrSnapNotActive = errors.New("snap not active")

	// ErrBuildPlatformNotSupported is returned if you build on
	// a not (yet) supported platform
	ErrBuildPlatformNotSupported = errors.New("building on a not (yet) supported platform")

	// ErrLicenseNotAccepted is returned when the user does not accept the
	// license
	ErrLicenseNotAccepted = errors.New("license not accepted")
	// ErrLicenseNotProvided is returned when the package specifies that
	// accepting a license is required, but no license file is provided
	ErrLicenseNotProvided = errors.New("snap.yaml requires license, but no license was provided")

	// ErrNotFirstBoot is an error that indicates that the first boot has already
	// run
	ErrNotFirstBoot = errors.New("this is not your first boot")

	// ErrNotImplemented may be returned when an implementation of
	// an interface is partial.
	ErrNotImplemented = errors.New("not implemented")

	// ErrNoGadgetConfiguration may be returned when there is a pkg.TypeGadget installed
	// but does not provide a configuration.
	ErrNoGadgetConfiguration = errors.New("no configuration entry found in the gadget snap")

	// ErrInstalledNonSnap is returned if a snap that is purportedly
	// installed turns out to not be a Snap.
	ErrInstalledNonSnap = errors.New("installed dependent snap is not a Snap")

	// ErrSideLoaded is returned on system update if the system was
	// created with a custom enablement snap.
	ErrSideLoaded = errors.New("cannot update system that uses custom enablement")

	// ErrPackageNameNotSupported is returned when installing legacy package such as those
	// that have the developer specified in their package names.
	ErrPackageNameNotSupported = errors.New("package name with developer not supported")

	// ErrInvalidSnap is returned when something on the filesystem does not make sense
	ErrInvalidSnap = errors.New("invalid package on system")

	// ErrInvalidSeccompPolicy is returned when policy-version and policy-vender are not set together
	ErrInvalidSeccompPolicy = errors.New("policy-version and policy-vendor must be specified together")
	// ErrNoSeccompPolicy is returned when an expected seccomp policy is not provided.
	ErrNoSeccompPolicy = errors.New("no seccomp policy provided")
)

// ErrArchitectureNotSupported is returned when trying to install a snappy package that
// is not supported on the system
type ErrArchitectureNotSupported struct {
	Architectures []string
}

func (e *ErrArchitectureNotSupported) Error() string {
	return fmt.Sprintf("package's supported architectures (%s) is incompatible with this system (%s)", strings.Join(e.Architectures, ", "), arch.UbuntuArchitecture())
}

// ErrInstallFailed is an error type for installation errors for snaps
type ErrInstallFailed struct {
	Snap    string
	OrigErr error
}

// ErrInstallFailed is an error type for installation errors for snaps
func (e *ErrInstallFailed) Error() string {
	return fmt.Sprintf("%s failed to install: %s", e.Snap, e.OrigErr)
}

// ErrHookFailed is returned if a hook command fails
type ErrHookFailed struct {
	Cmd      string
	Output   string
	ExitCode int
}

func (e *ErrHookFailed) Error() string {
	return fmt.Sprintf("hook command %v failed with exit status %d (output: %q)", e.Cmd, e.ExitCode, e.Output)
}

// ErrDataCopyFailed is returned if copying the snap data fialed
type ErrDataCopyFailed struct {
	OldPath  string
	NewPath  string
	ExitCode int
}

func (e *ErrDataCopyFailed) Error() string {
	return fmt.Sprintf("data copy from %v to %v failed with exit status %d", e.OldPath, e.NewPath, e.ExitCode)
}

// ErrUpgradeVerificationFailed is returned if the upgrade has not
// worked (i.e. no new version on the other partition)
type ErrUpgradeVerificationFailed struct {
	Msg string
}

func (e *ErrUpgradeVerificationFailed) Error() string {
	return fmt.Sprintf("upgrade verification failed: %s", e.Msg)
}

// ErrStructIllegalContent is returned if a struct contains illegal content
// as matched via "verifyWhitelistForStruct"
type ErrStructIllegalContent struct {
	Field     string
	Content   string
	Whitelist string
}

func (e *ErrStructIllegalContent) Error() string {
	return fmt.Sprintf("app description field '%s' contains illegal %q (legal: '%s')", e.Field, e.Content, e.Whitelist)
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

// ErrApparmorGenerate is reported if the apparmor profile generation fails
type ErrApparmorGenerate struct {
	ExitCode int
	Output   []byte
}

func (e ErrApparmorGenerate) Error() string {
	return fmt.Sprintf("apparmor generate fails with %v: '%v'", e.ExitCode, string(e.Output))
}

// ErrInvalidYaml is returned if a yaml file can not be parsed
type ErrInvalidYaml struct {
	File string
	Err  error
	Yaml []byte
}

func (e *ErrInvalidYaml) Error() string {
	// %#v of string(yaml) so the yaml is presented as a human-readable string, but in a single greppable line
	return fmt.Sprintf("can not parse %s: %v (from: %#v)", e.File, e.Err, string(e.Yaml))
}

// IsLicenseNotAccepted checks whether err is (directly or indirectly)
// due to a ErrLicenseNotAccepted
func IsLicenseNotAccepted(err error) bool {
	if err == ErrLicenseNotAccepted {
		return true
	}

	if err, ok := err.(*ErrInstallFailed); ok {
		if err.OrigErr == ErrLicenseNotAccepted {
			return true
		}
	}

	return false
}
