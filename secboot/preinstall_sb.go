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
	"path/filepath"

	sb_efi "github.com/snapcore/secboot/efi"
	"github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

const (
	hybridInstallBootloaderShimGlob  = "cdrom/EFI/boot/boot*.efi"
	hybridOInstallBootloaderGrubGlob = "cdrom/EFI/boot/grub*.efi"
	hybridInstallKernelFile          = "cdrom/casper/vmlinuz"
)

var (
	hybridInstallRootDir          = "/"
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

func NewPreinstallCompoundError(errorAndActions []preinstall.ErrorKindAndActions) error {
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
		errorAndActions, ok := err.(*preinstall.ErrorKindAndActions)
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
		errorAndActions, ok := err.(*preinstall.ErrorKindAndActions)
		if !ok {
			return []PreinstallErrorAndActions{
				newInternalErrorUnexpectedType(err),
			}
		}
		converted = append(converted, convertErrorType(errorAndActions))
	}
	return converted
}

func convertErrorType(errorAndActions *preinstall.ErrorKindAndActions) PreinstallErrorAndActions {
	return PreinstallErrorAndActions{
		Kind:    string(errorAndActions.ErrorKind),
		Message: errorAndActions.Unwrap().Error(),
		Args:    errorAndActions.ErrorArgs,
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

// PreinstallCheck runs preinstall checks without customization or profile generation options.
func PreinstallCheck(model *asserts.Model, tpmMode TPMProvisionMode) error {
	if model.IsHybrid() {
		// XXX: Expect preinstallNewRunChecksContext to require tpmMode in order to evaluate
		// lockout when required. Complete implementation after preinstallNewRunChecksContext
		// is modified.
		_ = tpmMode

		// XXX: Suggest also providing default value for check flags.
		checkCustomizationFlags := preinstall.CheckFlags(0)
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
		_, err := preinstallRun(checksContext, context.Background(), preinstall.ActionNone, args...)
		return err
	}

	// Ubuntu Core systems continue to use the simpler check because we expect
	// corner cases where previously allowed installations will be blocked by the
	// RunChecksContext API.
	// TODO: Transition Ubuntu Core to use the RunChecksContextAPI.
	// XXX: Need to return compound error to be consistent with preinstallRun.
	return CheckTPMKeySealingSupported(tpmMode)
}

func setHybridInstallRootDir(string rootDir) {
	if rootDir == ""{
		hybridInstallRootDir = "/"
	}
	hybridInstallRootDir = filepath.Clean(rootDir)
}

func hybridInstallerLoadedImages() ([]sb_efi.Image, error) {
	imageInfo := []struct {
		name string
		glob string
	}{
		{"shim", filepath.Join(HybridInstallRootDir, hybridInstallBootloaderShimGlob},
		{"grub", filepath.Join(HybridInstallRootDir, hybridInstallBootloaderGrubGlob},
		{"kernel", filepath.Join(HybridInstallRootDir, hybridInstallKernelFile},
	}

	var loadedImages []sb_efi.Image
	for _, info := range imageInfo {
		matches, err := filepath.Glob(info.glob)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot use globbing pattern %q: %v", info.glob, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("cannot locate installer %s using globbing pattern %q", info.name, info.glob)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("unexpected multiple matches for installer %s obtained using globbing pattern %q", info.name, info.glob)
		}
		loadedImages = append(loadedImages, sb_efi.NewFileImage(matches[0]))
	}

	return loadedImages, nil
}
