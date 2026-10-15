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

const systemFilesSummary = `allows access to system files or directories`

const systemFilesBaseDeclarationPlugs = `
  system-files:
    allow-installation: false
    deny-auto-connection: true
`

const systemFilesBaseDeclarationSlots = `
  system-files:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemFilesConnectedPlugAppArmor = `
# Description: Can access specific system files or directories.
# This is restricted because it gives file access to arbitrary locations.
`

type systemFilesInterface struct {
	commonFilesInterface
}

func validateSinglePathSystem(np string) error {
	if !strings.HasPrefix(np, "/") {
		return fmt.Errorf(`%q must start with "/"`, np)
	}
	if strings.Contains(np, "$HOME") {
		return fmt.Errorf(`$HOME cannot be used in %q`, np)
	}

	return nil
}

func init() {
	registerIface(&systemFilesInterface{
		commonFilesInterface{
			commonInterface: commonInterface{
				name:                 "system-files",
				summary:              systemFilesSummary,
				implicitOnCore:       true,
				implicitOnClassic:    true,
				baseDeclarationPlugs: systemFilesBaseDeclarationPlugs,
				baseDeclarationSlots: systemFilesBaseDeclarationSlots,
			},
			apparmorHeader:    systemFilesConnectedPlugAppArmor,
			extraPathValidate: validateSinglePathSystem,
		},
	})
}
