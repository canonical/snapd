// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

	sb_efi "github.com/snapcore/secboot/efi"
	"github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

var (
	preinstallNewRunChecksContext = preinstall.NewRunChecksContext
	preinstallRun                 = (*preinstall.RunChecksContext).Run
)

type compoundPreinstallError struct {
	errs []error
}

func (c *compoundPreinstallError) Error() string {
	return fmt.Sprintf("preinstall check detected %d errors", len(c.errs))
}

func (c *compoundPreinstallError) Unwrap() []error {
	return c.errs
}

func combineErrors(errs ...error) error {
	return &compoundPreinstallError{errs: errs}
}

func NewPreinstallCompoundError(errorAndActions []preinstall.WithKindAndActionsError) error {
	var errs []error
	for _, err := range errorAndActions {
		errs = append(errs, &err)
	}

	return combineErrors()
}

// UnpackPreinstallCheckError converts a single or compound preinstall check
// error into a slice of PreinstallErrorAndActions. If the provided error or any
// contained error is not of type *preinstall.ErrorKindAndActions, the function
// returns a slice containing a single internal error describing the type.
func UnpackPreinstallCheckError(err error) []PreinstallErrorAndActions {
	// expect either a single or compound error
	compoundErr, ok := err.(preinstall.CompoundError)
	if !ok {
		// single error
		errorAndActions, ok := err.(*preinstall.WithKindAndActionsError)
		if !ok {
			return []PreinstallErrorAndActions{
				newInternalErrorUnexpectedType(err),
			}
		}
		return []PreinstallErrorAndActions{
			convertErrorType(errorAndActions),
		}
	}

	// unpack compound error
	errs := compoundErr.Unwrap()
	converted := make([]PreinstallErrorAndActions, 0, len(errs))
	for _, err := range errs {
		errorAndActions, ok := err.(*preinstall.WithKindAndActionsError)
		if !ok {
			return []PreinstallErrorAndActions{
				newInternalErrorUnexpectedType(err),
			}
		}
		converted = append(converted, convertErrorType(errorAndActions))
	}
	return converted
}

func convertErrorType(errorAndActions *preinstall.WithKindAndActionsError) PreinstallErrorAndActions {
	return PreinstallErrorAndActions{
		Kind:    string(errorAndActions.Kind),
		Message: errorAndActions.Unwrap().Error(),
		Args:    errorAndActions.Args,
		Actions: convertActions(errorAndActions.Actions),
	}
}

func convertActions(actions []preinstall.Action) []string {
	convActions := make([]string, len(actions))
	for i, action := range actions {
		convActions[i] = string(action)
	}
	return convActions
}

func newInternalErrorUnexpectedType(err error) PreinstallErrorAndActions {
	message := fmt.Sprintf("cannot convert error of unexpected type %[1]T (%[1]v)", err)

	return PreinstallErrorAndActions{
		Kind:    string(preinstall.ErrorKindInternal),
		Message: message,
	}
}

// PreinstallCheck runs the default preinstall checks to evaluate whether the host
// environment is an EFI system suitable for TPM-based full disk encryption (FDE).
// It uses standard check and PCR profile options, without customizing TCG-compliant
// PCR profiles. When running in a virtual machine during testing, VM checks are
// permitted. Returns an error on failure and logs any warnings encountered.
func PreinstallCheck(model *asserts.Model, images []sb_efi.Image) error {
	checkCustomizationFlags := preinstall.CheckFlagsDefault
	if snapdenv.Testing() && systemd.IsVirtualMachine() {
		// allow virtual machine when testing
		checkCustomizationFlags |= preinstall.PermitVirtualMachine
	}

	// do not customize TCG compliant PCR profile generation
	profileOptionFlags := preinstall.PCRProfileOptionsDefault
	// no image required because we avoid profile option flags WithBootManagerCodeProfile and WithSecureBootPolicyProfile
	loadedImages := []sb_efi.Image{}
	checksContext := preinstallNewRunChecksContext(checkCustomizationFlags, loadedImages, profileOptionFlags)

	// no actions args due to no actions for preinstall checks
	args := []any{}
	// Ignore the returned *preinstall.CheckResult
	result, err := preinstallRun(checksContext, context.Background(), preinstall.ActionNone, args...)
	if err != nil {
		return err
	}
	if result.Warnings != nil {
		for _, warn := range result.Warnings.Unwrap() {
			logger.Noticef("preinstall check warning: %v", warn)
		}
	}
	return nil
}
