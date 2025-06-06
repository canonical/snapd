// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package secboot

import (
	"context"
	"fmt"

	"github.com/snapcore/secboot/efi"
	"github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

var (
	preinstallNewRunChecksContext = preinstall.NewRunChecksContext
	preinstallRunChecks           = (*preinstall.RunChecksContext).Run
)

// PreinstallCheck runs the default preinstall checks to evaluate whether the host
// environment is an EFI system suitable for TPM-based full disk encryption (FDE).
// It uses standard check and PCR profile options, without customizing TCG-compliant
// PCR profiles. To support testing, the check configuration is modified to permit
// running in a Virtual Machine (VM) when detecting test mode and running in a VM.
//
// Returns structured information about errors identified by the secboot checks
// and logs any warnings. If the errors returned by the secboot checks cannot be
// processed, this function returns an error.
func PreinstallCheck(bootImagePaths []string) ([]PreinstallErrorInfo, error) {
	// do not customize check configuration
	checkFlags := preinstall.CheckFlagsDefault
	if snapdenv.Testing() && systemd.IsVirtualMachine() {
		// with exception of testing in virtual machine
		checkFlags |= preinstall.PermitVirtualMachine
	}

	// do not customize TCG compliant PCR profile generation
	profileOptionFlags := preinstall.PCRProfileOptionsDefault
	// create boot file images from provided paths
	var bootImages []efi.Image
	for _, image := range bootImagePaths {
		bootImages = append(bootImages, efi.NewFileImage(image))
	}
	checksContext := preinstallNewRunChecksContext(checkFlags, bootImages, profileOptionFlags)

	// no actions or action args for preinstall checks
	result, err := preinstallRunChecks(checksContext, context.Background(), preinstall.ActionNone)
	if err != nil {
		return unpackPreinstallCheckError(err)
	}

	if result.Warnings != nil {
		for _, warn := range result.Warnings.Unwrap() {
			logger.Noticef("preinstall check warning: %v", warn)
		}
	}
	return nil, nil
}

// unpackPreinstallCheckError converts a single or compound preinstall check
// error into a slice of PreinstallErrorInfo. This function returns an error
// if the provided error or any compounded error is not of type
// *preinstall.ErrorKindAndActions.
func unpackPreinstallCheckError(err error) ([]PreinstallErrorInfo, error) {
	// expect either a single or compound error
	compoundErr, ok := err.(preinstall.CompoundError)
	if !ok {
		// single error
		kindAndActions, ok := err.(*preinstall.WithKindAndActionsError)
		if !ok {
			return nil, fmt.Errorf("cannot unpack error of unexpected type %[1]T (%[1]v)", err)
		}
		return []PreinstallErrorInfo{
			convertErrorType(kindAndActions),
		}, nil
	}

	// unpack compound error
	errs := compoundErr.Unwrap()
	if errs == nil {
		return nil, fmt.Errorf("unexpected compound error wraps nil")
	}
	unpacked := make([]PreinstallErrorInfo, 0, len(errs))
	for _, err := range errs {
		kindAndActions, ok := err.(*preinstall.WithKindAndActionsError)
		if !ok {
			return nil, fmt.Errorf("cannot unpack error of unexpected type %[1]T (%[1]v)", err)
		}
		unpacked = append(unpacked, convertErrorType(kindAndActions))
	}
	return unpacked, nil
}

func convertErrorType(kindAndActionsErr *preinstall.WithKindAndActionsError) PreinstallErrorInfo {
	return PreinstallErrorInfo{
		Kind:    string(kindAndActionsErr.Kind),
		Message: kindAndActionsErr.Error(), // safely handles kindAndActionsErr.Unwrap() == nil
		Args:    kindAndActionsErr.Args,
		Actions: convertActions(kindAndActionsErr.Actions),
	}
}

func convertActions(actions []preinstall.Action) []string {
	if actions == nil {
		return nil
	}

	convActions := make([]string, len(actions))
	for i, action := range actions {
		convActions[i] = string(action)
	}
	return convActions
}
