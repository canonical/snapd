// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const sshKeysSummary = `allows reading ssh user configuration and keys`

const sshKeysBaseDeclarationSlots = `
  ssh-keys:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const sshKeysConnectedPlugAppArmor = `
# Description: Can read ssh user configuration as well as public and private
# keys.

/usr/bin/ssh ixr,
/etc/ssh/ssh_config r,
owner @{HOME}/.ssh/{,**} r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "ssh-keys",
		summary:               sshKeysSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  sshKeysBaseDeclarationSlots,
		connectedPlugAppArmor: sshKeysConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
