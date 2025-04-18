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

	"github.com/snapcore/secboot/efi/preinstall"
	secboot_efi "github.com/snapcore/secboot/efi"
)

var (
	preinstallNewRunChecksContext = preinstall.NewRunChecksContext
	preinstallRun                 = (*preinstall.RunChecksContext).Run
	preinstallErrors              = (*preinstall.RunChecksContext).Errors
	preinstallLastError           = (*preinstall.RunChecksContext).LastError
	preinstallResult              = (*preinstall.RunChecksContext).Result
) 

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
        _, joinedErrors := preinstallRun(checksContext, context.Background(), preinstall.ActionNone, args)
	return joinedErrors
}
