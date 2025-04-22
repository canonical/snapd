// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2025 Canonical Ltd
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

package secboot

import (
	"context"
	"fmt"
	"reflect"

	secboot_efi "github.com/snapcore/secboot/efi"
	"github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/client"
)

var (
	preinstallNewRunChecksContext = preinstall.NewRunChecksContext
	preinstallRun                 = (*preinstall.RunChecksContext).Run
)

func convertCompoundError(err error) error {
	errs, ok := err.(preinstall.CompoundError)
	if !ok {
		if kindAndActions, ok := err.(*preinstall.ErrorKindAndActions); !ok {
			return client.NewCompoundPreinstallError("preinstall check detected errors", kindAndActions)
		}
		return client.NewCompoundPreinstallInternalError("cannot convert error of unexpected type %T (%v)", reflect.TypeOf(err), err)
	}

	var convErrors []error
	for _, err := range errs.Unwrap() {
		if kindAndActions, ok := err.(*preinstall.ErrorKindAndActions); !ok {
			return client.NewCompoundPreinstallInternalError("cannot convert error of unexpected type %T (%v)", reflect.TypeOf(err), err)
		} else {
			convKind, convKindErr := convertErrorKind(kindAndActions.ErrorKind)
			if convKindErr != nil {
				return client.NewCompoundPreinstallInternalError("%v (%v)", convKindErr, err)
			}

			convActions, convActionsErr := convertErrorActions(kindAndActions.Actions)
			if convActionsErr != nil {
				return client.NewCompoundPreinstallInternalError("%v (%v)", convActionsErr, err)
			}

			convErrors = append(convErrors, &client.PreinstallErrorAndActions{
				Kind:    convKind,
				Message: kindAndActions.Unwrap().Error(),
				Args:    kindAndActions.ErrorArgs,
				Actions: convActions,
			})
		}
	}

	if len(convErrors) > 0 {
		return client.NewCompoundPreinstallError("preinstall check detected errors", convErrors...)
	}

	return nil
}

