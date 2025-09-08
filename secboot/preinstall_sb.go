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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	sb_efi "github.com/snapcore/secboot/efi"
	sb_preinstall "github.com/snapcore/secboot/efi/preinstall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"
)

// PreinstallCheckContext wraps RunChecksContext to control access
// and avoid exposing the secboot dependency to external packages.
type PreinstallCheckContext struct {
	sbRunChecksContext *sb_preinstall.RunChecksContext
}

// preinstallCheckInfo contains information required post install
// for optimum PCR configuration and resealing.
type preinstallCheckInfo struct {
	Result         *sb_preinstall.CheckResult           `json:"result"`
	PCRProfileOpts sb_preinstall.PCRProfileOptionsFlags `json:"pcr-profile-opts"`
}

var (
	sbPreinstallNewRunChecksContext = sb_preinstall.NewRunChecksContext
	sbPreinstallRunChecks           = (*sb_preinstall.RunChecksContext).Run
)

// PreinstallCheck runs preinstall checks using default check configuration and
// TCG-compliant PCR profile generation options to evaluate whether the host
// environment is an EFI system suitable for TPM-based Full Disk Encryption. The
// caller must supply the current boot images in boot order via bootImagePaths.
// On success, it returns the preinstall check context required for follow-up
// preinstall checks with actions, and a list with details on all errors
// identified by secboot (or nil if no errors were found). Any warnings
// contained in the secboot result are logged. On failure, it returns the error
// encountered while interpreting the secboot error.
//
// To support testing, when the system is running in a Virtual Machine, the check
// configuration is modified to permit this to avoid an error.
func PreinstallCheck(ctx context.Context, bootImagePaths []string) (*PreinstallCheckContext, []PreinstallErrorDetails, error) {
	// allow value-added-retailer drivers that are:
	//  - listed as Driver#### load options
	//  - referenced in the DriverOrder UEFI variable
	//  - loaded from PCI device option ROMs (e.g. network card PXE ROMs)
	//TODO:FDEM: remove once secboot provides an action to apply this configuration
	checkFlags := sb_preinstall.PermitVARSuppliedDrivers
	if systemd.IsVirtualMachine() {
		// when running in Virtual Machine, allow it
		checkFlags |= sb_preinstall.PermitVirtualMachine
	}

	// do not customize TCG compliant PCR profile generation
	profileOptionFlags := sb_preinstall.PCRProfileOptionsDefault

	// create boot file images from provided paths
	var bootImages []sb_efi.Image
	for _, image := range bootImagePaths {
		bootImages = append(bootImages, sb_efi.NewFileImage(image))
	}

	checkContext := &PreinstallCheckContext{sbPreinstallNewRunChecksContext(checkFlags, bootImages, profileOptionFlags)}

	// no actions or action args for preinstall checks
	result, err := sbPreinstallRunChecks(checkContext.sbRunChecksContext, ctx, sb_preinstall.ActionNone)
	if err != nil {
		errorDetails, err := unwrapPreinstallCheckError(err)
		if err != nil {
			return nil, errorDetails, err
		}
		return checkContext, errorDetails, err
	}

	if result.Warnings != nil {
		for _, warn := range result.Warnings.Unwrap() {
			logger.Noticef("preinstall check warning: %v", warn)
		}
	}

	return checkContext, nil, nil
}

// PreinstallCheckAction runs a follow-up preinstall check using the specified
// action to evaluate whether a previously reported issue can be resolved. It
// reuses the check configuration and boot image state from the preinstall check
// context. On success, it returns a list with details on all remaining errors
// identified by secboot or nil if no errors were found. Any warnings contained
// in the secboot result are logged. On failure, it returns the error
// encountered while interpreting the secboot error.
func (c *PreinstallCheckContext) PreinstallCheckAction(ctx context.Context, action *PreinstallAction) ([]PreinstallErrorDetails, error) {
	//TODO:FDEM: Changes to secboot required to allow passing args in a more usable format
	result, err := sbPreinstallRunChecks(c.sbRunChecksContext, ctx, sb_preinstall.Action(action.Action))
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

// SaveCheckResult writes the serialized preinstall check information in the
// location specified by the filename.
func (c *PreinstallCheckContext) SaveCheckInfo(filename string) error {
	if c.sbRunChecksContext == nil {
		return fmt.Errorf("preinstall check context unavailable")
	}

	result := c.sbRunChecksContext.Result()
	if result == nil {
		errorCount := len(c.sbRunChecksContext.Errors())
		return fmt.Errorf("preinstall check result unavailable: %d unresolved errors", errorCount)
	}

	// TODO:FDEM: use profileOpts from c.sbRunChecksContext when there is a way.
	bytes, err := json.Marshal(preinstallCheckInfo{result, sb_preinstall.PCRProfileOptionsDefault})
	if err != nil {
		return fmt.Errorf("cannot serialize preinstall check information: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(filename, bytes, 0600, 0)
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
