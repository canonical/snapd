// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package builtin

import (
	"fmt"
	"strings"
)

const personalFilesSummary = `allows access to personal files or directories`

const personalFilesBaseDeclarationPlugs = `
  personal-files:
    allow-installation: false
    deny-auto-connection: true
`

const personalFilesBaseDeclarationSlots = `
  personal-files:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const personalFilesConnectedPlugAppArmor = `
# Description: Can access specific personal files or directories in the 
# users's home directory.
# This is restricted because it gives file access to arbitrary locations.
`

type personalFilesInterface struct {
	commonFilesInterface
}

func validateSinglePathHome(np string) error {
	if !strings.HasPrefix(np, "$HOME/") {
		return fmt.Errorf(`%q must start with "$HOME/"`, np)
	}
	if strings.Count(np, "$HOME") > 1 {
		return fmt.Errorf(`$HOME must only be used at the start of the path of %q`, np)
	}
	return nil
}

func init() {
	registerIface(&personalFilesInterface{
		commonFilesInterface{
			commonInterface: commonInterface{
				name:                 "personal-files",
				summary:              personalFilesSummary,
				implicitOnCore:       true,
				implicitOnClassic:    true,
				baseDeclarationPlugs: personalFilesBaseDeclarationPlugs,
				baseDeclarationSlots: personalFilesBaseDeclarationSlots,
			},
			apparmorHeader:    personalFilesConnectedPlugAppArmor,
			extraPathValidate: validateSinglePathHome,
		},
	})
}
