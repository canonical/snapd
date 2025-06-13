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

	sb_efi "github.com/snapcore/secboot/efi"
	sb_preinstall "github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

var (
	sbPreinstallNewRunChecksContext = sb_preinstall.NewRunChecksContext
	sbPreinstallRunChecks           = (*sb_preinstall.RunChecksContext).Run
)

// PreinstallCheck runs preinstall checks using default check configuration and
// TCG-compliant PCR profile generation options to evaluate whether the host
// environment is an EFI system suitable for TPM-based Full Disk Encryption. The
// caller must supply the current boot images in boot order via loadedImages.
// On success, it returns a list with details on all errors identified by secboot
// or nil if no errors were found. Any warnings contained in the secboot result
// are logged. On failure, it returns the error encountered while interpreting
// the secboot error.
//
// To support testing, when test mode is detected and the system is running in a
// Virtual Machine, the check configuration is modified to permit this without
// treating it as an error.
func PreinstallCheck(ctx context.Context, bootImagePaths []string) ([]PreinstallErrorDetails, error) {
	// do not customize check configuration
	checkFlags := sb_preinstall.CheckFlagsDefault
	if snapdenv.Testing() && systemd.IsVirtualMachine() {
		// with exception of testing in virtual machine
		checkFlags |= sb_preinstall.PermitVirtualMachine
	}

	// do not customize TCG compliant PCR profile generation
	profileOptionFlags := sb_preinstall.PCRProfileOptionsDefault

	// create boot file images from provided paths
	var bootImages []sb_efi.Image
	for _, image := range bootImagePaths {
		bootImages = append(bootImages, sb_efi.NewFileImage(image))
	}

	checksContext := sbPreinstallNewRunChecksContext(checkFlags, bootImages, profileOptionFlags)

	// no actions or action args for preinstall checks
	result, err := sbPreinstallRunChecks(checksContext, ctx, sb_preinstall.ActionNone)
	if err != nil {
		return unwrapPreinstallCheckError(err)
	}

	if result.Warnings != nil {
		for _, warn := range result.Warnings.Unwrap() {
			logger.Noticef("preinstall check warning: %v", warn)
		}
	}
	return nil, nil
}

// unwrapPreinstallCheckError converts a single or compound preinstall check
// error into a slice of PreinstallErrorDetails. This function returns an error
// if the provided error or any compounded error is not of type
// *preinstall.ErrorKindAndActions.
func unwrapPreinstallCheckError(err error) ([]PreinstallErrorDetails, error) {
	// expect either a single or compound error
	compoundErr, ok := err.(sb_preinstall.CompoundError)
	if !ok {
		// single error
		kindAndActions, ok := err.(*sb_preinstall.WithKindAndActionsError)
		if !ok {
			return nil, fmt.Errorf("cannot unwrap error of unexpected type %[1]T (%[1]v)", err)
		}
		return []PreinstallErrorDetails{
			convertPreinstallCheckErrorType(kindAndActions),
		}, nil
	}

	// unwrap compound error
	errs := compoundErr.Unwrap()
	if errs == nil {
		return nil, fmt.Errorf("compound error does not wrap any error")
	}
	unwrapped := make([]PreinstallErrorDetails, 0, len(errs))
	for _, err := range errs {
		kindAndActions, ok := err.(*sb_preinstall.WithKindAndActionsError)
		if !ok {
			return nil, fmt.Errorf("cannot unwrap error of unexpected type %[1]T (%[1]v)", err)
		}
		unwrapped = append(unwrapped, convertPreinstallCheckErrorType(kindAndActions))
	}
	return unwrapped, nil
}

func convertPreinstallCheckErrorType(kindAndActionsErr *sb_preinstall.WithKindAndActionsError) PreinstallErrorDetails {
	return PreinstallErrorDetails{
		Kind:    string(kindAndActionsErr.Kind),
		Message: kindAndActionsErr.Error(), // safely handles kindAndActionsErr.Unwrap() == nil
		Args:    kindAndActionsErr.Args,
		Actions: convertPreinstallCheckErrorActions(kindAndActionsErr.Actions),
	}
}

func convertPreinstallCheckErrorActions(actions []sb_preinstall.Action) []string {
	if actions == nil {
		return nil
	}

	convActions := make([]string, len(actions))
	for i, action := range actions {
		convActions[i] = string(action)
	}
	return convActions
}