func convertErrorKind(kind preinstall.ErrorKind) (client.ErrorKind, error) {
	var convKind client.ErrorKind

	switch kind {
	case preinstall.ErrorKindNone:
		convKind = client.ErrorKindNone
	case preinstall.ErrorKindInternal:
		convKind = client.ErrorKindInternal
	case preinstall.ErrorKindShutdownRequired:
		convKind = client.ErrorKindShutdownRequired
	case preinstall.ErrorKindRebootRequired:
		convKind = client.ErrorKindRebootRequired
	case preinstall.ErrorKindUnexpectedAction:
		convKind = client.ErrorKindUnexpectedAction
	case preinstall.ErrorKindMissingArgument:
		convKind = client.ErrorKindMissingArgument
	case preinstall.ErrorKindInvalidArgument:
		convKind = client.ErrorKindInvalidArgument
	case preinstall.ErrorKindRunningInVM:
		convKind = client.ErrorKindRunningInVM
	case preinstall.ErrorKindNoSuitableTPM2Device:
		convKind = client.ErrorKindNoSuitableTPM2Device
	case preinstall.ErrorKindTPMDeviceFailure:
		convKind = client.ErrorKindTPMDeviceFailure
	case preinstall.ErrorKindTPMDeviceDisabled:
		convKind = client.ErrorKindTPMDeviceDisabled
	case preinstall.ErrorKindTPMHierarchiesOwned:
		convKind = client.ErrorKindTPMHierarchiesOwned
	case preinstall.ErrorKindTPMDeviceLockout:
		convKind = client.ErrorKindTPMDeviceLockout
	case preinstall.ErrorKindInsufficientTPMStorage:
		convKind = client.ErrorKindInsufficientTPMStorage
	case preinstall.ErrorKindNoSuitablePCRBank:
		convKind = client.ErrorKindNoSuitablePCRBank
	case preinstall.ErrorKindMeasuredBoot:
		convKind = client.ErrorKindMeasuredBoot
	case preinstall.ErrorKindEmptyPCRBanks:
		convKind = client.ErrorKindEmptyPCRBanks
	case preinstall.ErrorKindTPMCommandFailed:
		convKind = client.ErrorKindTPMCommandFailed
	case preinstall.ErrorKindInvalidTPMResponse:
		convKind = client.ErrorKindInvalidTPMResponse
	case preinstall.ErrorKindTPMCommunication:
		convKind = client.ErrorKindTPMCommunication
	case preinstall.ErrorKindUnsupportedPlatform:
		convKind = client.ErrorKindUnsupportedPlatform
	case preinstall.ErrorKindUEFIDebuggingEnabled:
		convKind = client.ErrorKindUEFIDebuggingEnabled
	case preinstall.ErrorKindInsufficientDMAProtection:
		convKind = client.ErrorKindInsufficientDMAProtection
	case preinstall.ErrorKindNoKernelIOMMU:
		convKind = client.ErrorKindNoKernelIOMMU
	case preinstall.ErrorKindTPMStartupLocalityNotProtected:
		convKind = client.ErrorKindTPMStartupLocalityNotProtected
	case preinstall.ErrorKindHostSecurity:
		convKind = client.ErrorKindHostSecurity
	case preinstall.ErrorKindPCRUnusable:
		convKind = client.ErrorKindPCRUnusable
	case preinstall.ErrorKindPCRUnsupported:
		convKind = client.ErrorKindPCRUnsupported
	case preinstall.ErrorKindVARSuppliedDriversPresent:
		convKind = client.ErrorKindVARSuppliedDriversPresent
	case preinstall.ErrorKindSysPrepApplicationsPresent:
		convKind = client.ErrorKindSysPrepApplicationsPresent
	case preinstall.ErrorKindAbsolutePresent:
		convKind = client.ErrorKindAbsolutePresent
	case preinstall.ErrorKindInvalidSecureBootMode:
		convKind = client.ErrorKindInvalidSecureBootMode
	case preinstall.ErrorKindWeakSecureBootAlgorithmsDetected:
		convKind = client.ErrorKindWeakSecureBootAlgorithmsDetected
	case preinstall.ErrorKindPreOSDigestVerificationDetected:
		convKind = client.ErrorKindPreOSDigestVerificationDetected
	default:
		return client.ErrorKindNone, fmt.Errorf("unknown preinstall error kind %s", kind)
	}

	return convKind, nil
}

func convertErrorActions(actions []preinstall.Action) ([]client.PreinstallAction, error) {
	convActions := make([]client.PreinstallAction, len(actions))

	for _, action := range actions {

		switch action {
		case preinstall.ActionNone:
			convActions = append(convActions, client.ActionNone)
		case preinstall.ActionReboot:
			convActions = append(convActions, client.ActionReboot)
		case preinstall.ActionShutdown:
			convActions = append(convActions, client.ActionShutdown)
		case preinstall.ActionRebootToFWSettings:
			convActions = append(convActions, client.ActionRebootToFWSettings)
		case preinstall.ActionContactOEM:
			convActions = append(convActions, client.ActionContactOEM)
		case preinstall.ActionContactOSVendor:
			convActions = append(convActions, client.ActionContactOSVendor)
		default:
			return []client.PreinstallAction{}, fmt.Errorf("unknown preinstall action %s", action)
		}
	}

	return convActions, nil
}

func PreinstallCheck() error {
	// do not customize preinstall checks
	checkCustomizationFlags := preinstall.CheckFlags(0)
	// do not customize TCG compliant PCR profile generation
	profileOptionFlags := preinstall.PCRProfileOptionsFlags(0)
	// no image required because we avoid profile option flags WithBootManagerCodeProfile and WithSecureBootPolicyProfile
	loadedImages := []secboot_efi.Image{}
	checksContext := preinstallNewRunChecksContext(checkCustomizationFlags, loadedImages, profileOptionFlags)

	// no actions args due to no actions for preinstall checks
	args := []any{}
	_, compoundError := preinstallRun(checksContext, context.Background(), preinstall.ActionNone, args)
	return convertCompoundError(compoundError)
}
