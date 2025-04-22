// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.

 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package client

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type compoundPreinstallError struct {
	message string
	errs    []error
}

func (c *compoundPreinstallError) Error() string {
	return c.message
}

func NewCompoundPreinstallError(message string, errs ...error) error {
	return &compoundPreinstallError{
		message: message,
		errs:    errs,
	}
}

func NewCompoundPreinstallInternalError(format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	return NewCompoundPreinstallError(message, &PreinstallErrorAndActions{
		Kind:    ErrorKindInternal,
		Message: message,
	})
}

func UnwrapCompoundPreinstallError(err error) ([]PreinstallErrorAndActions, error) {
	compoundErr, ok := err.(*compoundPreinstallError)
	if !ok {
		return nil, fmt.Errorf("cannot unwrap error of unexpected type %T (%v)", reflect.TypeOf(err), err)
	}
	preinstallErrors := make([]PreinstallErrorAndActions, len(compoundErr.errs))
	for i, err := range compoundErr.errs {
		e, ok := err.(*PreinstallErrorAndActions)
		if !ok {
			return nil, fmt.Errorf("cannot unwrap error of unexpected type %T (%v)", reflect.TypeOf(err), err)
		}
		preinstallErrors[i] = *e
	}

	return preinstallErrors, nil
}

// PreinstallErrorAndActions describes a single preinstall check error along
// with corresponding suggested actions when available.
type PreinstallErrorAndActions struct {
	Kind    ErrorKind          `json:"kind"`
	Message string             `json:"message"`
	Args    json.RawMessage    `json:"args,omitempty"`
	Actions []PreinstallAction `json:"actions,omitempty"`
}

func (p *PreinstallErrorAndActions) Error() string {
	return fmt.Sprintf("%s | %s", p.Kind, p.Message)
}

// PreinstallAction describes an Action to resolve a detected error.
// See: github.com/snapcore/secboot/efi/preinstall
type PreinstallAction string

