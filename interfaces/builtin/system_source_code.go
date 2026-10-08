// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// https://refspecs.linuxfoundation.org/FHS_3.0/fhs-3.0.html#usrsrcSourceCode
const systemSourceCodeSummary = `allows read-only access to /usr/src on the system`

// Manually connected since this reveals kernel config, patches, etc of the
// running system which may or may not correspond to public distro packages
// as mapped from 'uname -r'
const systemSourceCodeBaseDeclarationSlots = `
  system-source-code:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemSourceCodeConnectedPlugAppArmor = `
# Description: can access /usr/src for kernel headers, etc
/usr/src/{,**} r,
`

type systemSourceCodeInterface struct {
	commonInterface
}

func init() {
	registerIface(&systemSourceCodeInterface{
		commonInterface: commonInterface{
			name:                  "system-source-code",
			summary:               systemSourceCodeSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  systemSourceCodeBaseDeclarationSlots,
			connectedPlugAppArmor: systemSourceCodeConnectedPlugAppArmor,
		},
	})
}
