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
	"encoding/json"

	"github.com/snapcore/secboot/efi/preinstall"
	secboot_efi "github.com/snapcore/secboot/efi"
)

type ErrorKind preinstall.ErrorKind
type ErrorArgs json.RawMessage
type ErrorAction preinstall.Action

var (
	preinstallNewRunChecksContext = preinstall.NewRunChecksContext
	preinstallRun                 = (*preinstall.RunChecksContext).Run
)

func unwrapCompoundError(err error) []ErrorAndActions {
	errs, ok := err.(preinstall.CompoundError)
        if !ok {
                return []ErrorAndActions {
			{
				Kind: preinstall.ErrorKindInternal,
				Message:"unexpected non-compound error",
				Args: {"original error message": errs.Error()},
				Action: nil,
			},
		}
        }

	errorsAndActions := []ErrorAndActions{}
	for _, err := range errs.Unwrap() {
                if kindAndActions, ok := err.(*preinstall.ErrorKindAndActions); !ok {
			return []client.PreinstallErrorAndActions {
				{
                        		Kind: preinstall.ErrorKindInternal,
                        		Message:"unexpected error type",
                        		Args: {"original error message": errs.Error()},
                        		Action: nil,
				},
                	}
		} else {
                        conv := client.ErrorAndActions{
				Kind: kindAndActions.ErrorKind,
                                Message: kindAndActions.Unwrap().Error(),
                                Args: kindAndActions.ErrorArgs,
                                Actions: kindAndActions.Actions,
                        }
                        errorsAndActions = append(errorsAndActions, conv)
                }
	}
        
        return errorsAndActions 
}

func PreinstallCheck() []client.PreinstallErrorAndActions {
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
	return unwrapCompoundError(compoundError)
}