// Preinstall error kinds describe problems detected during secboot preinstall check.
// See: github.com/snapcore/secboot/efi/preinstall
const (
	// ErrorKindNone indicates that no error occurred.
	ErrorKindNone ErrorKind = ""

	// ErrorKindInternal indicates that some kind of unexpected internal error
	// occurred that doesn't have a more appropriate error kind.
	ErrorKindInternal ErrorKind = "internal-error"

	// ErrorKindShutdownRequired indicates that a shutdown is required, and
	// is returned in response to some actions.
	ErrorKindShutdownRequired ErrorKind = "shutdown-required"

	// ErrorKindRebootRequired indicates that a reboot is required, and is
	// returned in response to some actions.
	ErrorKindRebootRequired ErrorKind = "reboot-required"

	// ErrorKindUnexpectedAction indicates that an action was supplied that
	// is unexpected because it isn't a remedial action associated with the
	// previously returned errors, or because the action is not supported.
	ErrorKindUnexpectedAction ErrorKind = "unexpected-action"

	// ErrorKindMissingArgument is returned if an action was supplied
	// that requires one or more arguments, but not enough arguments
	// are supplied.
	ErrorKindMissingArgument ErrorKind = "missing-argument"

	// ErrorKindInvalidArgument is returned if an action was supplied
	// that requires one or more arguments, but one or more of the
	// supplied arguments are of an invalid type of are an invalid value.
	// This will be supplied with a single argument of type int that
	// indicates which argument is invalid (and will be zero-indexed).
	ErrorKindInvalidArgument ErrorKind = "invalid-argument"

	// ErrorKindRunningInVM indicates that the current environment is a
	// virtal machine.
	ErrorKindRunningInVM ErrorKind = "running-in-vm"

	// ErrorKindNoSuitableTPM2Device indicates that the device has no
	// suitable TPM2 device. This is a fatal error. This error means that
	// full-disk encryption is not supported on this device.
	ErrorKindNoSuitableTPM2Device ErrorKind = "no-suitable-tpm2-device"

	// ErrorKindTPMDeviceFailure indicates that the TPM device has failed
	// an internal self check.
	ErrorKindTPMDeviceFailure ErrorKind = "tpm-device-failure"

	// ErrorKindTPMDeviceDisabled indicates that there is a TPM device
	// but it is currently disabled. Note that after enabling it, it may
	// still fail further checks which mean it is unsuitable.
	ErrorKindTPMDeviceDisabled ErrorKind = "tpm-device-disabled"

	// ErrorKindTPMHierarchiesOwned indicates that one or more TPM hierarchy
	// is currently owned, either because it has an authorization value or policy
	// set. This will be supplied with one or more arguments of
	// TPMHierarchyOwnershipInfo structures to indicate which hierarchies are
	// owned and whether it's because they have an authorization value or a policy.
	ErrorKindTPMHierarchiesOwned ErrorKind = "tpm-hierarchies-owned"

	// ErrorKindTPMDeviceLockout indicates that the TPM's dictionary attack
	// logic is currently triggered, preventing the use of any DA protected
	// resources. This will be accompanied with 2 arguments of type time.Duration,
	// with the first argument being the maximum time that it will take for the
	// lockout to clear, and the second argument being the maximum time it will
	// take for the fail count to reach 0.
	ErrorKindTPMDeviceLockout ErrorKind = "tpm-device-lockout"

	// ErrorKindInsufficientTPMStorage indicates that there isn't sufficient
	// storage space available to support FDE along with reprovisioning in
	// the future.
	ErrorKindInsufficientTPMStorage ErrorKind = "insufficient-tpm-storage"

	// ErrorKindNoSuitablePCRBank indicates that it was not possible to select
	// a suitable PCR bank. This could be because some mandatory PCR values are
	// inconsistent with the TCG log.
	// TODO: Expose some information about the error as arguments
	ErrorKindNoSuitablePCRBank ErrorKind = "no-suitable-pcr-bank"

	// ErrorKindMeasuredBoot indicates that there was an error with the TCG log
	// or some other error detected from the TCG log that isn't represented by
	// a more specific error kind.
	ErrorKindMeasuredBoot ErrorKind = "measured-boot"

	// ErrorKindEmptyPCRBanks indicates that one or more PCR banks thar are not
	// present in the TCG log are enabled but have unused PCRs in the TCG defined
	// space (ie, any of PCRs 0-7 are at their reset value). Whilst this isn't an
	// issue for the FDE use case because we can just select a good bank, it does
	// break remote attestation from this device, permitting an adversary to spoof
	// arbitrary trusted platforms by replaying PCR extends from software. This
	// will be accompanied with a slice of arguments of type tpm2.HashAlgorithmId
	// to indicate which banks are broken.
	ErrorKindEmptyPCRBanks ErrorKind = "empty-pcr-banks"

	// ErrorKindTPMCommandFailed indicates that an error occurred whilst
	// executing a TPM command. It will be accompanied with a single argument
	// of the type TPMErrorResponse.
	ErrorKindTPMCommandFailed ErrorKind = "tpm-command-failed"

	// ErrorKindInvalidTPMResponse indicates that the response from the TPM is
	// invalid, which makes it impossible to obtain a response code. This could
	// be because the response packet cannot be decoded, or one or more sessions
	// failed the response HMAC check.
	ErrorKindInvalidTPMResponse ErrorKind = "invalid-tpm-response"

	// ErrorKindTPMCommunication indicates that an error occurred at the transport
	// layer when executing a TPM command.
	ErrorKindTPMCommunication ErrorKind = "tpm-communication"

	// ErrorKindUnsupportedPlatform indicates that the current host platform is
	// not compatible with FDE. This generally occurs because the checks lack
	// the support for testing properties of the current platform, eg, whether
	// there is a correctly configured hardware RTM.
	ErrorKindUnsupportedPlatform ErrorKind = "unsupported-platform"

	// ErrorKindUEFIDebuggingEnabled indicates that the platform firmware currently
	// has a debugging endpoint enabled.
	ErrorKindUEFIDebuggingEnabled ErrorKind = "uefi-debugging-enabled"

	// ErrorKindInsufficientDMAProtection indicates that I/O DMA remapping was
	// disabled during the current boot cycle.
	ErrorKindInsufficientDMAProtection ErrorKind = "insufficient-dma-protection"

	// ErrorKindNoKernelIOMMU indicates that the OS kernel was not built with DMA
	// remapping support, or some configuration has resulted in it being disabled.
	ErrorKindNoKernelIOMMU ErrorKind = "no-kernel-iommu"

	// ErrorKindTPMStartupLocalityNotProtected indicates that the system has a discrete TPM
	// and the startup locality is not protected from access by privileged code running at
	// ring 0, such as the platform firmware or OS kernel. This makes it impossible to enable
	// a mitigation against reset attacks (see the description for DiscreteTPMDetected for more
	// information).
	ErrorKindTPMStartupLocalityNotProtected ErrorKind = "tpm-startup-locality-not-protected"

	// ErrorKindHostSecurity indicates that there is some problem with the system
	// security that isn't represented by a more specific error kind.
	ErrorKindHostSecurity ErrorKind = "host-security"

	// ErrorKindPCRUnusable indicates an error in the way that the platform
	// firmware performs measurements such that the PCR becomes unusable.
	// This will include a single tpm2.Handle argument to indicate which PCR
	// failed.
	ErrorKindPCRUnusable ErrorKind = "tpm-pcr-unusable"

	// ErrorKindPCRUnsupported indicates that a required PCR is currently
	// unsupported by the efi sub-package. This will include 2 arguments - the
	// first one being a tpm2.Handle to indicate which PCR is unsupported. The
	// second argument will be a string containing a URL to a github issue.
	ErrorKindPCRUnsupported ErrorKind = "tpm-pcr-unsupported"

	// ErrorKindVARSuppliedDriversPresent indicates that drivers running from value-added-retailer
	// components were detected. Whilst these should generally be authenticated as part of the
	// secure boot chain and the digsts of the executed code measured to the TPM, the presence of
	// these does increase PCR fragility, and a user may choose not to trust this code (in which
	// case, they will need to disable it somehow).
	// TODO: it might be worth including the device paths from the launch events in PCR2 as an
	// argument.
	ErrorKindVARSuppliedDriversPresent ErrorKind = "var-supplied-drivers-present"

	// ErrorKindSysPrepApplicationsPresent indicates that system preparation applications were
	// detected to be running before the operating system. The OS does not use these an they
	// increase the fragility of PCR4 because they are beyond the control of the operating system.
	// In general, it is recommended that these are disabled.
	// TODO: it might be worth including the device paths from the launch events in PCR4 as an
	// argument.
	ErrorKindSysPrepApplicationsPresent ErrorKind = "sys-prep-applications-present"

	// ErrorKindAbsolutePresent indicates that Absolute was detected to be executing before the
	// initial OS loader. This is an endpoint management agent that is shipped with the platform
	// firmware. As it requires an OS component, it is generally recommended that this is disabled
	// via the firmware settings UI. Leaving it enabled does increase fragility of PCR4 because it
	// exposes it to changes via firmware updates.
	ErrorKindAbsolutePresent ErrorKind = "absolute-present"

	// ErrorKindInvalidSecureBootMode indicates that the secure boot mode is invalid. Either secure
	// boot is disabled or deployed mode is not enabled.
	ErrorKindInvalidSecureBootMode ErrorKind = "invalid-secure-boot-mode"

	// ErrorKindWeakSecureBootAlgorithmsDetected indicates that either pre-OS components were
	// authenticated with weak Authenticode digests, or CAs with weak public keys were used to
	// authenticate components. This check does have some limitations - for components other than
	// OS components, it is not possible to determine the properties of the signing key for signed
	// components - it is only possible to determine the properties of the trust anchor (the
	// certificate that is stored in db).
	ErrorKindWeakSecureBootAlgorithmsDetected ErrorKind = "weak-secure-boot-algorithms-detected"
	// ErrorKindPreOSDigestVerificationDetected indicates that pre-OS components were authenticated
	// by matching their Authenticode digest to an entry in db. This means that db has to change with
	// every firmware update, increasing the fragility of PCR7.
	// TODO: it might be worth attempting to match the verification with a corresponding
	// launch event from PCR2 or PCR4 to grab the device path and include it as an argument.
	ErrorKindPreOSDigestVerificationDetected ErrorKind = "pre-os-digest-verification-detected"
)

// Preinstall actions describes actions to resolve problems detected during secboot preinstall check.
// See: github.com/snapcore/secboot/efi/preinstall
const (
	// ActionNone corresponds to no action.
	ActionNone PreinstallAction = ""

	// ActionReboot corresponds to rebooting the device. Note that this is a
	// pseudo-action. It cannot be performed by this package - the caller
	// should trigger the reboot.
	ActionReboot PreinstallAction = "reboot"

	// ActionShutdown corresponds to shutting down the device. Note that this
	// is a pseudo-action. It cannot be performed by this package - the caller
	// should trigger the shutdown.
	ActionShutdown PreinstallAction = "shutdown"

	// ActionRebootToFWSettings corresponds to rebooting the device to the firmware
	// settings in order to resolve a problem manually. Note that this is a
	// pseudo-action. It cannot be performed by this package - the caller should
	// trigger the reboot to FW settings.
	//
	// TODO: How do we improve this by offering a hint of what needs to change
	// from entering the firmware settings UI?
	ActionRebootToFWSettings PreinstallAction = "reboot-to-fw-settings"

	// ActionContactOEM is a hint that the user should contact the OEM for the
	// device because of a bug in the platform. It is a pseudo-action and cannnot
	// be performed by this package.
	ActionContactOEM PreinstallAction = "contact-oem"

	// ActionContactOSVendor is a hint that the user should contact the OS vendor
	// because of a bug in the OS. It is a pseudo-action and cannnot be performed
	// by this package.
	ActionContactOSVendor PreinstallAction = "contact-os-vendor"
)
